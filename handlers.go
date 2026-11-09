package agenda

import (
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/validation"
)

// The handlers are thin on purpose: read the input, ask the service, answer. No
// rule, database handle or Model construction lives here. A handler that
// reached data directly would skip the service's policy boundary, and the
// layout makes that visible rather than relying on review.
//
// Who is asking comes from ctx.User, which reads the subject RequireAuth put on
// the request. No handler loads the session again or takes an identity from the
// input, and none takes a tenant from anywhere: the tenant is the Grant's.
//
// An error from the service is returned as it is, and the router answers it:
// validation.Errors goes back where the request came from with the messages in
// the flash, ErrNotFound is 404 because it wraps model.ErrModelNotFound, and a
// policy refusal is 403. The answer is the status and its standard sentence,
// never the error's own text -- telling a client why a policy said no tells it
// what exists, one request at a time, and the reason belongs in the log. Any
// other error is one nobody expected, and it reaches the error page rather than
// a 200 with an empty body.
//
// Every GET answers the screen to a browser and JSON to a client that asks for
// it; every form posts to the same routes the JSON clients use.

// handlers are the handlers of the route table, by route name.
func (m *Module) handlers() map[string]func(*fhttp.Context) error {
	return map[string]func(*fhttp.Context) error{
		"agenda.index":       m.index,
		"agenda.create":      m.create,
		"agenda.store":       m.store,
		"agenda.show":        m.show,
		"agenda.confirm":     m.confirm,
		"agenda.reschedule":  m.rescheduleForm,
		"agenda.rescheduled": m.reschedule,
		"agenda.cancel":      m.cancel,

		"agenda.team.index":      m.teamIndex,
		"agenda.team.show":       m.teamShow,
		"agenda.team.complete":   m.teamComplete,
		"agenda.team.release":    m.teamRelease,
		"agenda.team.no_show":    m.teamNoShow,
		"agenda.team.cancel":     m.teamCancel,
		"agenda.team.reschedule": m.teamReschedule,

		"agenda.team.services":        m.teamServices,
		"agenda.team.services.create": m.teamServiceForm,
		"agenda.team.services.store":  m.teamServiceStore,
		"agenda.team.services.edit":   m.teamServiceForm,
		"agenda.team.services.update": m.teamServiceUpdate,
		"agenda.team.services.delete": m.teamServiceDelete,

		"agenda.team.hours":        m.teamHours,
		"agenda.team.hours.create": m.teamHourForm,
		"agenda.team.hours.store":  m.teamHourStore,
		"agenda.team.hours.delete": m.teamHourDelete,

		"agenda.team.closures":        m.teamClosures,
		"agenda.team.closures.create": m.teamClosureForm,
		"agenda.team.closures.store":  m.teamClosureStore,
		"agenda.team.closures.delete": m.teamClosureDelete,
	}
}

// index answers the customer's bookings.
func (m *Module) index(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, sortableBooking)
	page, err := m.bookings.Bookings(ctx.Ctx(), who, ListQuery{
		Search: p.search, Statuses: p.statuses, Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page, customerFields))
	}
	sc, err := m.screen(ctx, who, "screen.index_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewIndex, m.indexPage(ctx, sc, who, p, page))
}

// create answers the form that books: the services, and the free times of the
// day and the service the address names.
func (m *Module) create(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	services, err := m.bookings.Services(ctx.Ctx(), who)
	if err != nil {
		return err
	}
	p := newParamsOf(ctx, services)
	var slots []time.Time
	if p.day != "" && p.service != "" {
		day, err := time.ParseInLocation("2006-01-02", p.day, m.cfg.Zone)
		if err != nil {
			return validation.Errors{"day": {m.Labels().message("day")}}
		}
		if slots, err = m.bookings.Slots(ctx.Ctx(), who, p.service, day); err != nil {
			return err
		}
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"services": serviceList(services), "slots": instants(slots)})
	}
	sc, err := m.screen(ctx, who, "screen.new_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewNew, m.newPage(sc, m.cfg.Now().UTC(), services, p, slots))
}

// store books.
func (m *Module) store(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	start, err := m.parseInstant(ctx.Input("start"))
	if err != nil {
		return err
	}
	booked, err := m.bookings.Book(ctx.Ctx(), who, BookInput{
		Service: ctx.Input("service"), Start: start, Note: ctx.Input("note"),
		Subject: BookingSubject{Type: ctx.Input("subject_type"), ID: ctx.Input("subject_id"), Label: ctx.Input("subject_label")},
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(customerFields(summaryOf(booked))))
	}
	return ctx.Redirect(m.URLs().Show(booked.ID))
}

// show answers one booking.
func (m *Module) show(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.bookings.Booking(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(customerFields(b)))
	}
	sc, err := m.screen(ctx, who, "screen.show_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewShow, m.showPage(ctx, sc, who, b))
}

// confirm records that the customer will come.
func (m *Module) confirm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.bookings.Confirm(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return m.answer(ctx, summaryOf(b), m.URLs().Show(b.ID))
}

// rescheduleForm answers the form that moves a booking, with the free times of
// the day the address names.
func (m *Module) rescheduleForm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.bookings.Booking(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	day := cleanDay(ctx.Query("day"))
	var slots []time.Time
	if day != "" {
		parsed, err := time.ParseInLocation("2006-01-02", day, m.cfg.Zone)
		if err != nil {
			return validation.Errors{"day": {m.Labels().message("day")}}
		}
		if slots, err = m.bookings.Slots(ctx.Ctx(), who, b.ServiceKey, parsed); err != nil {
			return err
		}
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"booking": customerFields(b), "slots": instants(slots)})
	}
	sc, err := m.screen(ctx, who, "screen.reschedule_title", false)
	if err != nil {
		return err
	}
	return ctx.View(ViewReschedule, m.reschedulePage(sc, m.cfg.Now().UTC(), b, day, slots))
}

// reschedule moves a booking to the start the customer chose.
func (m *Module) reschedule(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	start, err := m.parseInstant(ctx.Input("start"))
	if err != nil {
		return err
	}
	b, err := m.bookings.Reschedule(ctx.Ctx(), who, ctx.Param("id"), start)
	if err != nil {
		return err
	}
	return m.answer(ctx, summaryOf(b), m.URLs().Show(b.ID))
}

// cancel gives the slot up.
func (m *Module) cancel(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.bookings.Cancel(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	return m.answer(ctx, summaryOf(b), m.URLs().Show(b.ID))
}

// answer ends an act on a booking: the booking as JSON for a client, and the
// booking's page for a browser.
func (m *Module) answer(ctx *fhttp.Context, b Summary, to string) error {
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(customerFields(b)))
	}
	return ctx.Redirect(to)
}

// teamAnswer is answer for the team's side.
func (m *Module) teamAnswer(ctx *fhttp.Context, b Summary) error {
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(teamFields(b)))
	}
	return ctx.Redirect(m.URLs().TeamShow(b.ID))
}

// teamIndex answers the bookings of every space.
func (m *Module) teamIndex(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := listParams(ctx, sortableTeam)
	day := cleanDay(ctx.Query("day"))
	page, err := m.team.Bookings(ctx.Ctx(), who, TeamQuery{
		Search: p.search, Statuses: p.statuses, Day: day, Tenant: ctx.Query("space"),
		Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: m.cfg.PageSize,
	})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, pageResource(page, teamFields))
	}
	sc, err := m.screen(ctx, who, "screen.team_index_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamIndex, m.teamIndexPage(ctx, sc, who, p, day, page))
}

// teamShow answers one booking, with what the team wrote on it.
func (m *Module) teamShow(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.team.Booking(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(teamFields(b)))
	}
	sc, err := m.screen(ctx, who, "screen.team_show_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamShow, m.teamShowPage(ctx, sc, who, b))
}

// teamComplete records that the work was done.
func (m *Module) teamComplete(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.team.Complete(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("note"))
	if err != nil {
		return err
	}
	return m.teamAnswer(ctx, summaryOf(b))
}

// teamRelease lets a booking through without a payment.
func (m *Module) teamRelease(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.team.Release(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	return m.teamAnswer(ctx, summaryOf(b))
}

// teamNoShow records an absence.
func (m *Module) teamNoShow(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.team.NoShow(ctx.Ctx(), who, ctx.Param("id"))
	if err != nil {
		return err
	}
	return m.teamAnswer(ctx, summaryOf(b))
}

// teamCancel cancels a booking on the team's behalf.
func (m *Module) teamCancel(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	b, err := m.team.Cancel(ctx.Ctx(), who, ctx.Param("id"), ctx.Input("reason"))
	if err != nil {
		return err
	}
	return m.teamAnswer(ctx, summaryOf(b))
}

// teamReschedule moves a booking to the day and time the team typed.
func (m *Module) teamReschedule(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	start, err := m.parseLocal(ctx.Input("day"), ctx.Input("time"))
	if err != nil {
		return err
	}
	b, err := m.team.Reschedule(ctx.Ctx(), who, ctx.Param("id"), start)
	if err != nil {
		return err
	}
	return m.teamAnswer(ctx, summaryOf(b))
}

// teamServices answers the team's services.
func (m *Module) teamServices(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := configParamsOf(ctx, sortableService)
	list, err := m.team.Services(ctx.Ctx(), who, p.query(m.cfg.PageSize))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, listResource(list, serviceFields))
	}
	sc, err := m.screen(ctx, who, "screen.services_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamServices, m.servicesPage(ctx, sc, who, p, list))
}

// teamServiceForm answers the form that adds a service, or changes the one the
// address names.
func (m *Module) teamServiceForm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	u := m.URLs()
	data := ServiceFormPageData{Action: u.TeamServices(), BackURL: u.TeamServices(), Duration: "40", Active: true}
	if id := ctx.Param("id"); id != "" {
		service, err := m.team.Service(ctx.Ctx(), who, id)
		if err != nil {
			return err
		}
		if ctx.WantsJSON() {
			return ctx.JSON(stdhttp.StatusOK, fieldsResource(serviceFields(service)))
		}
		data.Action, data.Editing = u.TeamService(id), true
		data.Key, data.Name, data.Description = service.Key, service.Name, service.Description
		data.Duration, data.RequiresPayment, data.Active = strconv.Itoa(service.DurationMinutes), service.RequiresPayment, service.Active
	} else if err := m.team.CanConfigure(ctx.Ctx(), who); err != nil {
		return err
	}
	title := "screen.service_new_title"
	if data.Editing {
		title = "screen.service_edit_title"
	}
	sc, err := m.screen(ctx, who, title, true)
	if err != nil {
		return err
	}
	data.Screen = sc
	return ctx.View(ViewTeamServiceForm, data)
}

// serviceInput reads a service from a form.
func serviceInput(ctx *fhttp.Context) ServiceInput {
	return ServiceInput{
		Key: ctx.Input("key"), Name: ctx.Input("name"), Description: ctx.Input("description"),
		DurationMinutes: atoi(ctx.Input("duration_minutes")),
		RequiresPayment: checked(ctx.Input("requires_payment")), Active: checked(ctx.Input("active")),
	}
}

// teamServiceStore adds a service.
func (m *Module) teamServiceStore(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	service, err := m.team.CreateService(ctx.Ctx(), who, serviceInput(ctx))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(serviceFields(serviceViewOf(service))))
	}
	return ctx.Redirect(m.URLs().TeamServices())
}

// teamServiceUpdate changes a service.
func (m *Module) teamServiceUpdate(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	service, err := m.team.UpdateService(ctx.Ctx(), who, ctx.Param("id"), serviceInput(ctx))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource(serviceFields(serviceViewOf(service))))
	}
	return ctx.Redirect(m.URLs().TeamServices())
}

// teamServiceDelete removes a service. A browser asks for it through htmx, which
// takes the row out of the table when the answer is a 200.
func (m *Module) teamServiceDelete(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.DeleteService(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return deleted(ctx)
}

// teamHours answers the windows of the team's week.
func (m *Module) teamHours(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := configParamsOf(ctx, sortableHour)
	list, err := m.team.Hours(ctx.Ctx(), who, p.query(m.cfg.PageSize))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, listResource(list, hourFields))
	}
	sc, err := m.screen(ctx, who, "screen.hours_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamHours, m.hoursPage(ctx, sc, who, p, list))
}

// teamHourForm answers the form that adds a window.
func (m *Module) teamHourForm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.CanConfigure(ctx.Ctx(), who); err != nil {
		return err
	}
	sc, err := m.screen(ctx, who, "screen.hour_new_title", true)
	if err != nil {
		return err
	}
	u := m.URLs()
	return ctx.View(ViewTeamHourForm, HourFormPageData{
		Screen: sc, Action: u.TeamHours(), BackURL: u.TeamHours(), WeekdayOptions: sc.WeekdayOptions(),
		Weekday: "1", Start: "09:00", End: "18:00",
	})
}

// teamHourStore adds a window to the week.
func (m *Module) teamHourStore(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	start, okStart := minutesOf(ctx.Input("from"))
	end, okEnd := minutesOf(ctx.Input("to"))
	errs := validation.Errors{}
	if !okStart {
		errs.Add("from", m.Labels().message("time"))
	}
	if !okEnd {
		errs.Add("to", m.Labels().message("time"))
	}
	if errs.Any() {
		return errs
	}
	hour, err := m.team.CreateHour(ctx.Ctx(), who, HourInput{Weekday: atoi(ctx.Input("weekday")), StartMinute: start, EndMinute: end})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(hourFields(hourViewOf(hour))))
	}
	return ctx.Redirect(m.URLs().TeamHours())
}

// teamHourDelete removes a window from the week.
func (m *Module) teamHourDelete(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.DeleteHour(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return deleted(ctx)
}

// teamClosures answers the team's closures.
func (m *Module) teamClosures(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	p := configParamsOf(ctx, sortableClosure)
	list, err := m.team.Closures(ctx.Ctx(), who, p.query(m.cfg.PageSize))
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, listResource(list, closureFields))
	}
	sc, err := m.screen(ctx, who, "screen.closures_title", true)
	if err != nil {
		return err
	}
	return ctx.View(ViewTeamClosures, m.closuresPage(ctx, sc, who, p, list))
}

// teamClosureForm answers the form that adds a closure.
func (m *Module) teamClosureForm(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.CanConfigure(ctx.Ctx(), who); err != nil {
		return err
	}
	sc, err := m.screen(ctx, who, "screen.closure_new_title", true)
	if err != nil {
		return err
	}
	u := m.URLs()
	return ctx.View(ViewTeamClosureForm, ClosureFormPageData{
		Screen: sc, Action: u.TeamClosures(), BackURL: u.TeamClosures(), FromTime: "00:00", ToTime: "23:59",
	})
}

// teamClosureStore adds a closure.
func (m *Module) teamClosureStore(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	from, err := m.parseLocal(ctx.Input("from_day"), ctx.Input("from_time"))
	if err != nil {
		return err
	}
	to, err := m.parseLocal(ctx.Input("to_day"), ctx.Input("to_time"))
	if err != nil {
		return err
	}
	closure, err := m.team.CreateClosure(ctx.Ctx(), who, ClosureInput{StartsAt: from, EndsAt: to, Reason: ctx.Input("reason")})
	if err != nil {
		return err
	}
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusCreated, fieldsResource(closureFields(closureViewOf(closure))))
	}
	return ctx.Redirect(m.URLs().TeamClosures())
}

// teamClosureDelete removes a closure.
func (m *Module) teamClosureDelete(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.DeleteClosure(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return deleted(ctx)
}

// deleted answers a removal: the row is taken out of the table by htmx when the
// answer is a 200, and a client reads the JSON.
func deleted(ctx *fhttp.Context) error {
	if ctx.WantsJSON() {
		return ctx.JSON(stdhttp.StatusOK, fieldsResource{"deleted": true})
	}
	return ctx.Status(stdhttp.StatusOK)
}

// params are what a list's address says, read and cleaned: a status that is not
// one, and a sort the list does not offer, are dropped rather than refused,
// because an address somebody edited by hand is not a form to send back.
type params struct {
	search    string
	statuses  []Status
	sort, dir string
	page      int
}

// listParams reads the query string of a list. The sort is kept only when the
// allowlist has it.
func listParams(ctx *fhttp.Context, allowed map[string]string) params {
	query := ctx.Request.URL.Query()
	p := params{search: strings.TrimSpace(query.Get("q"))}
	for _, raw := range query["status"] {
		if s := Status(strings.TrimSpace(raw)); s.Valid() {
			p.statuses = append(p.statuses, s)
		}
	}
	if sort := query.Get("sort"); sort != "" {
		if _, ok := allowed[sort]; ok {
			p.sort = sort
		}
	}
	if query.Get("dir") == "asc" {
		p.dir = "asc"
	}
	p.page, _ = strconv.Atoi(query.Get("page"))
	return p
}

// configParams are the parameters of a list of settings.
type configParams struct {
	search    string
	sort, dir string
	page      int
}

func configParamsOf(ctx *fhttp.Context, allowed map[string]string) configParams {
	query := ctx.Request.URL.Query()
	p := configParams{search: strings.TrimSpace(query.Get("q"))}
	if sort := query.Get("sort"); sort != "" {
		if _, ok := allowed[sort]; ok {
			p.sort = sort
		}
	}
	switch query.Get("dir") {
	case "asc", "desc":
		p.dir = query.Get("dir")
	}
	p.page, _ = strconv.Atoi(query.Get("page"))
	return p
}

func (p configParams) query(perPage int) ConfigQuery {
	return ConfigQuery{Search: p.search, Sort: p.sort, Dir: p.dir, Page: p.page, PerPage: perPage}
}

// newParams are what the form that books is asked for: the service, the day and
// what the booking is about.
type newParams struct {
	service, day string
	subject      BookingSubject
}

// newParamsOf reads them, and picks the first service when none was asked for.
func newParamsOf(ctx *fhttp.Context, services []ServiceView) newParams {
	p := newParams{
		service: strings.TrimSpace(ctx.Query("service")), day: cleanDay(ctx.Query("day")),
		subject: BookingSubject{
			Type: clip(strings.TrimSpace(ctx.Query("subject_type")), maxIdentifierLen),
			ID:   clip(strings.TrimSpace(ctx.Query("subject_id")), maxIdentifierLen),
		},
	}
	p.subject.Label = clip(strings.TrimSpace(ctx.Query("subject_label")), maxLabelLen)
	known := false
	for _, s := range services {
		known = known || s.Key == p.service
	}
	if !known {
		p.service = ""
		if len(services) > 0 {
			p.service = services[0].Key
		}
	}
	return p
}

// cleanDay keeps a day written as 2026-11-10 and drops anything else.
func cleanDay(value string) string {
	value = strings.TrimSpace(value)
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

// checked reads a checkbox.
func checked(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// parseInstant reads the start a form posted: an instant, as RFC 3339.
func (m *Module) parseInstant(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, validation.Errors{"start": {m.Labels().message("start")}}
	}
	return parsed.UTC(), nil
}

// parseLocal reads a day and a time of day typed in Config.Zone as an instant.
func (m *Module) parseLocal(day, clock string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02 15:04", strings.TrimSpace(day)+" "+strings.TrimSpace(clock), m.cfg.Zone)
	if err != nil {
		return time.Time{}, validation.Errors{"start": {m.Labels().message("start")}}
	}
	return parsed.UTC(), nil
}

// instants are starts as the JSON writes them.
func instants(slots []time.Time) []string {
	out := make([]string, 0, len(slots))
	for _, start := range slots {
		out = append(out, instant(start))
	}
	return out
}

// serviceList is the services as the JSON writes them.
func serviceList(services []ServiceView) []map[string]any {
	out := make([]map[string]any, 0, len(services))
	for _, s := range services {
		out = append(out, serviceFields(s))
	}
	return out
}

// pageResource is a page of a list of bookings as one response.
func pageResource(page Page, fields func(Summary) map[string]any) fieldsResource {
	items := make([]map[string]any, 0, len(page.Rows))
	for _, row := range page.Rows {
		items = append(items, fields(row))
	}
	return fieldsResource{"items": items, "total": page.Total, "page": page.Page, "pages": page.Pages}
}

// listResource is a page of a list of settings as one response.
func listResource[T any](list Listing[T], fields func(T) map[string]any) fieldsResource {
	items := make([]map[string]any, 0, len(list.Rows))
	for _, row := range list.Rows {
		items = append(items, fields(row))
	}
	return fieldsResource{"items": items, "total": list.Total, "page": list.Page, "pages": list.Pages}
}
