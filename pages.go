package agenda

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	fview "github.com/arandu-io/framework/view"
	"github.com/arandu-io/hesape/pagination"
	hview "github.com/arandu-io/hesape/view"
	"github.com/arandu-io/kyse/components"
)

// What the screens show, built from what the services answer. Nothing here
// reads the database or asks the policy: the handler already did both, and
// these only turn statuses, instants and minutes into what a page draws.

// The screens this package renders, by the name their path gives them.
const (
	ViewIndex           = "modules.agenda.index"
	ViewNew             = "modules.agenda.form"
	ViewShow            = "modules.agenda.show"
	ViewReschedule      = "modules.agenda.reschedule"
	ViewTeamIndex       = "modules.agenda.admin.index"
	ViewTeamShow        = "modules.agenda.admin.show"
	ViewTeamServices    = "modules.agenda.admin.services"
	ViewTeamServiceForm = "modules.agenda.admin.service_form"
	ViewTeamHours       = "modules.agenda.admin.hours"
	ViewTeamHourForm    = "modules.agenda.admin.hour_form"
	ViewTeamClosures    = "modules.agenda.admin.closures"
	ViewTeamClosureForm = "modules.agenda.admin.closure_form"
)

// URLs are the addresses of the routes under the module's configuration. A link
// is built from here and never written out a second time: under another
// Prefix the paths move and the names do not.
type URLs struct{ cfg Config }

func (u URLs) show(base, id string) string { return base + "/" + url.PathEscape(id) }

// Index is the customer's list.
func (u URLs) Index() string { return u.cfg.Prefix }

// New is the form that books, and where a day's free times are asked for.
func (u URLs) New() string { return u.cfg.Prefix + u.cfg.Paths.New }

// NewFor is New for a service and a subject, so the application can link a
// customer to the form with the booking already about something:
// NewFor("reconfiguracao", BookingSubject{Type: "camera", ID: "42", Label: "Portaria"}).
func (u URLs) NewFor(service string, subject BookingSubject) string {
	values := url.Values{}
	if service != "" {
		values.Set("service", service)
	}
	if subject.Type != "" {
		values.Set("subject_type", subject.Type)
	}
	if subject.ID != "" {
		values.Set("subject_id", subject.ID)
	}
	if subject.Label != "" {
		values.Set("subject_label", subject.Label)
	}
	if len(values) == 0 {
		return u.New()
	}
	return u.New() + "?" + values.Encode()
}

// Show is one booking, for the customer.
func (u URLs) Show(id string) string { return u.show(u.cfg.Prefix, id) }

// Confirm is where the customer posts a confirmation.
func (u URLs) Confirm(id string) string { return u.Show(id) + u.cfg.Paths.Confirmation }

// Reschedule is the customer's form that moves a booking, and where it is
// posted.
func (u URLs) Reschedule(id string) string { return u.Show(id) + u.cfg.Paths.Rescheduling }

// Cancel is where the customer posts a cancellation.
func (u URLs) Cancel(id string) string { return u.Show(id) + u.cfg.Paths.Cancellation }

// TeamIndex is the team's list.
func (u URLs) TeamIndex() string { return u.cfg.TeamPrefix }

// TeamShow is one booking, for the team.
func (u URLs) TeamShow(id string) string { return u.show(u.cfg.TeamPrefix, id) }

// TeamComplete is where the team posts that the work was done.
func (u URLs) TeamComplete(id string) string { return u.TeamShow(id) + u.cfg.Paths.Completion }

// TeamRelease is where the team posts a courtesy.
func (u URLs) TeamRelease(id string) string { return u.TeamShow(id) + u.cfg.Paths.Release }

// TeamNoShow is where the team posts an absence.
func (u URLs) TeamNoShow(id string) string { return u.TeamShow(id) + u.cfg.Paths.NoShow }

// TeamCancel is where the team posts a cancellation.
func (u URLs) TeamCancel(id string) string { return u.TeamShow(id) + u.cfg.Paths.Cancellation }

// TeamReschedule is where the team posts a new time.
func (u URLs) TeamReschedule(id string) string { return u.TeamShow(id) + u.cfg.Paths.Rescheduling }

func (u URLs) settings(segment string) string {
	return u.cfg.TeamPrefix + u.cfg.Paths.Settings + segment
}

// TeamServices is the team's list of services.
func (u URLs) TeamServices() string { return u.settings(u.cfg.Paths.Services) }

// TeamServiceNew is the form that adds a service.
func (u URLs) TeamServiceNew() string { return u.TeamServices() + u.cfg.Paths.New }

// TeamService is one service: where it is edited, saved and deleted.
func (u URLs) TeamService(id string) string { return u.show(u.TeamServices(), id) }

// TeamHours is the team's list of weekly windows.
func (u URLs) TeamHours() string { return u.settings(u.cfg.Paths.Hours) }

// TeamHourNew is the form that adds a window.
func (u URLs) TeamHourNew() string { return u.TeamHours() + u.cfg.Paths.New }

// TeamHour is one window: where it is deleted.
func (u URLs) TeamHour(id string) string { return u.show(u.TeamHours(), id) }

// TeamClosures is the team's list of closures.
func (u URLs) TeamClosures() string { return u.settings(u.cfg.Paths.Closures) }

// TeamClosureNew is the form that adds a closure.
func (u URLs) TeamClosureNew() string { return u.TeamClosures() + u.cfg.Paths.New }

// TeamClosure is one closure: where it is deleted.
func (u URLs) TeamClosure(id string) string { return u.show(u.TeamClosures(), id) }

// Screen is what every page of this package carries: the page hesape's view.New
// builds, the application's chrome, and the sentences.
//
// It embeds hesape's view.Page, so every page of this package is a
// hesape/view.Layout -- what the layouts of an application on Arandu framework
// v0.62 and newer assert -- and it answers HasErrors and ErrorSummary as well,
// so it is also the framework/view.Layout an older layout asks for.
type Screen struct {
	hview.Page
	Labels Labels
	// URLs are the addresses of the package's routes.
	URLs URLs
	// Location is the timezone instants are written in.
	Location *time.Location

	chrome any
}

// Compile-time proof that a page of this package fits both layout contracts.
var (
	_ hview.Layout   = Screen{}
	_ fview.Layout   = Screen{}
	_ slog.LogValuer = Screen{}
)

// Chrome is what Config.Chrome answered for this screen: the application's
// sidebar, navigation and account menu, in whatever type its layout draws from.
// A layout asserts that type on it; nil when Config.Chrome is nil.
func (s Screen) Chrome() any { return s.chrome }

// HasErrors says whether the attempt that landed here was rejected:
// framework/view.Layout's name for hesape's Any.
func (s Screen) HasErrors() bool { return s.Page.Any() }

// All is one line per rejected message, for the banner above a form, in the
// screen's language: the field is named by its label in the catalogue
// ("Serviço: Preencha este campo.").
func (s Screen) All() []string { return s.Labels.Summary(s.Page.Errors) }

// ErrorSummary is one line per rejected field: framework/view.Layout's name for
// All.
func (s Screen) ErrorSummary() []string { return s.All() }

// FieldErrors is every message for one field.
func (s Screen) FieldErrors(name string) []string { return s.Page.Get(name) }

// MarshalJSON writes the page with the CSRF token redacted, as
// framework/view.Page does: a page dumped for debugging never publishes the
// token. It answers for every page data that embeds Screen.
func (s Screen) MarshalJSON() ([]byte, error) { return framePage(s.Page).MarshalJSON() }

// LogValue is the page as a log line carries it, the token redacted.
func (s Screen) LogValue() slog.Value { return framePage(s.Page).LogValue() }

// Form is the form state of the page.
func (s Screen) Form() FormState { return FormState{Page: s.Page} }

// FormState is what a form field reads from the page it is drawn on: what was
// typed before a refusal, and the message for each field.
type FormState struct{ hview.Page }

// FieldError is the first message for one field, empty when it was accepted.
func (f FormState) FieldError(name string) string { return f.Page.FieldError(name) }

// HasErrors says whether the attempt that landed here was rejected.
func (f FormState) HasErrors() bool { return f.Page.Any() }

// ErrorSummary is one line per rejected field.
func (f FormState) ErrorSummary() []string { return f.Page.All() }

// FieldErrors is every message for one field.
func (f FormState) FieldErrors(name string) []string { return f.Page.Get(name) }

// MarshalJSON writes the form state with the CSRF token redacted.
func (f FormState) MarshalJSON() ([]byte, error) { return framePage(f.Page).MarshalJSON() }

// LogValue is the form state as a log line carries it, the token redacted.
func (f FormState) LogValue() slog.Value { return framePage(f.Page).LogValue() }

// framePage copies a hesape page into framework/view.Page, by name, for the two
// methods that redact the token there.
func framePage(p hview.Page) fview.Page {
	return fview.Page{
		Title: p.Title, Description: p.Description, Canonical: p.Canonical, AppName: p.AppName, Token: p.Token,
		Authenticated: p.Authenticated, UserName: p.UserName,
		HomeURL: p.HomeURL, LoginURL: p.LoginURL, LogoutURL: p.LogoutURL, RegisterURL: p.RegisterURL,
		PanelURL: p.PanelURL, AdminURL: p.AdminURL,
		Impersonating: p.Impersonating, ImpersonationActs: p.ImpersonationActs, Impersonator: p.Impersonator,
		LeaveImpersonationURL: p.LeaveImpersonationURL,
		Path:                  p.Path, Errors: p.Errors, Old: p.Old, Messages: p.Messages,
	}
}

// TableLines are the sentences a data table on these screens says, every one
// from the package's catalogue (the keys under "table"), so the table speaks the
// screen's language and not the English underneath. The placeholders ({label},
// {total}, {n}, {column}) are the table's own and are filled when it draws them.
func (s Screen) TableLines() components.DataTableLines {
	l := s.Labels
	return components.DataTableLines{
		Filter: l.T("table.filter"), NoResult: l.T("table.no_result"), Rows: l.T("table.rows"),
		SelectedCount: l.T("table.selected_count"), RowsPerPage: l.T("table.rows_per_page"), PageOf: l.T("table.page_of"),
		First: l.T("table.first"), Previous: l.T("table.previous"), Next: l.T("table.next"), Last: l.T("table.last"),
		Back: l.T("table.back"), Forward: l.T("table.forward"),
		SelectAll: l.T("table.select_all"), SelectRow: l.T("table.select_row"), Bulk: l.T("table.bulk"), Apply: l.T("table.apply"),
		Ascending: l.T("table.ascending"), Descending: l.T("table.descending"), Unsorted: l.T("table.unsorted"),
		Reset: l.T("table.reset"), ClearFilters: l.T("table.clear_filters"),
		NoFacetResults: l.T("table.no_facet_results"), FacetSelected: l.T("table.facet_selected"),
		OpenMenu: l.T("table.open_menu"), Actions: l.T("table.actions"), ToggleColumns: l.T("table.toggle_columns"),
		Asc: l.T("table.asc"), Desc: l.T("table.desc"), Hide: l.T("table.hide"),
	}
}

// SearchLabel is the name a screen reader gives a table's search box:
// "Buscar em Agendamentos".
func (s Screen) SearchLabel(table string) string {
	return s.Labels.line("control.search_label", map[string]any{"label": table})
}

// StatusOptions are the statuses as options of a filter or a select.
func (s Screen) StatusOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, len(Statuses))
	for _, status := range Statuses {
		out = append(out, components.SelectOption{Value: string(status), Label: s.Labels.Status(status)})
	}
	return out
}

// WeekdayOptions are the days of the week as options of a select, Sunday first.
func (s Screen) WeekdayOptions() []components.SelectOption {
	out := make([]components.SelectOption, 0, 7)
	for day := 0; day < 7; day++ {
		out = append(out, components.SelectOption{Value: strconv.Itoa(day), Label: s.Labels.Weekday(day)})
	}
	return out
}

// DatePicker is a date field with the Kyse calendar beside it, in the screen's
// language. The field is still a date input: it holds the value, submits it as
// 2026-11-10 whatever the reader sees, and the browser draws it in the reader's
// own order (dd/mm/aaaa in Brazil). The calendar writes into it. What the
// caller already set is kept.
func (s Screen) DatePicker(p components.DateTimePickerProps) template.HTML {
	p.Kind = "date"
	p.Calendar = true
	if p.MonthNames == nil {
		p.MonthNames = s.Labels.list("month", 1, 12)
	}
	if p.WeekdayNames == nil {
		p.WeekdayNames = s.Labels.list("weekday_short", 0, 6)
	}
	if p.OpenLabel == "" {
		p.OpenLabel = s.Labels.line("calendar.open", map[string]any{"label": p.Label})
	}
	if p.TodayLabel == "" {
		p.TodayLabel = s.Labels.T("calendar.today")
	}
	if p.ClearLabel == "" {
		p.ClearLabel = s.Labels.T("calendar.clear")
	}
	return components.DateTimePicker(p)
}

// TimePicker is a time field, in whole minutes.
func (s Screen) TimePicker(p components.DateTimePickerProps) template.HTML {
	p.Kind = "time"
	if p.Step == "" {
		p.Step = "60"
	}
	return components.DateTimePicker(p)
}

// When is an instant as Brazilians write it, in the screen's timezone.
func (s Screen) When(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(s.zone()).Format("02/01/2006 15:04")
}

// Day is the date of an instant, in the screen's timezone.
func (s Screen) Day(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(s.zone()).Format("02/01/2006")
}

// Clock is the time of day of an instant, in the screen's timezone.
func (s Screen) Clock(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(s.zone()).Format("15:04")
}

func (s Screen) zone() *time.Location {
	if s.Location == nil {
		return time.UTC
	}
	return s.Location
}

// BookingRow is one booking as a list or a page shows it.
type BookingRow struct {
	ID, Code             string
	Service, ServiceKey  string
	Subject, SubjectType string
	SubjectID            string
	Status, StatusKey    string
	Day, Time, Ends      string
	When                 string
	Requester            string
	RequesterID          string
	Space                string
	Note                 string
	PaymentRef           string
	HoldUntil            string
	Confirmed, Reminded  string
	Completed            string
	CompletedBy          string
	TeamNote             string
	ReleasedBy           string
	ReleasedReason       string
	CancelReason         string
	CancelledBy          string
	Created, Updated     string
	// StartsSort orders like the instant it stands for.
	StartsSort  string
	Reschedules int
	URL         string
}

// bookingRow is a booking as a screen shows it.
func (s Screen) bookingRow(b Summary, open string) BookingRow {
	requester := b.RequesterName
	if requester == "" {
		requester = b.RequesterID
	}
	completedBy := b.CompletedByName
	if completedBy == "" {
		completedBy = b.CompletedBy
	}
	releasedBy := b.ReleasedByName
	if releasedBy == "" {
		releasedBy = b.ReleasedBy
	}
	row := BookingRow{
		ID: b.ID, Code: b.Code, Service: b.ServiceName, ServiceKey: b.ServiceKey,
		Subject: b.Subject.Label, SubjectType: b.Subject.Type, SubjectID: b.Subject.ID,
		Status: s.Labels.Status(b.Status), StatusKey: string(b.Status),
		Day: s.Day(b.StartsAt), Time: s.Clock(b.StartsAt), Ends: s.Clock(b.EndsAt), When: s.When(b.StartsAt),
		Requester: requester, RequesterID: b.RequesterID, Space: b.Tenant, Note: b.CustomerNote,
		PaymentRef: b.PaymentRef, TeamNote: b.TeamNote, CompletedBy: completedBy,
		ReleasedBy: releasedBy, ReleasedReason: b.ReleasedReason, CancelReason: b.CancelReason,
		Created: s.When(b.CreatedAt), Updated: s.When(b.UpdatedAt),
		StartsSort:  b.StartsAt.UTC().Format(time.RFC3339),
		Reschedules: b.Reschedules, URL: open,
	}
	if row.Service == "" {
		row.Service = b.ServiceKey
	}
	if b.CancelledBy != "" {
		row.CancelledBy = s.Labels.By(b.CancelledBy)
	}
	for target, at := range map[*string]*time.Time{&row.HoldUntil: b.HoldUntil, &row.Confirmed: b.ConfirmedAt, &row.Reminded: b.RemindedAt, &row.Completed: b.CompletedAt} {
		if at != nil {
			*target = s.When(*at)
		}
	}
	return row
}

// IndexPageData is the customer's list, a page at a time, filtered, searched
// and ordered on the server.
type IndexPageData struct {
	Screen
	Rows          []BookingRow
	Query         string
	Statuses      []string
	SortKey       string
	SortDir       string
	URL, NewURL   string
	CanBook       bool
	Pager         components.Paginator
	CurrentPage   int
	Pages, Total  int
	PerPage       int
	StatusOptions []components.SelectOption
}

// TableState is the customer's list as the data table draws its addresses from:
// what is searched, the order in force (the next start first when nobody
// ordered), the statuses chosen, the page and its size. Every control of the
// table builds its address from it, so a step keeps the others.
func (d IndexPageData) TableState() components.DataTableState {
	return components.DataTableValues{
		Q: d.Query, Sort: orderKey(d.SortKey), Dir: orderDir(d.SortDir),
		Facets:      map[string][]string{"status": d.Statuses},
		CurrentPage: d.CurrentPage, Size: d.PerPage, DefaultSize: d.PerPage,
	}
}

// orderKey and orderDir are the order in force; a list nobody ordered is
// ordered by the start, latest first.
func orderKey(key string) string {
	if key == "" {
		return "starts_at"
	}
	return key
}

func orderDir(dir string) string {
	if dir == "asc" {
		return "asc"
	}
	return "desc"
}

// NewPageData is the form that books: the service and the day the customer is
// looking at, and the free times of that day.
type NewPageData struct {
	Screen
	// Action is where the booking is posted, and FormURL where the free times of
	// a day are asked for.
	Action, FormURL, BackURL string
	ServiceOptions           []components.SelectOption
	ServiceKey               string
	// ServiceNote says how long the service takes and whether it asks for a
	// payment.
	ServiceNote     string
	RequiresPayment bool
	NoServices      bool
	Day             string
	MinDay, MaxDay  string
	// Searched says a day was chosen, so an empty list of times means none are
	// free and not that nobody asked.
	Searched bool
	Slots    []components.RadioOption
	Subject  BookingSubject
}

// ShowPageData is one booking, for the customer.
type ShowPageData struct {
	Screen
	Booking                              BookingRow
	BackURL                              string
	ConfirmURL, RescheduleURL, CancelURL string
	CanConfirm, CanReschedule, CanCancel bool
	AwaitingPayment                      bool
	PaymentURL                           string
	ChangeClosed                         bool
	HoldExpired                          bool
}

// ReschedulePageData is the customer's form that moves a booking.
type ReschedulePageData struct {
	Screen
	Booking                  BookingRow
	Action, FormURL, BackURL string
	Day                      string
	MinDay, MaxDay           string
	Searched                 bool
	Slots                    []components.RadioOption
}

// TeamIndexPageData is the team's list, a page at a time, filtered, searched
// and ordered on the server.
type TeamIndexPageData struct {
	Screen
	Rows          []BookingRow
	Query         string
	Statuses      []string
	Day           string
	SortKey       string
	SortDir       string
	URL           string
	Pager         components.Paginator
	CurrentPage   int
	Pages, Total  int
	PerPage       int
	StatusOptions []components.SelectOption
	// SettingsURLs are the team's settings pages, for the links above the list.
	ServicesURL, HoursURL, ClosuresURL string
	CanConfigure                       bool
}

// TableState is the team's list as the data table draws its addresses from; see
// IndexPageData.TableState. The day travels as a facet, so a step of the table
// keeps it.
func (d TeamIndexPageData) TableState() components.DataTableState {
	facets := map[string][]string{"status": d.Statuses}
	if d.Day != "" {
		facets["day"] = []string{d.Day}
	}
	return components.DataTableValues{
		Q: d.Query, Sort: orderKey(d.SortKey), Dir: orderDir(d.SortDir), Facets: facets,
		CurrentPage: d.CurrentPage, Size: d.PerPage, DefaultSize: d.PerPage,
	}
}

// TeamShowPageData is one booking, for the team, with what the team wrote and
// the controls that act on it.
type TeamShowPageData struct {
	Screen
	Booking                                                      BookingRow
	BackURL                                                      string
	CompleteURL, ReleaseURL, NoShowURL, CancelURL, RescheduleURL string
	CanComplete, CanRelease, CanNoShow, CanCancel, CanReschedule bool
	Day, Time                                                    string
	MinDay                                                       string
}

// ServiceRow is a service as the settings list shows it.
type ServiceRow struct {
	ID, Key, Name, Description string
	Duration                   string
	Payment, Active            string
	EditURL, DeleteURL         string
}

// ServicesPageData is the team's list of services.
type ServicesPageData struct {
	Screen
	Rows                               []ServiceRow
	Query                              string
	SortKey, SortDir                   string
	URL, NewURL                        string
	Pager                              components.Paginator
	CurrentPage                        int
	Pages, Total                       int
	PerPage                            int
	CanConfigure                       bool
	ServicesURL, HoursURL, ClosuresURL string
}

// TableState is the list as the data table draws its addresses from.
func (d ServicesPageData) TableState() components.DataTableState {
	return components.DataTableValues{
		Q: d.Query, Sort: configKey(d.SortKey, "name"), Dir: configDir(d.SortDir, "asc"),
		CurrentPage: d.CurrentPage, Size: d.PerPage, DefaultSize: d.PerPage,
	}
}

func configKey(key, fallback string) string {
	if key == "" {
		return fallback
	}
	return key
}

func configDir(dir, fallback string) string {
	if dir == "asc" || dir == "desc" {
		return dir
	}
	return fallback
}

// ServiceFormPageData is the form that adds or changes a service.
type ServiceFormPageData struct {
	Screen
	Action, BackURL string
	Editing         bool
	Key             string
	Name            string
	Description     string
	Duration        string
	RequiresPayment bool
	Active          bool
}

// HourRow is a window of the week as the settings list shows it.
type HourRow struct {
	ID, Weekday, From, To string
	DeleteURL             string
}

// HoursPageData is the team's list of weekly windows.
type HoursPageData struct {
	Screen
	Rows                               []HourRow
	SortKey, SortDir                   string
	URL, NewURL                        string
	Pager                              components.Paginator
	CurrentPage                        int
	Pages, Total                       int
	PerPage                            int
	CanConfigure                       bool
	ServicesURL, HoursURL, ClosuresURL string
}

// TableState is the list as the data table draws its addresses from.
func (d HoursPageData) TableState() components.DataTableState {
	return components.DataTableValues{
		Sort: configKey(d.SortKey, "weekday"), Dir: configDir(d.SortDir, "asc"),
		CurrentPage: d.CurrentPage, Size: d.PerPage, DefaultSize: d.PerPage,
	}
}

// HourFormPageData is the form that adds a window.
type HourFormPageData struct {
	Screen
	Action, BackURL string
	WeekdayOptions  []components.SelectOption
	Weekday         string
	Start, End      string
}

// ClosureRow is a closure as the settings list shows it.
type ClosureRow struct {
	ID, From, To, Reason string
	DeleteURL            string
}

// ClosuresPageData is the team's list of closures.
type ClosuresPageData struct {
	Screen
	Rows                               []ClosureRow
	Query                              string
	SortKey, SortDir                   string
	URL, NewURL                        string
	Pager                              components.Paginator
	CurrentPage                        int
	Pages, Total                       int
	PerPage                            int
	CanConfigure                       bool
	ServicesURL, HoursURL, ClosuresURL string
}

// TableState is the list as the data table draws its addresses from.
func (d ClosuresPageData) TableState() components.DataTableState {
	return components.DataTableValues{
		Q: d.Query, Sort: configKey(d.SortKey, "from"), Dir: configDir(d.SortDir, "desc"),
		CurrentPage: d.CurrentPage, Size: d.PerPage, DefaultSize: d.PerPage,
	}
}

// ClosureFormPageData is the form that adds a closure.
type ClosureFormPageData struct {
	Screen
	Action, BackURL   string
	FromDay, FromTime string
	ToDay, ToTime     string
	Reason            string
}

// pager is the page of a list the data table reads its position from.
func pager(rows, total, perPage, page int, path string, query url.Values) components.Paginator {
	return pagination.NewLengthAwarePage(rows, total, perPage, page, pagination.Options{Path: path, Query: query})
}

// StatusVariant is the badge variant a status wears: the ones that wait stand
// out, the ones that are over fade.
func StatusVariant(key string) string {
	switch Status(key) {
	case StatusAwaitingPayment:
		return "destructive"
	case StatusCompleted, StatusCancelled, StatusExpired, StatusNoShow:
		return "outline"
	case StatusScheduled, StatusConfirmed:
		return "secondary"
	}
	return ""
}

// Minutes writes minutes since midnight as a time of day: 540 is "09:00".
func Minutes(n int) string { return fmt.Sprintf("%02d:%02d", n/60, n%60) }

// NewURLs are the addresses of the routes under a pair of prefixes and the
// segments, for a screen built outside a request: a test that draws a page, an
// e-mail that links to one. Empty values are the defaults.
func NewURLs(prefix, teamPrefix string, paths Paths) URLs {
	return URLs{cfg: Config{Prefix: prefix, TeamPrefix: teamPrefix, Paths: paths}.withDefaults()}
}
