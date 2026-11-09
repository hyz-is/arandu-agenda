package agenda

import (
	"net/url"
	"strconv"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/kyse/components"
)

// This file builds what the screens are handed. Like pages.go it reads nothing
// and decides nothing: the handler already asked the service, and these only
// ask the policy what to draw -- a button for an act the subject may do -- never
// whether to do it.

// recordOf is a booking as the policy is asked about it: the fields it decides
// on, and no connection.
func recordOf(b Summary) Booking {
	return Booking{
		ID: b.ID, TenantID: b.Tenant, RequesterID: b.RequesterID, Status: b.Status,
		StartsAt: b.StartsAt, EndsAt: b.EndsAt, HoldUntil: b.HoldUntil,
	}
}

// allows says whether a customer may do an act on a booking, to decide whether a
// button is drawn. It cannot authorize anything: it throws the Grant away, and
// the route behind the button asks the policy again.
func (m *Module) allows(ctx *fhttp.Context, who security.Subject, a security.Action, record Booking) bool {
	return auth.Allows(ctx.Ctx(), BookingPolicy{}, who, a, record)
}

// teamAllows is allows for a member of the team.
func (m *Module) teamAllows(ctx *fhttp.Context, who security.Subject, a security.Action) bool {
	return auth.Allows(ctx.Ctx(), TeamPolicy{Realm: m.cfg.TeamTenant}, who, a, Booking{})
}

func (m *Module) indexPage(ctx *fhttp.Context, sc Screen, who security.Subject, p params, page Page) IndexPageData {
	u := m.URLs()
	rows := make([]BookingRow, 0, len(page.Rows))
	for _, b := range page.Rows {
		rows = append(rows, sc.bookingRow(b, u.Show(b.ID)))
	}
	chosen := make([]string, 0, len(p.statuses))
	for _, s := range p.statuses {
		chosen = append(chosen, string(s))
	}
	return IndexPageData{
		Screen: sc, Rows: rows, Query: p.search, Statuses: chosen, SortKey: p.sort, SortDir: p.dir,
		URL: u.Index(), NewURL: u.New(), CanBook: m.allows(ctx, who, AgendaBook, Booking{}),
		Pager:       pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.Index(), listQueryValues(p)),
		CurrentPage: page.Page, Pages: page.Pages, Total: page.Total, PerPage: m.cfg.PageSize,
		StatusOptions: sc.StatusOptions(),
	}
}

// dayBounds are the first and the last day a booking can be asked for, as the
// date fields write them.
func (m *Module) dayBounds(now time.Time) (string, string) {
	first := now.Add(m.cfg.MinNotice).In(m.cfg.Zone)
	last := now.Add(m.cfg.Horizon).In(m.cfg.Zone)
	return first.Format("2006-01-02"), last.Format("2006-01-02")
}

// slotOptions are the free starts of a day as the options of a set of radios,
// each valued with the instant in UTC and labelled with the time of day.
func (m *Module) slotOptions(sc Screen, slots []time.Time) []components.RadioOption {
	out := make([]components.RadioOption, 0, len(slots))
	for _, start := range slots {
		out = append(out, components.RadioOption{Label: sc.Clock(start), Value: start.UTC().Format(time.RFC3339)})
	}
	return out
}

// serviceOptions are the services a customer can book as the options of a
// select.
func serviceOptions(services []ServiceView) []components.SelectOption {
	out := make([]components.SelectOption, 0, len(services))
	for _, s := range services {
		out = append(out, components.SelectOption{Value: s.Key, Label: s.Name})
	}
	return out
}

// serviceNote is the line under the service: how long it takes, and whether it
// asks for a payment.
func (sc Screen) serviceNote(s ServiceView) string {
	key := "screen.service_note_free"
	if s.RequiresPayment {
		key = "screen.service_note_paid"
	}
	return sc.Labels.line(key, map[string]any{"minutes": s.DurationMinutes})
}

func (m *Module) newPage(sc Screen, now time.Time, services []ServiceView, p newParams, slots []time.Time) NewPageData {
	u := m.URLs()
	minDay, maxDay := m.dayBounds(now)
	data := NewPageData{
		Screen: sc, Action: u.Index(), FormURL: u.New(), BackURL: u.Index(),
		ServiceOptions: serviceOptions(services), NoServices: len(services) == 0,
		ServiceKey: p.service, Day: p.day, MinDay: minDay, MaxDay: maxDay, Searched: p.day != "" && p.service != "",
		Slots: m.slotOptions(sc, slots), Subject: p.subject,
	}
	for _, s := range services {
		if s.Key == p.service {
			data.ServiceNote, data.RequiresPayment = sc.serviceNote(s), s.RequiresPayment
		}
	}
	return data
}

func (m *Module) showPage(ctx *fhttp.Context, sc Screen, who security.Subject, b Summary) ShowPageData {
	u := m.URLs()
	record := recordOf(b)
	now := m.cfg.Now().UTC()
	open := record.ChangeOpen(now, m.cfg.ChangeUntil)
	waiting := record.Status == StatusAwaitingPayment && record.Occupies(now)
	page := ShowPageData{
		Screen: sc, Booking: sc.bookingRow(b, u.Show(b.ID)), BackURL: u.Index(),
		ConfirmURL: u.Confirm(b.ID), RescheduleURL: u.Reschedule(b.ID), CancelURL: u.Cancel(b.ID),
		CanConfirm:      record.CanConfirm(now) && m.allows(ctx, who, AgendaChange, record),
		CanReschedule:   open && m.allows(ctx, who, AgendaChange, record),
		CanCancel:       open && m.allows(ctx, who, AgendaChange, record),
		AwaitingPayment: waiting,
		ChangeClosed:    record.CanMove() && !open,
		HoldExpired:     record.Status == StatusAwaitingPayment && !waiting,
	}
	if waiting && m.cfg.PaymentURL != nil {
		page.PaymentURL = m.cfg.PaymentURL(b)
	}
	return page
}

func (m *Module) reschedulePage(sc Screen, now time.Time, b Summary, day string, slots []time.Time) ReschedulePageData {
	u := m.URLs()
	minDay, maxDay := m.dayBounds(now)
	return ReschedulePageData{
		Screen: sc, Booking: sc.bookingRow(b, u.Show(b.ID)), Action: u.Reschedule(b.ID), FormURL: u.Reschedule(b.ID),
		BackURL: u.Show(b.ID), Day: day, MinDay: minDay, MaxDay: maxDay, Searched: day != "", Slots: m.slotOptions(sc, slots),
	}
}

func (m *Module) teamIndexPage(ctx *fhttp.Context, sc Screen, who security.Subject, p params, day string, page Page) TeamIndexPageData {
	u := m.URLs()
	rows := make([]BookingRow, 0, len(page.Rows))
	for _, b := range page.Rows {
		rows = append(rows, sc.bookingRow(b, u.TeamShow(b.ID)))
	}
	statuses := make([]string, 0, len(p.statuses))
	for _, s := range p.statuses {
		statuses = append(statuses, string(s))
	}
	query := listQueryValues(p)
	if day != "" {
		query.Set("day", day)
	}
	return TeamIndexPageData{
		Screen: sc, Rows: rows, Query: p.search, Statuses: statuses, Day: day,
		SortKey: p.sort, SortDir: p.dir, URL: u.TeamIndex(),
		Pager:       pager(len(rows), page.Total, m.cfg.PageSize, page.Page, u.TeamIndex(), query),
		CurrentPage: page.Page, Pages: page.Pages, Total: page.Total, PerPage: m.cfg.PageSize,
		StatusOptions: sc.StatusOptions(),
		ServicesURL:   u.TeamServices(), HoursURL: u.TeamHours(), ClosuresURL: u.TeamClosures(),
		CanConfigure: m.teamAllows(ctx, who, AgendaConfigure),
	}
}

func (m *Module) teamShowPage(ctx *fhttp.Context, sc Screen, who security.Subject, b Summary) TeamShowPageData {
	u := m.URLs()
	record := recordOf(b)
	now := m.cfg.Now().UTC()
	manage := m.teamAllows(ctx, who, AgendaManage)
	local := b.StartsAt.In(m.cfg.Zone)
	return TeamShowPageData{
		Screen: sc, Booking: sc.bookingRow(b, u.TeamShow(b.ID)), BackURL: u.TeamIndex(),
		CompleteURL: u.TeamComplete(b.ID), ReleaseURL: u.TeamRelease(b.ID), NoShowURL: u.TeamNoShow(b.ID),
		CancelURL: u.TeamCancel(b.ID), RescheduleURL: u.TeamReschedule(b.ID),
		CanComplete:   record.CanComplete() && manage,
		CanRelease:    record.CanSettle() && manage,
		CanNoShow:     record.CanNoShow(now) && manage,
		CanCancel:     record.CanMove() && manage,
		CanReschedule: record.CanMove() && manage,
		Day:           local.Format("2006-01-02"), Time: local.Format("15:04"),
		MinDay: now.In(m.cfg.Zone).Format("2006-01-02"),
	}
}

// settingsNav are the addresses of the three settings pages.
func (m *Module) settingsNav() (services, hours, closures string) {
	u := m.URLs()
	return u.TeamServices(), u.TeamHours(), u.TeamClosures()
}

func (m *Module) servicesPage(ctx *fhttp.Context, sc Screen, who security.Subject, p configParams, list Listing[ServiceView]) ServicesPageData {
	u := m.URLs()
	rows := make([]ServiceRow, 0, len(list.Rows))
	for _, s := range list.Rows {
		rows = append(rows, ServiceRow{
			ID: s.ID, Key: s.Key, Name: s.Name, Description: s.Description,
			Duration: sc.Labels.line("screen.minutes", map[string]any{"minutes": s.DurationMinutes}),
			Payment:  sc.Labels.yesNo(s.RequiresPayment), Active: sc.Labels.yesNo(s.Active),
			EditURL: u.TeamService(s.ID), DeleteURL: u.TeamService(s.ID),
		})
	}
	services, hours, closures := m.settingsNav()
	return ServicesPageData{
		Screen: sc, Rows: rows, Query: p.search, SortKey: p.sort, SortDir: p.dir, URL: u.TeamServices(), NewURL: u.TeamServiceNew(),
		Pager:       pager(len(rows), list.Total, m.cfg.PageSize, list.Page, u.TeamServices(), p.values()),
		CurrentPage: list.Page, Pages: list.Pages, Total: list.Total, PerPage: m.cfg.PageSize,
		CanConfigure: m.teamAllows(ctx, who, AgendaConfigure), ServicesURL: services, HoursURL: hours, ClosuresURL: closures,
	}
}

func (m *Module) hoursPage(ctx *fhttp.Context, sc Screen, who security.Subject, p configParams, list Listing[HourView]) HoursPageData {
	u := m.URLs()
	rows := make([]HourRow, 0, len(list.Rows))
	for _, h := range list.Rows {
		rows = append(rows, HourRow{
			ID: h.ID, Weekday: sc.Labels.Weekday(h.Weekday), From: Minutes(h.StartMinute), To: Minutes(h.EndMinute),
			DeleteURL: u.TeamHour(h.ID),
		})
	}
	services, hours, closures := m.settingsNav()
	return HoursPageData{
		Screen: sc, Rows: rows, SortKey: p.sort, SortDir: p.dir, URL: u.TeamHours(), NewURL: u.TeamHourNew(),
		Pager:       pager(len(rows), list.Total, m.cfg.PageSize, list.Page, u.TeamHours(), p.values()),
		CurrentPage: list.Page, Pages: list.Pages, Total: list.Total, PerPage: m.cfg.PageSize,
		CanConfigure: m.teamAllows(ctx, who, AgendaConfigure), ServicesURL: services, HoursURL: hours, ClosuresURL: closures,
	}
}

func (m *Module) closuresPage(ctx *fhttp.Context, sc Screen, who security.Subject, p configParams, list Listing[ClosureView]) ClosuresPageData {
	u := m.URLs()
	rows := make([]ClosureRow, 0, len(list.Rows))
	for _, c := range list.Rows {
		rows = append(rows, ClosureRow{ID: c.ID, From: sc.When(c.StartsAt), To: sc.When(c.EndsAt), Reason: c.Reason, DeleteURL: u.TeamClosure(c.ID)})
	}
	services, hours, closures := m.settingsNav()
	return ClosuresPageData{
		Screen: sc, Rows: rows, Query: p.search, SortKey: p.sort, SortDir: p.dir, URL: u.TeamClosures(), NewURL: u.TeamClosureNew(),
		Pager:       pager(len(rows), list.Total, m.cfg.PageSize, list.Page, u.TeamClosures(), p.values()),
		CurrentPage: list.Page, Pages: list.Pages, Total: list.Total, PerPage: m.cfg.PageSize,
		CanConfigure: m.teamAllows(ctx, who, AgendaConfigure), ServicesURL: services, HoursURL: hours, ClosuresURL: closures,
	}
}

// listQueryValues are the parameters a list carries onto its page links.
func listQueryValues(p params) url.Values {
	values := url.Values{}
	if p.search != "" {
		values.Set("q", p.search)
	}
	for _, s := range p.statuses {
		values.Add("status", string(s))
	}
	if p.sort != "" {
		values.Set("sort", p.sort)
	}
	if p.dir != "" {
		values.Set("dir", p.dir)
	}
	return values
}

// values are the parameters a settings list carries onto its page links.
func (p configParams) values() url.Values {
	values := url.Values{}
	if p.search != "" {
		values.Set("q", p.search)
	}
	if p.sort != "" {
		values.Set("sort", p.sort)
	}
	if p.dir != "" {
		values.Set("dir", p.dir)
	}
	return values
}

// minutesOf reads a time of day, "09:00", as minutes since midnight.
func minutesOf(value string) (int, bool) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, false
	}
	return parsed.Hour()*60 + parsed.Minute(), true
}

// atoi reads a whole number, and zero when it is not one.
func atoi(value string) int {
	n, _ := strconv.Atoi(trimmed(value))
	return n
}
