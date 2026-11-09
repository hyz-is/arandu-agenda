package agenda

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"
	"github.com/arandu-io/hesape/database/model"

	"github.com/hyz-is/arandu-agenda/internal/platform"
)

// core is what the services share: the database handle, the configuration, the
// sentences and the clock. It holds no policy, because each service has its
// own, and nothing here authorizes anything: a method of core is reached only
// after the service that called it asked its policy and holds the Grant.
type core struct {
	db     *data.DB
	cfg    Config
	labels Labels
	now    clock
}

func newCore(db *data.DB, cfg Config) *core {
	read := cfg.Now
	return &core{db: db, cfg: cfg, labels: labelsOf(cfg), now: func() time.Time { return read().UTC() }}
}

// BookingService orchestrates what a customer does with the agenda: it
// validates the request, asks the policy for a Grant, applies the rules the
// entity holds for itself, and persists with that Grant.
//
// It receives its collaborators through the constructor. There is no container
// and no resolution by reflection: what this service is made of is written at
// the one place that builds it, and reading that place is how somebody learns
// what the package touches.
//
// Everything a handler is allowed to do for a customer goes through here. The
// service is the only owner of the database handle on this side, so the request
// layer cannot reach a Model before the policy has answered.
type BookingService struct {
	*core
	policy BookingPolicy
}

// NewBookingService wires the service over the application's database handle.
func NewBookingService(db *data.DB, cfg Config) *BookingService {
	return &BookingService{core: newCore(db, cfg.withDefaults()), policy: BookingPolicy{}}
}

// viewer is how much of the space a customer reads.
type viewer int

const (
	// viewsOwn reads the bookings the subject made.
	viewsOwn viewer = iota
	// viewsSpace reads every booking of the space.
	viewsSpace
)

// action is the action the viewer was authorized with.
func (v viewer) action() security.Action {
	if v == viewsSpace {
		return AgendaViewSpace
	}
	return AgendaViewOwn
}

// authorizeView asks the policy for the widest reading the subject holds: the
// whole space's, else their own. Both questions go to the policy; the answer
// says which one it allowed, and the statement is narrowed to it.
func (s *BookingService) authorizeView(ctx context.Context, actor security.Subject) (security.Grant, viewer, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewSpace, Booking{})
	if err == nil {
		return g, viewsSpace, nil
	}
	g, ownErr := security.Authorize(ctx, s.policy, actor, AgendaViewOwn, Booking{})
	if ownErr != nil {
		return security.Grant{}, viewsOwn, ownErr
	}
	return g, viewsOwn, nil
}

// visible narrows a statement to what the viewer reads: every booking of the
// space, or the subject's own. The tenant is the Grant's, applied by the
// table; this is the second predicate, and the one a rule about individual
// records belongs in.
func (v viewer) visible(q *BookingQuery, actor security.Subject) *BookingQuery {
	if v == viewsOwn {
		return q.Where("requester_id", "=", actor.ID)
	}
	return q
}

// sortableBooking is the ordering allowlist of the customer's list. A column
// name taken directly from a request would turn ordering into an injection
// surface.
var sortableBooking = map[string]string{
	"": "starts_at", "starts_at": "starts_at", "created_at": "created_at", "code": "code",
	"service": "service_name", "status": "status", "subject": "subject_label",
}

// CanBook answers whether the subject may book, for the screen that draws the
// form and the button that opens it. It asks the policy exactly as Book does,
// and the refusal it returns is the one Book would.
func (s *BookingService) CanBook(ctx context.Context, actor security.Subject) error {
	_, err := security.Authorize(ctx, s.policy, actor, AgendaBook, Booking{})
	return err
}

// Services returns the services the team offers, the ones a customer can book.
func (s *BookingService) Services(ctx context.Context, actor security.Subject) ([]ServiceView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaBook, Booking{})
	if err != nil {
		return nil, err
	}
	found, err := Services(s.db).Where("active", "=", 1).OrderBy("name").OrderBy("key").Get(ctx, g)
	if err != nil {
		return nil, err
	}
	out := make([]ServiceView, 0, len(found))
	for _, row := range found {
		out = append(out, serviceViewOf(row))
	}
	return out, nil
}

// authorizeSlots asks the policy whether the subject may look at the free
// times: whoever may book, and whoever may change a booking, which is choosing
// another time. The refusal is the one of booking.
func (s *BookingService) authorizeSlots(ctx context.Context, actor security.Subject) (security.Grant, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaBook, Booking{})
	if err == nil {
		return g, nil
	}
	g, changeErr := security.Authorize(ctx, s.policy, actor, AgendaChange, Booking{})
	if changeErr != nil {
		return security.Grant{}, err
	}
	return g, nil
}

// Slots returns the free starts of one day for a service, sorted: inside the
// team's weekly hours, outside its closures, with a place free, no sooner than
// Config.MinNotice and no later than Config.Horizon from now. Only the calendar
// date of day counts, read in Config.Zone.
func (s *BookingService) Slots(ctx context.Context, actor security.Subject, serviceKey string, day time.Time) ([]time.Time, error) {
	g, err := s.authorizeSlots(ctx, actor)
	if err != nil {
		return nil, err
	}
	service, err := s.offered(ctx, g, serviceKey)
	if err != nil {
		return nil, err
	}
	now := s.now()
	from := s.dayStart(day)
	schedule, err := s.schedule(ctx, g, Span{Start: from, End: from.Add(48 * time.Hour)}, "", now)
	if err != nil {
		return nil, err
	}
	return schedule.Slots(now, day, service.duration()), nil
}

// Book takes a place on the team's agenda for the signed-in customer, in their
// own space.
//
// The slot is checked again inside a transaction that holds the lock of the
// protocol counter, so two people asking for the last place take turns and the
// second is told ErrSlotTaken. A service that asks for a payment leaves the
// booking waiting for it, holding the slot for Config.PaymentHold; any other is
// scheduled at once.
func (s *BookingService) Book(ctx context.Context, actor security.Subject, in BookInput) (*Booking, error) {
	if errs := s.validateBook(in); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaBook, Booking{})
	if err != nil {
		return nil, err
	}
	tenant := data.Tenant(g)
	service, err := s.offered(ctx, g, trimmed(in.Service))
	if err != nil {
		return nil, err
	}
	subject := BookingSubject{Type: trimmed(in.Subject.Type), ID: trimmed(in.Subject.ID), Label: clip(trimmed(in.Subject.Label), maxLabelLen)}
	if s.cfg.Subjects != nil && (subject.Type != "" || subject.ID != "") {
		label, err := s.cfg.Subjects.Resolve(ctx, tenant, subject)
		if err != nil {
			return nil, s.labels.invalid("subject", "subject")
		}
		subject.Label = clip(trimmed(label), maxLabelLen)
	}
	who := s.person(ctx, tenant, actor.ID)
	start := in.Start.UTC()

	var booked *Booking
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		now := s.now()
		number, err := s.bump(ctx, g, now)
		if err != nil {
			return err
		}
		from := s.dayStart(start.In(s.cfg.Zone))
		schedule, err := s.schedule(ctx, g, Span{Start: from, End: from.Add(48 * time.Hour)}, "", now)
		if err != nil {
			return err
		}
		if !schedule.Offers(now, start, service.duration()) {
			return s.labels.taken()
		}
		row, err := Bookings(s.db).New()
		if err != nil {
			return err
		}
		if row.ID, err = data.NewID(); err != nil {
			return err
		}
		row.TenantID = data.Tenant(g)
		row.Code = protocol(number)
		row.ServiceKey, row.ServiceName = service.Key, service.Name
		row.SubjectType, row.SubjectID, row.SubjectLabel = subject.Type, subject.ID, subject.Label
		row.RequesterID, row.RequesterName = who.ID, who.Name
		row.CustomerNote = trimmed(in.Note)
		row.StartsAt, row.EndsAt = start, start.Add(service.duration())
		row.Status = StatusScheduled
		if bool(service.RequiresPayment) {
			hold := now.Add(s.cfg.PaymentHold)
			row.Status, row.HoldUntil = StatusAwaitingPayment, &hold
		}
		row.CreatedAt, row.UpdatedAt = now, now
		if _, err := row.Save(ctx, g); err != nil {
			return err
		}
		booked = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityBooked, booked, map[string]any{
		"service": booked.ServiceKey, "starts_at": instant(booked.StartsAt), "awaiting_payment": booked.Status == StatusAwaitingPayment,
	}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventBooked, Booking: summaryOf(booked), By: ByCustomer, AwaitingPayment: booked.Status == StatusAwaitingPayment})
	return booked, nil
}

// Bookings returns a page of the customer's bookings: their own, or the whole
// space's when the policy allows it, searched, filtered by status and ordered by
// the server.
func (s *BookingService) Bookings(ctx context.Context, actor security.Subject, q ListQuery) (Page, error) {
	g, scope, err := s.authorizeView(ctx, actor)
	if err != nil {
		return Page{}, err
	}
	column, ok := sortableBooking[q.Sort]
	if !ok {
		return Page{}, s.labels.invalid("sort", "sort")
	}
	statuses, err := s.statuses(q.Statuses)
	if err != nil {
		return Page{}, err
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *BookingQuery {
		b := scope.visible(Bookings(s.db), actor)
		if len(statuses) > 0 {
			b = b.WhereIn("status", statuses)
		}
		matching(b.Base(), q.Search, "code", "service_name", "subject_label")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return Page{}, err
	}
	rows := filtered()
	if q.Dir == "asc" {
		rows = rows.OrderBy(column)
	} else {
		rows = rows.OrderByDesc(column)
	}
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return Page{}, err
	}
	out := Page{Rows: make([]Summary, 0, len(found)), Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	for _, b := range found {
		out.Rows = append(out.Rows, customerView(b))
	}
	return out, nil
}

// Booking returns one booking the customer may read. A booking of another
// space, and one of the same space the subject may not read, are not found.
func (s *BookingService) Booking(ctx context.Context, actor security.Subject, id string) (Summary, error) {
	g, scope, err := s.authorizeView(ctx, actor)
	if err != nil {
		return Summary{}, err
	}
	b, err := scope.visible(Bookings(s.db).WhereKey(id), actor).First(ctx, g)
	if err != nil {
		return Summary{}, err
	}
	if b == nil {
		return Summary{}, ErrNotFound
	}
	// The first Authorize saw an empty booking, so every rule about the record
	// -- whose it is -- never ran. A booking the policy refuses on the second
	// call is answered as not found, as one of another space is: telling a
	// customer that a booking exists and is not theirs tells them what exists.
	if _, err := security.Authorize(ctx, s.policy, actor, scope.action(), *b); err != nil {
		return Summary{}, ErrNotFound
	}
	return customerView(b), nil
}

// Confirm records that the customer will come. Only a booking that holds its
// slot for good, and has not started, can be confirmed.
func (s *BookingService) Confirm(ctx context.Context, actor security.Subject, id string) (*Booking, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaChange, Booking{})
	if err != nil {
		return nil, err
	}
	b, err := s.owned(ctx, g, actor, id)
	if err != nil {
		return nil, err
	}
	if err := b.Confirm(s.now()); err != nil {
		return nil, s.labels.invalid("status", "not_confirmable")
	}
	if _, err := b.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityConfirmed, b, nil); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventConfirmed, Booking: summaryOf(b), By: ByCustomer})
	return b, nil
}

// Reschedule moves the booking to another start, which has to be one Slots
// offers, until Config.ChangeUntil before the start it has. The payment, the
// protocol and the hold stay; the confirmation and the reminder belong to the
// old time and go.
func (s *BookingService) Reschedule(ctx context.Context, actor security.Subject, id string, newStart time.Time) (*Booking, error) {
	if newStart.IsZero() {
		return nil, s.labels.invalid("start", "required")
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaChange, Booking{})
	if err != nil {
		return nil, err
	}
	b, err := s.owned(ctx, g, actor, id)
	if err != nil {
		return nil, err
	}
	if !b.CanMove() {
		return nil, s.labels.invalid("status", "not_changeable")
	}
	if !b.ChangeOpen(s.now(), s.cfg.ChangeUntil) {
		return nil, s.labels.invalid("start", "change_closed")
	}
	from := b.StartsAt
	moved, err := s.move(ctx, g, b, newStart.UTC(), true)
	if err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityRescheduled, moved, map[string]any{
		"from": instant(from), "to": instant(moved.StartsAt), "side": ByCustomer,
	}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventRescheduled, Booking: summaryOf(moved), By: ByCustomer})
	return moved, nil
}

// Cancel gives the slot up, until Config.ChangeUntil before the start. The
// reason is the customer's own words, kept on the booking and never in the log.
func (s *BookingService) Cancel(ctx context.Context, actor security.Subject, id, reason string) (*Booking, error) {
	if utf8.RuneCountInString(trimmed(reason)) > maxReasonLen {
		return nil, s.labels.invalid("reason", "max", maxReplace(maxReasonLen))
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaChange, Booking{})
	if err != nil {
		return nil, err
	}
	b, err := s.owned(ctx, g, actor, id)
	if err != nil {
		return nil, err
	}
	if !b.CanMove() {
		return nil, s.labels.invalid("status", "not_changeable")
	}
	if !b.ChangeOpen(s.now(), s.cfg.ChangeUntil) {
		return nil, s.labels.invalid("start", "change_closed")
	}
	if err := b.Cancel(ByCustomer, trimmed(reason), s.now()); err != nil {
		return nil, s.labels.invalid("status", "not_changeable")
	}
	if _, err := b.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityCancelled, b, map[string]any{"side": ByCustomer}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventCancelled, Booking: summaryOf(b), By: ByCustomer})
	return b, nil
}

// owned loads a booking the customer acts on and asks the policy about the row.
// The read is narrowed to the bookings the subject can see, so a booking of
// another requester is not found for one who reads only their own.
func (s *BookingService) owned(ctx context.Context, g security.Grant, actor security.Subject, id string) (*Booking, error) {
	scope := viewsOwn
	if actor.Can(AgendaViewSpace) {
		scope = viewsSpace
	}
	b, err := scope.visible(Bookings(s.db).WhereKey(id), actor).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, AgendaChange, *b); err != nil {
		return nil, ErrNotFound
	}
	return b, nil
}

// offered is the active service with that key, or a rejected input: a customer
// books what the team offers, and what it stopped offering is not found.
func (c *core) offered(ctx context.Context, g security.Grant, key string) (*Service, error) {
	key = trimmed(key)
	if key == "" {
		return nil, c.labels.invalid("service", "required")
	}
	service, err := Services(c.db).Where("key", "=", key).Where("active", "=", 1).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if service == nil {
		return nil, c.labels.invalid("service", "service")
	}
	return service, nil
}

// duration is how long the service holds the team.
func (s Service) duration() time.Duration { return time.Duration(s.DurationMinutes) * time.Minute }

// dayStart is midnight of the calendar day the instant has in Config.Zone, as
// an instant.
func (c *core) dayStart(day time.Time) time.Time {
	year, month, date := day.Date()
	return time.Date(year, month, date, 0, 0, 0, 0, c.cfg.Zone).UTC()
}

// schedule reads what a schedule is made of for a range of time: the weekly
// hours, the closures that touch the range, and the bookings that hold the team
// in it, of every space. The bookings are read through the table that crosses
// tenants and only for the columns that say when and whether, and the one named
// by exclude -- the booking being moved -- is left out of them.
func (c *core) schedule(ctx context.Context, g security.Grant, within Span, exclude string, now time.Time) (Schedule, error) {
	weekly, err := Hours(c.db).OrderBy("weekday").OrderBy("start_minute").Get(ctx, g)
	if err != nil {
		return Schedule{}, err
	}
	closures, err := Closures(c.db).Where("starts_at", "<", within.End).Where("ends_at", ">", within.Start).Get(ctx, g)
	if err != nil {
		return Schedule{}, err
	}
	holding, err := platform.Bookings(c.db).
		WhereIn("status", []any{string(StatusScheduled), string(StatusConfirmed), string(StatusAwaitingPayment)}).
		Where("starts_at", "<", within.End).Where("ends_at", ">", within.Start).
		Get(ctx, g, platform.OccupancyColumns...)
	if err != nil {
		return Schedule{}, err
	}
	out := Schedule{
		Zone: c.cfg.Zone, Capacity: c.cfg.Capacity, Step: c.cfg.SlotStep, MinNotice: c.cfg.MinNotice, Horizon: c.cfg.Horizon,
	}
	for _, w := range weekly {
		out.Weekly = append(out.Weekly, Window{Weekday: w.Weekday, StartMinute: w.StartMinute, EndMinute: w.EndMinute})
	}
	for _, closure := range closures {
		out.Closures = append(out.Closures, Span{Start: closure.StartsAt, End: closure.EndsAt})
	}
	for _, b := range holding {
		if b.ID == exclude || !holds(Status(b.Status), b.HoldUntil, now) {
			continue
		}
		out.Busy = append(out.Busy, Span{Start: b.StartsAt, End: b.EndsAt})
	}
	return out, nil
}

// move gives a booking another start inside a transaction that holds the lock,
// and saves it. offered says the start has to be one the customer is offered;
// otherwise it only has to be in the future, outside every closure and with a
// place free, which is what the team is held to. The booking is the caller's,
// loaded and authorized.
func (c *core) move(ctx context.Context, g security.Grant, b *Booking, start time.Time, offered bool) (*Booking, error) {
	span := Span{Start: start, End: start.Add(b.EndsAt.Sub(b.StartsAt))}
	err := database.Transaction(ctx, c.db, func(ctx context.Context) error {
		now := c.now()
		if err := c.lock(ctx, g, now); err != nil {
			return err
		}
		from := c.dayStart(start.In(c.cfg.Zone))
		schedule, err := c.schedule(ctx, g, Span{Start: from, End: from.Add(48 * time.Hour)}, b.ID, now)
		if err != nil {
			return err
		}
		ok := schedule.Free(span) && start.After(now)
		if offered {
			ok = schedule.Offers(now, start, span.End.Sub(span.Start))
		}
		if !ok {
			return c.labels.taken()
		}
		if err := b.Move(span.Start, span.End, now); err != nil {
			return c.labels.invalid("status", "not_changeable")
		}
		_, err = b.Save(ctx, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ensure makes sure the counter exists, and leaves it as it was when it does.
func (c *core) ensure(ctx context.Context, g security.Grant, now time.Time) error {
	_, err := Counters(c.db).InsertOrIgnore(ctx, g, map[string]any{
		"id": counterBooking, "last_number": 0, "updated_at": now,
	})
	return err
}

// lock takes the counter row for the rest of the transaction, without spending
// a number. Writing the row is what takes the lock on every engine: the second
// transaction to write it waits for the first to end.
func (c *core) lock(ctx context.Context, g security.Grant, now time.Time) error {
	if err := c.ensure(ctx, g, now); err != nil {
		return err
	}
	_, err := Counters(c.db).WhereKey(counterBooking).Update(ctx, g, map[string]any{"updated_at": now})
	return err
}

// bump spends the next number, and takes the lock to do it. It writes before it
// reads, so two transactions that come here at once are told apart by the row
// they wait on and each reads the number it spent. The unique index on the code
// is the net under it. A transaction that is rolled back gives the number back.
func (c *core) bump(ctx context.Context, g security.Grant, now time.Time) (int64, error) {
	if err := c.ensure(ctx, g, now); err != nil {
		return 0, err
	}
	if _, err := Counters(c.db).WhereKey(counterBooking).Increment(ctx, g, "last_number", 1, map[string]any{"updated_at": now}); err != nil {
		return 0, err
	}
	row, err := Counters(c.db).WhereKey(counterBooking).First(ctx, g)
	if err != nil {
		return 0, err
	}
	if row == nil {
		return 0, errors.New("agenda: the counter of the bookings is gone")
	}
	return row.LastNumber, nil
}

// statuses turns the statuses a request named into the values a statement
// compares with, refusing one that is not a status.
func (c *core) statuses(in []Status) ([]any, error) {
	out := make([]any, 0, len(in))
	for _, s := range in {
		if !s.Valid() {
			return nil, c.labels.invalid("status", "status")
		}
		out = append(out, string(s))
	}
	return out, nil
}

// paging bounds a page: from one, and at most PageSize rows unless the caller
// asked for fewer.
func (c *core) paging(perPage, page int) (int, int) {
	if perPage <= 0 || perPage > MaxPageSize {
		perPage = c.cfg.PageSize
	}
	if page < 1 {
		page = 1
	}
	return perPage, page
}

// matching adds one group, column LIKE %term% OR ..., case insensitive on every
// engine, to a statement. The term is bound, never written into the statement;
// a % or _ in it widens the match and nothing more. An empty term adds nothing.
func matching(b *model.Builder, term string, columns ...string) {
	term = strings.TrimSpace(term)
	if term == "" {
		return
	}
	pattern := "%" + term + "%"
	b.WhereGroup("and", func(nested *model.Builder) {
		for i, column := range columns {
			boolean := "or"
			if i == 0 {
				boolean = "and"
			}
			nested.GetQuery().WhereLike(column, pattern, false, boolean, false)
		}
	})
}

// person is who a user is, as the application's Directory knows them; just the
// identifier when it has none or does not know them.
func (c *core) person(ctx context.Context, tenant, id string) Person {
	who := Person{ID: id}
	if c.cfg.Directory == nil || id == "" {
		return who
	}
	known, err := c.cfg.Directory.Person(ctx, tenant, id)
	if err != nil {
		return who
	}
	who.Name = clip(trimmed(known.Name), maxNameLen)
	who.Email = clip(trimmed(known.Email), 254)
	return who
}

// clip cuts text to at most n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// validateBook checks what the customer typed, before anything is authorized.
func (c *core) validateBook(in BookInput) validation.Errors {
	errs := validation.Errors{}
	if trimmed(in.Service) == "" {
		errs.Add("service", c.labels.message("required"))
	}
	if in.Start.IsZero() {
		errs.Add("start", c.labels.message("required"))
	}
	if utf8.RuneCountInString(trimmed(in.Note)) > maxNoteLen {
		errs.Add("note", c.labels.message("max", maxReplace(maxNoteLen)))
	}
	for field, value := range map[string]string{"subject": in.Subject.Type, "subject_id": in.Subject.ID} {
		if utf8.RuneCountInString(trimmed(value)) > maxIdentifierLen {
			errs.Add(field, c.labels.message("max", maxReplace(maxIdentifierLen)))
		}
	}
	return errs
}
