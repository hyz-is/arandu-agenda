package feature_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/http/middleware"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/database/migrations"

	agenda "github.com/hyz-is/arandu-agenda"
)

// These tests drive the module the way an application does: build it, register
// its routes on a router, sign people in, and make requests. The screens are
// drawn from views the application compiles, so the pages themselves are held
// by tests/Unit; here every route answers the JSON a client asks for, and every
// form is posted the way a browser posts it.

// reservedPrefix is the namespace the framework keeps for itself: the health
// probe, the reload endpoint, the development console, the addressed assets.
const reservedPrefix = "/_arandu"

// site is the module registered on a router the way the kernel builds one.
type site struct {
	world
	router *fhttp.Router
	flash  *security.Flash
}

// serve registers the routes of a world's module.
func serve(t *testing.T, w world) site {
	t.Helper()
	flash := security.NewFlash([]byte(appKey), false)
	router := fhttp.NewRouter().WithFlash(flash)
	w.module.Routes(router.ForModule(w.module.Name()))
	return site{world: w, router: router, flash: flash}
}

// signIn starts a session for the subject and returns the cookies a browser
// would send back. Only the account is written to the session; the actions
// come back through the store's resolver, as an application's would.
func (s site) signIn(t *testing.T, subject security.Subject) []*http.Cookie {
	t.Helper()
	remember(subject)
	rec := httptest.NewRecorder()
	if _, err := s.sessions.Start(context.Background(), rec, security.Subject{ID: subject.ID, Tenant: subject.Tenant}); err != nil {
		t.Fatalf("starting a session: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("starting a session wrote no cookie")
	}
	return cookies
}

// request is one request to make.
type request struct {
	method  string
	target  string
	form    url.Values
	json    bool
	cookies []*http.Cookie
}

// answer makes one request and returns the recorder. A panic out of the router
// is a failure of the test: it is how an error nobody claimed arrives here.
func (s site) answer(t *testing.T, r request) (rec *httptest.ResponseRecorder) {
	t.Helper()
	body := strings.NewReader("")
	if r.form != nil {
		body = strings.NewReader(r.form.Encode())
	}
	req := httptest.NewRequest(r.method, r.target, body)
	if r.form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if r.json {
		req.Header.Set("Accept", "application/json")
	}
	for _, cookie := range r.cookies {
		req.AddCookie(cookie)
	}
	rec = httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("%s %s panicked, which a deployed router answers with 500: %v", r.method, r.target, recovered)
		}
	}()
	s.router.ServeHTTP(rec, req)
	return rec
}

// decode reads a JSON response into a map.
func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not JSON (%d): %s", rec.Code, rec.Body.String())
	}
	if data, ok := out["data"].(map[string]any); ok {
		return data
	}
	return out
}

// everyRoute is a request to each route the module registers, under the
// default prefixes. TestTheModuleRegistersItsRoutesUnderItsPrefixes fails when
// a route exists that this list does not reach.
var everyRoute = []struct {
	method string
	target string
	team   bool
}{
	{http.MethodGet, "/agenda", false},
	{http.MethodGet, "/agenda/new", false},
	{http.MethodPost, "/agenda", false},
	{http.MethodGet, "/agenda/b-1", false},
	{http.MethodPost, "/agenda/b-1/confirmation", false},
	{http.MethodGet, "/agenda/b-1/rescheduling", false},
	{http.MethodPost, "/agenda/b-1/rescheduling", false},
	{http.MethodPost, "/agenda/b-1/cancellation", false},

	{http.MethodGet, "/admin/agenda", true},
	{http.MethodGet, "/admin/agenda/b-1", true},
	{http.MethodPost, "/admin/agenda/b-1/completion", true},
	{http.MethodPost, "/admin/agenda/b-1/release", true},
	{http.MethodPost, "/admin/agenda/b-1/no-show", true},
	{http.MethodPost, "/admin/agenda/b-1/cancellation", true},
	{http.MethodPost, "/admin/agenda/b-1/rescheduling", true},
	{http.MethodGet, "/admin/agenda/settings/services", true},
	{http.MethodGet, "/admin/agenda/settings/services/new", true},
	{http.MethodPost, "/admin/agenda/settings/services", true},
	{http.MethodGet, "/admin/agenda/settings/services/b-1", true},
	{http.MethodPost, "/admin/agenda/settings/services/b-1", true},
	{http.MethodDelete, "/admin/agenda/settings/services/b-1", true},
	{http.MethodGet, "/admin/agenda/settings/hours", true},
	{http.MethodGet, "/admin/agenda/settings/hours/new", true},
	{http.MethodPost, "/admin/agenda/settings/hours", true},
	{http.MethodDelete, "/admin/agenda/settings/hours/b-1", true},
	{http.MethodGet, "/admin/agenda/settings/closures", true},
	{http.MethodGet, "/admin/agenda/settings/closures/new", true},
	{http.MethodPost, "/admin/agenda/settings/closures", true},
	{http.MethodDelete, "/admin/agenda/settings/closures/b-1", true},
}

func TestAVisitorWithNoSessionIsSentToSignIn(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))

	// RequireAuth answers before the handler runs, so a visitor with no
	// session reaches neither the policy nor the database.
	for _, r := range everyRoute {
		rec := s.answer(t, request{method: r.method, target: r.target})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != middleware.SignInPath {
			t.Errorf("%s %s answered %d to %q, want %d to %s", r.method, r.target, rec.Code, rec.Header().Get("Location"), http.StatusSeeOther, middleware.SignInPath)
		}
	}
}

func TestACustomerCannotReachTheTeamsSideAndTheTeamCannotActAsACustomer(t *testing.T) {
	t.Parallel()
	// A form that passes every check, so what refuses is the policy and not the
	// validation that comes first.
	valid := url.Values{
		"service": {"instalacao"}, "start": {"2026-11-11T12:00:00Z"}, "key": {"x"}, "name": {"X"}, "duration_minutes": {"40"},
		"weekday": {"1"}, "from": {"09:00"}, "to": {"10:00"}, "from_day": {"2026-12-25"}, "from_time": {"00:00"}, "to_day": {"2026-12-26"}, "to_time": {"00:00"},
		"day": {"2026-11-12"}, "time": {"10:00"}, "reason": {"cortesia"},
	}
	s := serve(t, install(t))
	customer := s.signIn(t, owner("acme", "user-boss"))
	team := s.signIn(t, agent("team-1"))

	for _, r := range everyRoute {
		if !r.team {
			continue
		}
		rec := s.answer(t, request{method: r.method, target: r.target, cookies: customer, json: true, form: valid})
		if rec.Code != http.StatusForbidden {
			t.Errorf("a customer on %s %s was answered %d, want 403", r.method, r.target, rec.Code)
		}
	}
	// The team member holds none of the customer's actions: the customer's side
	// is not theirs, though the session is valid.
	for _, r := range everyRoute {
		if r.team {
			continue
		}
		rec := s.answer(t, request{method: r.method, target: r.target, cookies: team, json: true, form: valid})
		if rec.Code != http.StatusForbidden {
			t.Errorf("a team member on %s %s was answered %d, want 403", r.method, r.target, rec.Code)
		}
	}
}

func TestACustomerBooksReadsConfirmsMovesAndCancelsOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	ana := s.signIn(t, member("acme", "user-ana"))

	// The form: the services and the free times of a day, as JSON.
	rec := s.answer(t, request{method: http.MethodGet, target: "/agenda/new?service=reconfiguracao&day=2026-11-11", cookies: ana, json: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("the form answered %d: %s", rec.Code, rec.Body)
	}
	form := decode(t, rec)
	slots, _ := form["slots"].([]any)
	if len(form["services"].([]any)) != 2 || len(slots) != 19 || slots[0] != "2026-11-11T12:00:00Z" {
		t.Fatalf("the form answered %v", form)
	}
	// An address somebody edited by hand is not a form to send back: a day that is
	// not one is dropped, and the form shows no times.
	rec = s.answer(t, request{method: http.MethodGet, target: "/agenda/new?service=reconfiguracao&day=ontem", cookies: ana, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || len(got["slots"].([]any)) != 0 {
		t.Fatalf("a day that is not one answered %d: %s", rec.Code, rec.Body)
	}

	// Booking redirects to the booking's page.
	rec = s.answer(t, request{method: http.MethodPost, target: "/agenda", cookies: ana, form: url.Values{
		"service": {"reconfiguracao"}, "start": {"2026-11-11T12:00:00Z"}, "subject_type": {"camera"}, "subject_id": {"42"}, "subject_label": {"Portaria"}, "note": {"Escura."},
	}})
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/agenda/") {
		t.Fatalf("booking answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	page := rec.Header().Get("Location")

	rec = s.answer(t, request{method: http.MethodGet, target: page, cookies: ana, json: true})
	shown := decode(t, rec)
	if rec.Code != http.StatusOK || shown["code"] != "AG-000001" || shown["status"] != "awaiting_payment" || shown["service"] != "reconfiguracao" || shown["starts_at"] != "2026-11-11T12:00:00Z" {
		t.Fatalf("reading answered %d: %v", rec.Code, shown)
	}
	// The customer is not shown the team's side of the booking, nor the space.
	for _, internal := range []string{"tenant", "tenant_id", "team_note", "released_by", "released_reason", "completed_by", "payment_ref", "requester_name", "reminded_at", "cancelled_by"} {
		if _, ok := shown[internal]; ok {
			t.Errorf("the customer's booking carries %q", internal)
		}
	}
	if subject := shown["subject"].(map[string]any); subject["type"] != "camera" || subject["id"] != "42" || subject["label"] != "Portaria" {
		t.Errorf("the subject is %v", subject)
	}

	// A booking taken: the form goes back with the sentence, and JSON gets a 422.
	rec = s.answer(t, request{method: http.MethodPost, target: "/agenda", cookies: ana, json: true, form: url.Values{"service": {"instalacao"}, "start": {"2026-11-11T12:00:00Z"}}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Este horário não está mais disponível") {
		t.Fatalf("booking a taken time answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/agenda", cookies: ana, json: true, form: url.Values{"service": {"instalacao"}, "start": {"amanhã"}}})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "Escolha um horário válido.") {
		t.Fatalf("booking with a start that is not one answered %d: %s", rec.Code, rec.Body)
	}

	// The payment arrives from the application's side; the customer confirms,
	// moves it, and cancels it.
	id := shown["id"].(string)
	if _, err := w.module.Settle(context.Background(), security.SystemGrant(agenda.AgendaSettle, "acme"), id, "pix-1"); err != nil {
		t.Fatalf("settling: %v", err)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: page + "/confirmation", cookies: ana, json: true})
	if rec.Code != http.StatusOK || decode(t, rec)["status"] != "confirmed" {
		t.Fatalf("confirming answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: page + "/rescheduling?day=2026-11-13", cookies: ana, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || len(got["slots"].([]any)) == 0 || got["booking"].(map[string]any)["code"] != "AG-000001" {
		t.Fatalf("the rescheduling form answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: page + "/rescheduling", cookies: ana, form: url.Values{"start": {"2026-11-13T12:00:00Z"}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != page {
		t.Fatalf("rescheduling answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = s.answer(t, request{method: http.MethodGet, target: page, cookies: ana, json: true})
	if got := decode(t, rec); got["starts_at"] != "2026-11-13T12:00:00Z" || got["reschedules"].(float64) != 1 || got["status"] != "scheduled" {
		t.Fatalf("the moved booking is %v", got)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: page + "/cancellation", cookies: ana, json: true, form: url.Values{"reason": {"Mudei de ideia"}}})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["status"] != "cancelled" || got["cancel_reason"] != "Mudei de ideia" {
		t.Fatalf("cancelling answered %d: %s", rec.Code, rec.Body)
	}

	// The list.
	rec = s.answer(t, request{method: http.MethodGet, target: "/agenda?q=AG-000001&status=cancelled&sort=code&dir=asc", cookies: ana, json: true})
	if list := decode(t, rec); rec.Code != http.StatusOK || list["total"].(float64) != 1 || len(list["items"].([]any)) != 1 {
		t.Fatalf("listing answered %d: %s", rec.Code, rec.Body)
	}
	// An address somebody edited by hand is not a form to send back.
	rec = s.answer(t, request{method: http.MethodGet, target: "/agenda?sort=tenant_id&status=bogus&page=-4", cookies: ana, json: true})
	if rec.Code != http.StatusOK || decode(t, rec)["total"].(float64) != 1 {
		t.Fatalf("a hand-edited address answered %d: %s", rec.Code, rec.Body)
	}
}

func TestARejectedFormGoesBackWithTheMessagesInPortuguese(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))
	ana := s.signIn(t, member("acme", "user-ana"))

	rec := s.answer(t, request{method: http.MethodPost, target: "/agenda", cookies: ana, form: url.Values{"service": {""}, "start": {"2026-11-11T12:00:00Z"}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("an empty form answered %d to %q, want %d back where it came from", rec.Code, rec.Header().Get("Location"), http.StatusSeeOther)
	}
	next := httptest.NewRequest(http.MethodGet, "/", nil)
	next.Header.Set("Accept", "text/html")
	for _, cookie := range rec.Result().Cookies() {
		next.AddCookie(cookie)
	}
	errs, _, ok := s.flash.Take(httptest.NewRecorder(), next)
	if !ok || len(errs["service"]) == 0 || errs["service"][0] != "Preencha este campo." {
		t.Fatalf("the redirect carries %v in the flash", errs)
	}
}

func TestABookingOfAnotherSpaceIsNotFoundOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	b := book(t, w, member("acme", "user-ana"), "instalacao", local(2026, time.November, 11, 9, 0))
	gil := s.signIn(t, member("globex", "user-gil"))
	bia := s.signIn(t, member("acme", "user-bia"))

	for _, who := range [][]*http.Cookie{gil, bia} {
		for _, r := range []struct{ method, target string }{
			{http.MethodGet, "/agenda/" + b.ID}, {http.MethodPost, "/agenda/" + b.ID + "/confirmation"},
			{http.MethodGet, "/agenda/" + b.ID + "/rescheduling"}, {http.MethodPost, "/agenda/" + b.ID + "/cancellation"},
		} {
			if rec := s.answer(t, request{method: r.method, target: r.target, cookies: who, json: true}); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s answered %d, want 404", r.method, r.target, rec.Code)
			}
		}
	}
	// The tenant is not a parameter: asking for another space changes nothing.
	for _, target := range []string{"/agenda?tenant=acme", "/agenda?tenant_id=acme&space=acme", "/agenda/" + b.ID + "?tenant=acme"} {
		if rec := s.answer(t, request{method: http.MethodGet, target: target, cookies: gil, json: true}); rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), b.ID) {
			t.Errorf("%s answered with another space's booking", target)
		}
	}
}

func TestTheTeamReadsActsAndConfiguresOverHTTP(t *testing.T) {
	t.Parallel()
	w := install(t)
	s := serve(t, w)
	rita := s.signIn(t, agent("team-1"))
	b := book(t, w, member("acme", "user-ana"), "reconfiguracao", local(2026, time.November, 11, 9, 0))

	rec := s.answer(t, request{method: http.MethodGet, target: "/admin/agenda?day=2026-11-11&status=awaiting_payment&q=Ana", cookies: rita, json: true})
	list := decode(t, rec)
	if rec.Code != http.StatusOK || list["total"].(float64) != 1 || list["items"].([]any)[0].(map[string]any)["tenant"] != "acme" {
		t.Fatalf("the team's list answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/" + b.ID, cookies: rita, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["tenant"] != "acme" || got["requester_name"] != "Ana Souza" {
		t.Fatalf("the team's booking answered %d: %s", rec.Code, rec.Body)
	}

	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + b.ID + "/release", cookies: rita, json: true, form: url.Values{"reason": {"Cortesia"}}})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["status"] != "scheduled" || got["released_by"] != "team-1" || got["released_reason"] != "Cortesia" {
		t.Fatalf("releasing answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + b.ID + "/rescheduling", cookies: rita, form: url.Values{"day": {"2026-11-14"}, "time": {"10:00"}}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/agenda/"+b.ID {
		t.Fatalf("rescheduling answered %d to %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/" + b.ID, cookies: rita, json: true})
	if got := decode(t, rec); got["starts_at"] != "2026-11-14T13:00:00Z" {
		t.Fatalf("the team moved the booking to %v, want 10:00 in Sao Paulo", got["starts_at"])
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + b.ID + "/rescheduling", cookies: rita, json: true, form: url.Values{"day": {"x"}, "time": {"10:00"}}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a day that is not one answered %d", rec.Code)
	}
	w.clock.set(local(2026, time.November, 14, 11, 0).UTC())
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + b.ID + "/completion", cookies: rita, json: true, form: url.Values{"note": {"Feito."}}})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["status"] != "completed" || got["team_note"] != "Feito." || got["completed_by"] != "team-1" {
		t.Fatalf("completing answered %d: %s", rec.Code, rec.Body)
	}
	other := book(t, w, member("acme", "user-ana"), "instalacao", local(2026, time.November, 16, 9, 0))
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + other.ID + "/cancellation", cookies: rita, json: true, form: url.Values{"reason": {"Técnico doente"}}})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["status"] != "cancelled" || got["cancelled_by"] != "team" {
		t.Fatalf("cancelling answered %d: %s", rec.Code, rec.Body)
	}
	absent := book(t, w, member("acme", "user-ana"), "instalacao", local(2026, time.November, 16, 9, 40))
	w.clock.set(local(2026, time.November, 16, 10, 0).UTC())
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + absent.ID + "/no-show", cookies: rita, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["status"] != "no_show" {
		t.Fatalf("marking an absence answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/nobody", cookies: rita, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("a booking that does not exist answered %d", rec.Code)
	}

	// The settings: a service, a window of the week, a closure.
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/settings/services", cookies: rita, json: true, form: url.Values{
		"key": {"visita"}, "name": {"Visita técnica"}, "duration_minutes": {"60"}, "requires_payment": {"on"}, "active": {"on"},
	}})
	service := decode(t, rec)
	if rec.Code != http.StatusCreated || service["key"] != "visita" || service["requires_payment"] != true || service["active"] != true || service["duration_minutes"].(float64) != 60 {
		t.Fatalf("creating a service answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/settings/services/" + service["id"].(string), cookies: rita, json: true, form: url.Values{
		"key": {"visita"}, "name": {"Visita"}, "duration_minutes": {"30"},
	}})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["name"] != "Visita" || got["active"] != false || got["requires_payment"] != false {
		t.Fatalf("updating a service answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/settings/services?q=visita", cookies: rita, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["total"].(float64) != 1 {
		t.Fatalf("listing the services answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/settings/services/" + service["id"].(string), cookies: rita, json: true})
	if got := decode(t, rec); rec.Code != http.StatusOK || got["key"] != "visita" {
		t.Fatalf("reading a service answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodDelete, target: "/admin/agenda/settings/services/" + service["id"].(string), cookies: rita})
	if rec.Code != http.StatusOK {
		t.Fatalf("deleting a service answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodDelete, target: "/admin/agenda/settings/services/" + service["id"].(string), cookies: rita, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("deleting a service twice answered %d", rec.Code)
	}

	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/settings/hours", cookies: rita, json: true, form: url.Values{"weekday": {"6"}, "from": {"09:00"}, "to": {"12:00"}}})
	hour := decode(t, rec)
	if rec.Code != http.StatusCreated || hour["weekday"].(float64) != 6 || hour["start_minute"].(float64) != 540 || hour["end_minute"].(float64) != 720 {
		t.Fatalf("creating a window answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/settings/hours", cookies: rita, json: true, form: url.Values{"weekday": {"6"}, "from": {"9h"}, "to": {"12:00"}}}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a time that is not one answered %d", rec.Code)
	}
	if rec := s.answer(t, request{method: http.MethodDelete, target: "/admin/agenda/settings/hours/" + hour["id"].(string), cookies: rita, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("deleting a window answered %d: %s", rec.Code, rec.Body)
	}
	rec = s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/settings/closures", cookies: rita, json: true, form: url.Values{
		"from_day": {"2026-12-25"}, "from_time": {"00:00"}, "to_day": {"2026-12-25"}, "to_time": {"23:59"}, "reason": {"Natal"},
	}})
	closure := decode(t, rec)
	if rec.Code != http.StatusCreated || closure["starts_at"] != "2026-12-25T03:00:00Z" || closure["ends_at"] != "2026-12-26T02:59:00Z" {
		t.Fatalf("creating a closure answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/settings/closures", cookies: rita, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("listing the closures answered %d", rec.Code)
	}
	if rec := s.answer(t, request{method: http.MethodDelete, target: "/admin/agenda/settings/closures/" + closure["id"].(string), cookies: rita, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("deleting a closure answered %d: %s", rec.Code, rec.Body)
	}
}

func TestTheModuleRegistersItsRoutesUnderItsPrefixes(t *testing.T) {
	t.Parallel()
	s := serve(t, install(t))

	got := make([]string, 0, len(everyRoute))
	for _, route := range s.router.Routes() {
		if route.Module != "agenda" {
			t.Errorf("the route %s %s is not tagged with the module name: %q", route.Method, route.Pattern, route.Module)
		}
		got = append(got, route.Method+" "+route.Pattern)
	}
	want := make([]string, 0, len(everyRoute))
	for _, r := range everyRoute {
		want = append(want, r.method+" "+routePattern(r.target))
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("registered\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The names are what a URL is built from.
	names := map[string]bool{}
	for _, route := range s.router.Routes() {
		names[route.RouteName()] = true
	}
	for _, name := range []string{"agenda.index", "agenda.show", "agenda.confirm", "agenda.reschedule", "agenda.cancel", "agenda.team.index", "agenda.team.release", "agenda.team.hours.delete"} {
		if !names[name] {
			t.Errorf("no route is named %s", name)
		}
	}
}

// routePattern turns the sample identifier of everyRoute into the wildcard the
// router registers.
func routePattern(target string) string { return strings.ReplaceAll(target, "/b-1", "/{id}") }

func TestThePrefixesAndTheSegmentsMoveTheRoutes(t *testing.T) {
	t.Parallel()
	w := installWith(t, func(c *agenda.Config) {
		c.Prefix, c.TeamPrefix, c.Paths = "/painel/agendamentos", "/admin/agenda", agenda.PortuguesePaths
	})
	s := serve(t, w)
	ana := s.signIn(t, member("acme", "user-ana"))

	rec := s.answer(t, request{method: http.MethodPost, target: "/painel/agendamentos", cookies: ana, json: true, form: url.Values{"service": {"instalacao"}, "start": {"2026-11-11T12:00:00Z"}}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("booking under the new prefix answered %d: %s", rec.Code, rec.Body)
	}
	id := decode(t, rec)["id"].(string)
	if rec := s.answer(t, request{method: http.MethodPost, target: "/painel/agendamentos/" + id + "/confirmacao", cookies: ana, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("confirming under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	rita := s.signIn(t, agent("team-1"))
	if rec := s.answer(t, request{method: http.MethodPost, target: "/admin/agenda/" + id + "/ausencia", cookies: rita, json: true}); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an absence before the start under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/agenda/configuracoes/servicos", cookies: rita, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("the services under the new segments answered %d: %s", rec.Code, rec.Body)
	}
	// What was at the old addresses is gone.
	if rec := s.answer(t, request{method: http.MethodGet, target: "/agenda", cookies: ana, json: true}); rec.Code != http.StatusNotFound {
		t.Fatalf("the old address answered %d, want 404", rec.Code)
	}

	// The URLs the screens link to follow the configuration.
	urls := w.module.URLs()
	if got := urls.Confirm("abc"); got != "/painel/agendamentos/abc/confirmacao" {
		t.Fatalf("the confirmation address is %q", got)
	}
	if got := urls.TeamService("a b"); got != "/admin/agenda/configuracoes/servicos/a%20b" {
		t.Fatalf("the service address is %q", got)
	}
	if got := urls.NewFor("reconfiguracao", agenda.BookingSubject{Type: "camera", ID: "42", Label: "Portaria 2"}); got != "/painel/agendamentos/novo?service=reconfiguracao&subject_id=42&subject_label=Portaria+2&subject_type=camera" {
		t.Fatalf("the booking form address is %q", got)
	}
	if got := urls.NewFor("", agenda.BookingSubject{}); got != "/painel/agendamentos/novo" {
		t.Fatalf("the bare booking form address is %q", got)
	}
}

func TestTheTwoSidesAreMountedApartAndGuardedApart(t *testing.T) {
	t.Parallel()
	denied := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	w := installWith(t, func(c *agenda.Config) { c.TeamMiddleware = []func(http.Handler) http.Handler{denied} })

	// Mounted alone, each side registers its own routes and no others.
	customerOnly := fhttp.NewRouter()
	w.module.CustomerRoutes(customerOnly.ForModule("agenda"))
	teamOnly := fhttp.NewRouter()
	w.module.TeamRoutes(teamOnly.ForModule("agenda"))
	for _, route := range customerOnly.Routes() {
		if strings.HasPrefix(route.Pattern, "/admin") {
			t.Errorf("the customer's routes include %s", route.Pattern)
		}
	}
	for _, route := range teamOnly.Routes() {
		if !strings.HasPrefix(route.Pattern, "/admin") {
			t.Errorf("the team's routes include %s", route.Pattern)
		}
	}
	if len(customerOnly.Routes()) == 0 || len(teamOnly.Routes()) == 0 {
		t.Fatal("a side registered no route, so this test proved nothing")
	}

	// A guard of the application's own side runs on that side alone.
	s := serve(t, w)
	rita := s.signIn(t, agent("team-1"))
	ana := s.signIn(t, member("acme", "user-ana"))
	if rec := s.answer(t, request{method: http.MethodGet, target: "/admin/agenda", cookies: rita, json: true}); rec.Code != http.StatusTeapot {
		t.Fatalf("the team's guard did not run: %d", rec.Code)
	}
	if rec := s.answer(t, request{method: http.MethodGet, target: "/agenda", cookies: ana, json: true}); rec.Code != http.StatusOK {
		t.Fatalf("the team's guard ran on the customer's side: %d", rec.Code)
	}
}

// TestNoRouteLandsInTheFrameworkNamespace is the one property of this package
// that syntax cannot hold: a prefix arrives through configuration, so the only
// way to know where the routes ended up is to register them and read the table
// back.
func TestNoRouteLandsInTheFrameworkNamespace(t *testing.T) {
	t.Parallel()

	for _, change := range []func(*agenda.Config){
		func(*agenda.Config) {},
		func(c *agenda.Config) {
			c.Prefix, c.TeamPrefix, c.Paths = "/painel/agendamentos", "/admin/agenda", agenda.PortuguesePaths
		},
	} {
		s := serve(t, installWith(t, change))
		registered := 0
		for _, route := range s.router.Routes() {
			registered++
			if route.Pattern == reservedPrefix || strings.HasPrefix(route.Pattern, reservedPrefix+"/") {
				t.Errorf("the route %s %s is registered under %s/, which the framework keeps for itself and refuses at boot", route.Method, route.Pattern, reservedPrefix)
			}
		}
		if registered == 0 {
			t.Fatal("the module registered no route, so this test proved nothing")
		}
	}
}

func TestNewRefusesAWiringThatCannotWork(t *testing.T) {
	t.Parallel()

	sessions := security.NewSessionStore([]byte(appKey), 3600e9, false, security.NewMemoryBackend())
	handle := data.Wrap(nil, data.DialectSQLite)
	valid := agenda.Config{TeamTenant: realm, Zone: zone}

	for name, cfg := range map[string]agenda.Config{
		"no team tenant":            {Zone: zone},
		"a team tenant that is not": {TeamTenant: "Not/Valid", Zone: zone},
		"no zone":                   {TeamTenant: realm},
		"a relative prefix":         {TeamTenant: realm, Zone: zone, Prefix: "agenda"},
		"a prefix in the framework": {TeamTenant: realm, Zone: zone, TeamPrefix: "/_arandu/agenda"},
		"the same prefix twice":     {TeamTenant: realm, Zone: zone, Prefix: "/x", TeamPrefix: "/x"},
		"a wildcard prefix":         {TeamTenant: realm, Zone: zone, Prefix: "/agenda/{id}"},
		"a segment with a wildcard": {TeamTenant: realm, Zone: zone, Paths: agenda.Paths{New: "/{new}"}},
		"a negative page size":      {TeamTenant: realm, Zone: zone, PageSize: -1},
		"a page size too big":       {TeamTenant: realm, Zone: zone, PageSize: agenda.MaxPageSize + 1},
		"a capacity too big":        {TeamTenant: realm, Zone: zone, Capacity: agenda.MaxCapacity + 1},
		"a step in seconds":         {TeamTenant: realm, Zone: zone, SlotStep: 90 * time.Second},
		"a horizon too far":         {TeamTenant: realm, Zone: zone, Horizon: agenda.MaxHorizon + 1},
		"a nil guard":               {TeamTenant: realm, Zone: zone, TeamMiddleware: []func(http.Handler) http.Handler{nil}},
	} {
		if _, err := agenda.New(cfg, handle, sessions); err == nil {
			t.Errorf("a configuration with %s was accepted", name)
		}
	}
	if _, err := agenda.New(valid, nil, sessions); err == nil {
		t.Error("a nil database handle was accepted")
	}
	if _, err := agenda.New(valid, handle, nil); err == nil {
		t.Error("a nil session store was accepted")
	}
	if _, err := agenda.New(valid, handle, sessions); err != nil {
		t.Fatalf("a valid wiring was refused: %v", err)
	}
}

func TestTheModuleDeclaresItsSchema(t *testing.T) {
	t.Parallel()
	w := install(t)

	declared := w.module.Migrations()
	if len(declared) == 0 {
		t.Fatal("the module declares migrations = true and returns none")
	}
	names := make([]string, 0, len(declared))
	for _, migration := range declared {
		name := migration.GetName()
		if name == "" {
			t.Fatal("a migration has no name, and the name is what carries the order")
		}
		names = append(names, name)
		// A migration that cannot be rolled back is a deploy that cannot be
		// undone. The migrator finds Down by type assertion, so a Down with the
		// wrong signature is a rollback that silently does nothing.
		if _, ok := migration.(migrations.ReversibleMigration); !ok {
			t.Errorf("the migration %s has no Down", name)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("the migrations are not returned in the order their names sort in: %v", names)
	}

	// Down takes it all back, and Up can run again.
	conn := hedatabaseFor(w)
	for _, migration := range declared {
		if err := migration.(migrations.ReversibleMigration).Down(context.Background(), conn); err != nil {
			t.Fatalf("rolling back %s: %v", migration.GetName(), err)
		}
	}
	var left int
	if err := w.sql.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name LIKE 'agenda\_%' ESCAPE '\'`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("%d tables are left after the rollback, %v", left, err)
	}
	for _, migration := range declared {
		if err := migration.Up(context.Background(), conn); err != nil {
			t.Fatalf("applying %s again: %v", migration.GetName(), err)
		}
	}
}
