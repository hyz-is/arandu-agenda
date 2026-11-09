package agenda

import (
	"context"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/framework/validation"
	"github.com/arandu-io/hesape/database"

	"github.com/hyz-is/arandu-agenda/internal/platform"
)

// TeamService orchestrates what the platform team does with the bookings of
// every space and with its own agenda.
//
// A member of the team is a subject of Config.TeamTenant, and TeamPolicy
// decides what they may do. The bookings are in the customers' tenants, so a
// use case on one goes in two steps: it asks the policy for a Grant in the
// team's own realm, finds the booking's tenant with it, and then acts with a
// Grant scoped to that one tenant, which exists only because the first one was
// issued for the same action. Nothing a request names chooses the tenant: it is
// the one the booking's row says. The team's services, hours and closures
// belong to no customer: the realm Grant is the one they are read and written
// with.
type TeamService struct {
	*core
	policy TeamPolicy
}

// NewTeamService wires the service over the application's database handle.
func NewTeamService(db *data.DB, cfg Config) *TeamService {
	cfg = cfg.withDefaults()
	return &TeamService{core: newCore(db, cfg), policy: TeamPolicy{Realm: cfg.TeamTenant}}
}

// sortableTeam is the ordering allowlist of the team's list.
var sortableTeam = map[string]string{
	"": "starts_at", "starts_at": "starts_at", "created_at": "created_at", "code": "code", "service": "service_name",
	"status": "status", "requester": "requester_name", "tenant": "tenant_id", "subject": "subject_label",
}

// Bookings returns a page of the bookings of every space: searched by code,
// requester, service and subject, filtered by status, day and space, and
// ordered by the server.
func (s *TeamService) Bookings(ctx context.Context, actor security.Subject, q TeamQuery) (Page, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewAll, Booking{})
	if err != nil {
		return Page{}, err
	}
	column, ok := sortableTeam[q.Sort]
	if !ok {
		return Page{}, s.labels.invalid("sort", "sort")
	}
	statuses, err := s.statuses(q.Statuses)
	if err != nil {
		return Page{}, err
	}
	if q.Tenant != "" && !security.ValidTenant(q.Tenant) {
		return Page{}, s.labels.invalid("tenant", "tenant")
	}
	var dayFrom, dayTo time.Time
	if q.Day != "" {
		day, err := time.ParseInLocation("2006-01-02", q.Day, s.cfg.Zone)
		if err != nil {
			return Page{}, s.labels.invalid("day", "day")
		}
		year, month, date := day.Date()
		dayFrom = time.Date(year, month, date, 0, 0, 0, 0, s.cfg.Zone).UTC()
		dayTo = time.Date(year, month, date+1, 0, 0, 0, 0, s.cfg.Zone).UTC()
	}
	perPage, page := s.paging(q.PerPage, q.Page)

	filtered := func() *platform.BookingQuery {
		b := platform.Bookings(s.db)
		if len(statuses) > 0 {
			b = b.WhereIn("status", statuses)
		}
		if q.Tenant != "" {
			b = b.Where("tenant_id", "=", q.Tenant)
		}
		if !dayFrom.IsZero() {
			b = b.Where("starts_at", ">=", dayFrom).Where("starts_at", "<", dayTo)
		}
		matching(b.Base(), q.Search, "code", "requester_name", "service_name", "subject_label")
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
	found, err := rows.OrderByDesc("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g, platform.Columns...)
	if err != nil {
		return Page{}, err
	}
	out := Page{Rows: make([]Summary, 0, len(found)), Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	for _, b := range found {
		out.Rows = append(out.Rows, summaryOfPlatform(b))
	}
	return out, nil
}

// Booking returns one booking of any space, with what the team wrote on it and
// the names of the team members who acted.
func (s *TeamService) Booking(ctx context.Context, actor security.Subject, id string) (Summary, error) {
	b, _, err := s.reach(ctx, actor, AgendaViewAll, id)
	if err != nil {
		return Summary{}, err
	}
	out := summaryOf(b)
	out.ReleasedByName = s.person(ctx, s.cfg.TeamTenant, b.ReleasedBy).Name
	out.CompletedByName = s.person(ctx, s.cfg.TeamTenant, b.CompletedBy).Name
	return out, nil
}

// Release lets a booking that waits for a payment through as a courtesy: it
// holds its slot for good, and who did it and why are kept on the booking and
// in the log. A booking whose hold lapsed is released only if its slot is still
// free.
func (s *TeamService) Release(ctx context.Context, actor security.Subject, id, reason string) (*Booking, error) {
	reason = trimmed(reason)
	if reason == "" {
		return nil, s.labels.invalid("reason", "required")
	}
	if utf8.RuneCountInString(reason) > maxReasonLen {
		return nil, s.labels.invalid("reason", "max", maxReplace(maxReasonLen))
	}
	b, g, err := s.reach(ctx, actor, AgendaManage, id)
	if err != nil {
		return nil, err
	}
	if !b.CanSettle() {
		return nil, s.labels.invalid("status", "not_awaiting")
	}
	err = database.Transaction(ctx, s.db, func(ctx context.Context) error {
		now := s.now()
		if err := s.lock(ctx, g, now); err != nil {
			return err
		}
		if b.Lapsed(now) {
			if err := s.holdAgain(ctx, g, b, now); err != nil {
				return err
			}
		}
		if err := b.Release(actor.ID, reason, now); err != nil {
			return s.labels.invalid("status", "not_awaiting")
		}
		_, err := b.Save(ctx, g)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityReleased, b, map[string]any{"released_by": actor.ID}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventScheduled, Booking: summaryOf(b), By: ByTeam})
	return b, nil
}

// Complete records that the team did the work. The note is the team's and the
// customer never reads it.
func (s *TeamService) Complete(ctx context.Context, actor security.Subject, id, note string) (*Booking, error) {
	note = trimmed(note)
	if utf8.RuneCountInString(note) > maxNoteLen {
		return nil, s.labels.invalid("note", "max", maxReplace(maxNoteLen))
	}
	b, g, err := s.reach(ctx, actor, AgendaManage, id)
	if err != nil {
		return nil, err
	}
	if err := b.Complete(actor.ID, note, s.now()); err != nil {
		return nil, s.labels.invalid("status", "not_completable")
	}
	if _, err := b.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityCompleted, b, map[string]any{"completed_by": actor.ID}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventCompleted, Booking: summaryOf(b), By: ByTeam})
	return b, nil
}

// NoShow records that the customer did not turn up. It is possible once the
// start has passed.
func (s *TeamService) NoShow(ctx context.Context, actor security.Subject, id string) (*Booking, error) {
	b, g, err := s.reach(ctx, actor, AgendaManage, id)
	if err != nil {
		return nil, err
	}
	if err := b.MarkNoShow(s.now()); err != nil {
		return nil, s.labels.invalid("status", "not_no_show")
	}
	if _, err := b.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityNoShow, b, nil); err != nil {
		return nil, err
	}
	return b, nil
}

// Cancel gives the booking's slot up on the team's behalf, at any time before
// it is done. The customer is told.
func (s *TeamService) Cancel(ctx context.Context, actor security.Subject, id, reason string) (*Booking, error) {
	reason = trimmed(reason)
	if utf8.RuneCountInString(reason) > maxReasonLen {
		return nil, s.labels.invalid("reason", "max", maxReplace(maxReasonLen))
	}
	b, g, err := s.reach(ctx, actor, AgendaManage, id)
	if err != nil {
		return nil, err
	}
	if err := b.Cancel(ByTeam, reason, s.now()); err != nil {
		return nil, s.labels.invalid("status", "not_changeable")
	}
	if _, err := b.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityCancelled, b, map[string]any{"side": ByTeam}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventCancelled, Booking: summaryOf(b), By: ByTeam})
	return b, nil
}

// Reschedule moves a booking to the start the team chose, at any time before it
// is done. The team is not held to the weekly hours, the notice or the horizon:
// it can fit somebody in. It is held to the closures and to the capacity, which
// are about whether the team is there. The customer is told.
func (s *TeamService) Reschedule(ctx context.Context, actor security.Subject, id string, newStart time.Time) (*Booking, error) {
	if newStart.IsZero() {
		return nil, s.labels.invalid("start", "required")
	}
	b, g, err := s.reach(ctx, actor, AgendaManage, id)
	if err != nil {
		return nil, err
	}
	if !b.CanMove() {
		return nil, s.labels.invalid("status", "not_changeable")
	}
	from := b.StartsAt
	moved, err := s.move(ctx, g, b, newStart.UTC(), false)
	if err != nil {
		return nil, err
	}
	if err := s.recordBooking(ctx, g, actor, ActivityRescheduled, moved, map[string]any{
		"from": instant(from), "to": instant(moved.StartsAt), "side": ByTeam,
	}); err != nil {
		return nil, err
	}
	s.notify(ctx, Event{Kind: EventRescheduled, Booking: summaryOf(moved), By: ByTeam})
	return moved, nil
}

// holdAgain checks that the slot of a booking whose hold lapsed is still free,
// for a payment or a release that came late: the time was offered once, and what
// can have changed since is who else holds it.
func (c *core) holdAgain(ctx context.Context, g security.Grant, b *Booking, now time.Time) error {
	from := c.dayStart(b.StartsAt.In(c.cfg.Zone))
	schedule, err := c.schedule(ctx, g, Span{Start: from, End: from.Add(48 * time.Hour)}, b.ID, now)
	if err != nil {
		return err
	}
	if !b.StartsAt.After(now) || !schedule.Free(Span{Start: b.StartsAt, End: b.EndsAt}) {
		return c.labels.taken()
	}
	return nil
}

// reach finds the booking a team member acts on and the Grant to act on it with.
//
// The policy is asked twice, as it is for a customer's reading: first for the
// kind of act, with nothing in hand, and then about the booking itself. Between
// them the row is found in any tenant -- through the Global table, with the
// realm's Grant -- and its tenant is the one the work is scoped to.
func (s *TeamService) reach(ctx context.Context, actor security.Subject, a security.Action, id string) (*Booking, security.Grant, error) {
	realm, err := security.Authorize(ctx, s.policy, actor, a, Booking{})
	if err != nil {
		return nil, security.Grant{}, err
	}
	row, err := platform.Bookings(s.db).WhereKey(id).First(ctx, realm, platform.KeyColumns...)
	if err != nil {
		return nil, security.Grant{}, err
	}
	if row == nil {
		return nil, security.Grant{}, ErrNotFound
	}
	g, err := scopedGrant(realm, a, row.TenantID)
	if err != nil {
		return nil, security.Grant{}, err
	}
	b, err := Bookings(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, security.Grant{}, err
	}
	if b == nil {
		return nil, security.Grant{}, ErrNotFound
	}
	if _, err := security.Authorize(ctx, s.policy, actor, a, *b); err != nil {
		return nil, security.Grant{}, err
	}
	return b, g, nil
}

// The settings. The team's services, weekly hours and closures belong to no
// customer, so they are read and written with the realm Grant: ViewAll reads
// them and Configure changes them.

var serviceKey = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// sortableService, sortableHour and sortableClosure are the ordering allowlists
// of the settings lists.
var (
	sortableService = map[string]string{"": "name", "name": "name", "key": "key", "duration": "duration_minutes", "payment": "requires_payment", "active": "active"}
	sortableHour    = map[string]string{"": "weekday", "weekday": "weekday", "from": "start_minute", "to": "end_minute"}
	sortableClosure = map[string]string{"": "starts_at", "from": "starts_at", "to": "ends_at", "reason": "reason"}
)

// Services returns a page of the team's services, the inactive ones too.
func (s *TeamService) Services(ctx context.Context, actor security.Subject, q ConfigQuery) (Listing[ServiceView], error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewAll, Booking{})
	if err != nil {
		return Listing[ServiceView]{}, err
	}
	column, ok := sortableService[q.Sort]
	if !ok {
		return Listing[ServiceView]{}, s.labels.invalid("sort", "sort")
	}
	perPage, page := s.paging(q.PerPage, q.Page)
	filtered := func() *ServiceQuery {
		b := Services(s.db)
		matching(b.Base(), q.Search, "name", "key", "description")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return Listing[ServiceView]{}, err
	}
	rows := filtered()
	if q.Dir == "desc" {
		rows = rows.OrderByDesc(column)
	} else {
		rows = rows.OrderBy(column)
	}
	found, err := rows.OrderBy("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return Listing[ServiceView]{}, err
	}
	out := Listing[ServiceView]{Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	for _, row := range found {
		out.Rows = append(out.Rows, serviceViewOf(row))
	}
	return out, nil
}

// CanConfigure answers whether the subject may change the team's settings, for
// the screen that draws a form. It asks the policy exactly as the change does,
// and the refusal it returns is the one the change would.
func (s *TeamService) CanConfigure(ctx context.Context, actor security.Subject) error {
	_, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	return err
}

// Service returns one service of the team.
func (s *TeamService) Service(ctx context.Context, actor security.Subject, id string) (ServiceView, error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewAll, Booking{})
	if err != nil {
		return ServiceView{}, err
	}
	row, err := Services(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return ServiceView{}, err
	}
	if row == nil {
		return ServiceView{}, ErrNotFound
	}
	return serviceViewOf(row), nil
}

// CreateService adds a service to the team's catalogue.
func (s *TeamService) CreateService(ctx context.Context, actor security.Subject, in ServiceInput) (*Service, error) {
	if errs := s.validateService(in); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return nil, err
	}
	if err := s.keyFree(ctx, g, trimmed(in.Key), ""); err != nil {
		return nil, err
	}
	now := s.now()
	row, err := Services(s.db).New()
	if err != nil {
		return nil, err
	}
	if row.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	fill(row, in)
	row.CreatedAt, row.UpdatedAt = now, now
	if _, err := row.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, entry{event: ActivityServiceCreated, refType: ActivityRefService, refID: row.ID, code: row.Key}, nil); err != nil {
		return nil, err
	}
	return row, nil
}

// UpdateService changes a service. Its key is changed with it, so the bookings
// made under the old key keep the key they were made with.
func (s *TeamService) UpdateService(ctx context.Context, actor security.Subject, id string, in ServiceInput) (*Service, error) {
	if errs := s.validateService(in); errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return nil, err
	}
	row, err := Services(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNotFound
	}
	if err := s.keyFree(ctx, g, trimmed(in.Key), row.ID); err != nil {
		return nil, err
	}
	fill(row, in)
	row.UpdatedAt = s.now()
	if _, err := row.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, entry{event: ActivityServiceUpdated, refType: ActivityRefService, refID: row.ID, code: row.Key}, nil); err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteService removes a service from the catalogue. The bookings already made
// keep the key and the name they were made with.
func (s *TeamService) DeleteService(ctx context.Context, actor security.Subject, id string) error {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return err
	}
	row, err := Services(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrNotFound
	}
	if _, err := Services(s.db).WhereKey(id).Delete(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, entry{event: ActivityServiceDeleted, refType: ActivityRefService, refID: row.ID, code: row.Key}, nil)
}

// fill copies a service's input onto its row.
func fill(row *Service, in ServiceInput) {
	row.Key = trimmed(in.Key)
	row.Name = trimmed(in.Name)
	row.Description = trimmed(in.Description)
	row.DurationMinutes = in.DurationMinutes
	row.RequiresPayment = Flag(in.RequiresPayment)
	row.Active = Flag(in.Active)
}

// keyFree reports a key another service already has.
func (s *TeamService) keyFree(ctx context.Context, g security.Grant, key, except string) error {
	q := Services(s.db).Where("key", "=", key)
	if except != "" {
		q = q.Where("id", "!=", except)
	}
	taken, err := q.Exists(ctx, g)
	if err != nil {
		return err
	}
	if taken {
		return s.labels.invalid("key", "key_taken")
	}
	return nil
}

func (s *TeamService) validateService(in ServiceInput) validation.Errors {
	errs := validation.Errors{}
	key := trimmed(in.Key)
	switch {
	case key == "":
		errs.Add("key", s.labels.message("required"))
	case utf8.RuneCountInString(key) > maxKeyLen:
		errs.Add("key", s.labels.message("max", maxReplace(maxKeyLen)))
	case !serviceKey.MatchString(key):
		errs.Add("key", s.labels.message("key_format"))
	}
	name := trimmed(in.Name)
	switch {
	case name == "":
		errs.Add("name", s.labels.message("required"))
	case utf8.RuneCountInString(name) > maxNameLen:
		errs.Add("name", s.labels.message("max", maxReplace(maxNameLen)))
	}
	if utf8.RuneCountInString(trimmed(in.Description)) > maxDescriptionLn {
		errs.Add("description", s.labels.message("max", maxReplace(maxDescriptionLn)))
	}
	if in.DurationMinutes < 5 || in.DurationMinutes > maxDuration {
		errs.Add("duration_minutes", s.labels.message("duration", translationRange(5, maxDuration)))
	}
	return errs
}

// Hours returns a page of the windows of the team's week.
func (s *TeamService) Hours(ctx context.Context, actor security.Subject, q ConfigQuery) (Listing[HourView], error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewAll, Booking{})
	if err != nil {
		return Listing[HourView]{}, err
	}
	column, ok := sortableHour[q.Sort]
	if !ok {
		return Listing[HourView]{}, s.labels.invalid("sort", "sort")
	}
	perPage, page := s.paging(q.PerPage, q.Page)
	total, err := Hours(s.db).Count(ctx, g)
	if err != nil {
		return Listing[HourView]{}, err
	}
	rows := Hours(s.db)
	if q.Dir == "desc" {
		rows = rows.OrderByDesc(column)
	} else {
		rows = rows.OrderBy(column)
	}
	found, err := rows.OrderBy("weekday").OrderBy("start_minute").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return Listing[HourView]{}, err
	}
	out := Listing[HourView]{Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	for _, row := range found {
		out.Rows = append(out.Rows, hourViewOf(row))
	}
	return out, nil
}

// CreateHour adds a window to the team's week. It cannot overlap one the same
// weekday already has.
func (s *TeamService) CreateHour(ctx context.Context, actor security.Subject, in HourInput) (*Hour, error) {
	errs := validation.Errors{}
	if in.Weekday < 0 || in.Weekday > 6 {
		errs.Add("weekday", s.labels.message("weekday"))
	}
	if in.StartMinute < 0 || in.StartMinute >= maxMinutes {
		errs.Add("from", s.labels.message("time"))
	}
	if in.EndMinute <= 0 || in.EndMinute > maxMinutes {
		errs.Add("to", s.labels.message("time"))
	}
	if !errs.Any() && in.EndMinute <= in.StartMinute {
		errs.Add("to", s.labels.message("hour_order"))
	}
	if errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return nil, err
	}
	overlap, err := Hours(s.db).Where("weekday", "=", in.Weekday).
		Where("start_minute", "<", in.EndMinute).Where("end_minute", ">", in.StartMinute).Exists(ctx, g)
	if err != nil {
		return nil, err
	}
	if overlap {
		return nil, s.labels.invalid("from", "hour_overlap")
	}
	row, err := Hours(s.db).New()
	if err != nil {
		return nil, err
	}
	if row.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	row.Weekday, row.StartMinute, row.EndMinute = in.Weekday, in.StartMinute, in.EndMinute
	row.CreatedAt = s.now()
	if _, err := row.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, entry{event: ActivityHourCreated, refType: ActivityRefHour, refID: row.ID, code: row.ID}, map[string]any{
		"weekday": row.Weekday, "start_minute": row.StartMinute, "end_minute": row.EndMinute,
	}); err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteHour removes a window from the team's week.
func (s *TeamService) DeleteHour(ctx context.Context, actor security.Subject, id string) error {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return err
	}
	row, err := Hours(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrNotFound
	}
	if _, err := Hours(s.db).WhereKey(id).Delete(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, entry{event: ActivityHourDeleted, refType: ActivityRefHour, refID: row.ID, code: row.ID}, nil)
}

// Closures returns a page of the team's closures.
func (s *TeamService) Closures(ctx context.Context, actor security.Subject, q ConfigQuery) (Listing[ClosureView], error) {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaViewAll, Booking{})
	if err != nil {
		return Listing[ClosureView]{}, err
	}
	column, ok := sortableClosure[q.Sort]
	if !ok {
		return Listing[ClosureView]{}, s.labels.invalid("sort", "sort")
	}
	perPage, page := s.paging(q.PerPage, q.Page)
	filtered := func() *ClosureQuery {
		b := Closures(s.db)
		matching(b.Base(), q.Search, "reason")
		return b
	}
	total, err := filtered().Count(ctx, g)
	if err != nil {
		return Listing[ClosureView]{}, err
	}
	rows := filtered()
	if q.Dir == "asc" {
		rows = rows.OrderBy(column)
	} else {
		rows = rows.OrderByDesc(column)
	}
	found, err := rows.OrderBy("id").Offset((page-1)*perPage).Limit(perPage).Get(ctx, g)
	if err != nil {
		return Listing[ClosureView]{}, err
	}
	out := Listing[ClosureView]{Total: int(total), Page: page, Pages: pagesOf(int(total), perPage)}
	for _, row := range found {
		out.Rows = append(out.Rows, closureViewOf(row))
	}
	return out, nil
}

// CreateClosure adds a range the team takes no booking in.
func (s *TeamService) CreateClosure(ctx context.Context, actor security.Subject, in ClosureInput) (*Closure, error) {
	errs := validation.Errors{}
	if in.StartsAt.IsZero() {
		errs.Add("starts_at", s.labels.message("required"))
	}
	if in.EndsAt.IsZero() {
		errs.Add("ends_at", s.labels.message("required"))
	}
	if !errs.Any() && !in.EndsAt.After(in.StartsAt) {
		errs.Add("ends_at", s.labels.message("closure_order"))
	}
	reason := trimmed(in.Reason)
	if utf8.RuneCountInString(reason) > maxReasonLen {
		errs.Add("reason", s.labels.message("max", maxReplace(maxReasonLen)))
	}
	if errs.Any() {
		return nil, errs
	}
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return nil, err
	}
	row, err := Closures(s.db).New()
	if err != nil {
		return nil, err
	}
	if row.ID, err = data.NewID(); err != nil {
		return nil, err
	}
	row.StartsAt, row.EndsAt, row.Reason = in.StartsAt.UTC(), in.EndsAt.UTC(), reason
	row.CreatedAt = s.now()
	if _, err := row.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.record(ctx, g, actor, entry{event: ActivityClosureCreated, refType: ActivityRefClosure, refID: row.ID, code: row.ID}, map[string]any{
		"starts_at": instant(row.StartsAt), "ends_at": instant(row.EndsAt),
	}); err != nil {
		return nil, err
	}
	return row, nil
}

// DeleteClosure removes a closure.
func (s *TeamService) DeleteClosure(ctx context.Context, actor security.Subject, id string) error {
	g, err := security.Authorize(ctx, s.policy, actor, AgendaConfigure, Booking{})
	if err != nil {
		return err
	}
	row, err := Closures(s.db).WhereKey(id).First(ctx, g)
	if err != nil {
		return err
	}
	if row == nil {
		return ErrNotFound
	}
	if _, err := Closures(s.db).WhereKey(id).Delete(ctx, g); err != nil {
		return err
	}
	return s.record(ctx, g, actor, entry{event: ActivityClosureDeleted, refType: ActivityRefClosure, refID: row.ID, code: row.ID}, nil)
}
