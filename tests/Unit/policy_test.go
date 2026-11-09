package unit_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/model"
	"github.com/arandu-io/hesape/database/query"

	agenda "github.com/hyz-is/arandu-agenda"
)

// The properties this package exists to keep are checked here, and they are
// checked against the code rather than described in a document:
//
//  1. the policies deny every action, and open one only for the subject who was
//     granted it, in the tenant the record is in;
//  2. the services authorize before constructing or running the generated query;
//  3. the tenant comes from the Grant;
//  4. nothing reaches the database without passing through the first two.
//
// The fourth is checked by the handle these tests pass in. It wraps a nil
// *sql.DB, so any statement that were issued would panic and fail the test
// loudly -- which makes "the refusal happened before the Model" a fact the
// suite proves rather than a comment. The structural twin in audit_test.go
// keeps that order visible on every service method, including an allowed path.

// customerActions are the actions of the customer's side, and teamActions the
// team's: every one the two policies answer about.
var (
	customerActions = agenda.Actions()
	teamActions     = agenda.TeamActions()
	everyAction     = append(append([]security.Action(nil), customerActions...), teamActions...)
	systemActions   = []security.Action{agenda.AgendaSettle, agenda.AgendaRunTasks}
)

// administrator is the most privileged subject an application could produce
// without granting it an action. It is the one to test the default with: a
// policy that refuses an administrator who holds no action refuses everyone.
func administrator(tenant string) security.Subject {
	return security.Subject{ID: "user-1", Tenant: tenant, Roles: []string{"admin", "owner", "agenda.admin"}, Verified: true}
}

// granted is a subject who holds exactly these actions.
func granted(tenant, id string, actions ...security.Action) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true, Actions: actions}
}

func TestThePoliciesDenyEveryActionByDefault(t *testing.T) {
	t.Parallel()

	for _, action := range append(slices.Clone(everyAction), systemActions...) {
		for name, policy := range map[string]security.Policy[agenda.Booking]{
			"customer": agenda.BookingPolicy{},
			"team":     agenda.TeamPolicy{Realm: "platform"},
		} {
			for tenant := range map[string]bool{"acme": true, "platform": true} {
				_, err := security.Authorize(context.Background(), policy, administrator(tenant), action, agenda.Booking{})
				if !errors.Is(err, security.ErrForbidden) {
					t.Errorf("the %s policy allowed %s to an administrator of %s who was granted nothing: %v", name, action, tenant, err)
				}
			}
		}
	}
}

func TestNobodyHoldsTheApplicationsAndTheSchedulersActions(t *testing.T) {
	t.Parallel()

	// Settling a booking and running the tasks are the system's, issued by the
	// code that does them. A subject who carries the action is still refused it
	// by both policies, so no person can ask for it, and neither is offered to a
	// catalogue.
	for _, action := range systemActions {
		holder := granted("platform", "u", action)
		for _, policy := range []security.Policy[agenda.Booking]{agenda.BookingPolicy{}, agenda.TeamPolicy{Realm: "platform"}} {
			if _, err := security.Authorize(context.Background(), policy, holder, action, agenda.Booking{}); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("a policy allowed a person the system's action %s: %v", action, err)
			}
		}
		for _, list := range [][]security.Action{customerActions, teamActions} {
			if slices.Contains(list, action) {
				t.Fatalf("the system's action %s is offered to a catalogue", action)
			}
		}
	}
}

func TestTheTwoSetsOfActionsAreApart(t *testing.T) {
	t.Parallel()

	for _, action := range customerActions {
		if slices.Contains(teamActions, action) {
			t.Errorf("%s is in both sets, so a catalogue an organization grants from could hold a team action", action)
		}
	}
	for _, action := range append(slices.Clone(everyAction), systemActions...) {
		if !strings.HasPrefix(string(action), "agenda.") {
			t.Errorf("%s is not named after the package", action)
		}
	}
	if len(everyAction) != 7 {
		t.Errorf("%d actions are declared; the release notes name seven, and the two of the system", len(everyAction))
	}
}

func TestACustomerIsAllowedWhatTheyWereGrantedAndNothingMore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	own := agenda.Booking{ID: "b-1", TenantID: "acme", RequesterID: "user-ana"}
	others := agenda.Booking{ID: "b-2", TenantID: "acme", RequesterID: "user-bia"}
	policy := agenda.BookingPolicy{}

	for _, c := range []struct {
		name   string
		who    security.Subject
		action security.Action
		on     agenda.Booking
		want   bool
	}{
		{"book, granted", granted("acme", "user-ana", agenda.AgendaBook), agenda.AgendaBook, agenda.Booking{}, true},
		{"book, not granted", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaBook, agenda.Booking{}, false},
		{"book, on a stored booking", granted("acme", "user-ana", agenda.AgendaBook), agenda.AgendaBook, own, false},
		{"view own, the probe", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaViewOwn, agenda.Booking{}, true},
		{"view own, theirs", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaViewOwn, own, true},
		{"view own, somebody else's", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaViewOwn, others, false},
		{"view own, another space's", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaViewOwn, agenda.Booking{ID: "b-3", TenantID: "globex", RequesterID: "user-ana"}, false},
		{"view space, somebody else's", granted("acme", "user-boss", agenda.AgendaViewSpace), agenda.AgendaViewSpace, others, true},
		{"view space, another space's", granted("acme", "user-boss", agenda.AgendaViewSpace), agenda.AgendaViewSpace, agenda.Booking{ID: "b-3", TenantID: "globex"}, false},
		{"change, theirs", granted("acme", "user-ana", agenda.AgendaChange), agenda.AgendaChange, own, true},
		{"change, somebody else's", granted("acme", "user-ana", agenda.AgendaChange), agenda.AgendaChange, others, false},
		{"change, somebody else's, with the space", granted("acme", "user-boss", agenda.AgendaChange, agenda.AgendaViewSpace), agenda.AgendaChange, others, true},
		{"change, with only the right to read", granted("acme", "user-ana", agenda.AgendaViewOwn), agenda.AgendaChange, own, false},
		{"a team action", granted("acme", "user-ana", agenda.AgendaManage), agenda.AgendaManage, own, false},
	} {
		err := policy.Can(ctx, c.who, c.action, c.on)
		if (err == nil) != c.want {
			t.Errorf("%s: got %v, want allowed=%v", c.name, err, c.want)
		}
	}
}

func TestThePolicyDeniesARecordOfAnotherTenant(t *testing.T) {
	t.Parallel()

	other := agenda.Booking{ID: "record-1", TenantID: "globex", RequesterID: "user-1"}
	everything := granted("acme", "user-1", customerActions...)

	for _, action := range customerActions {
		err := agenda.BookingPolicy{}.Can(context.Background(), everything, action, other)
		if err == nil {
			t.Fatalf("the policy allowed %s on a record belonging to another tenant", action)
		}
		// The message is asserted because the tenant check is the one refusal
		// that has to survive somebody opening the actions below it.
		if !strings.Contains(err.Error(), "another tenant") {
			t.Fatalf("the refusal did not name the tenant: %v", err)
		}
	}
}

func TestThePolicyDeniesAGuestAndNobody(t *testing.T) {
	t.Parallel()

	for _, action := range everyAction {
		for name, policy := range map[string]security.Policy[agenda.Booking]{"customer": agenda.BookingPolicy{}, "team": agenda.TeamPolicy{Realm: "acme"}} {
			if _, err := security.Authorize(context.Background(), policy, security.Guest("acme"), action, agenda.Booking{}); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("the %s policy allowed a guest %s: %v", name, action, err)
			}
			// The zero Subject is a session that failed to load, not an
			// anonymous reader, and it is refused before the policy is consulted.
			if _, err := security.Authorize(context.Background(), policy, security.Subject{}, action, agenda.Booking{}); !errors.Is(err, security.ErrForbidden) {
				t.Fatalf("the %s policy allowed the empty subject %s: %v", name, action, err)
			}
		}
	}
}

func TestTheTeamPolicyAllowsTheTeamAndOnlyTheTeam(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	policy := agenda.TeamPolicy{Realm: "platform"}
	inAnyTenant := agenda.Booking{ID: "b-1", TenantID: "globex", RequesterID: "user-gil"}

	// A member of the team reads and acts on a booking of any tenant: crossing
	// tenants is what the team does.
	for _, action := range teamActions {
		if err := policy.Can(ctx, granted("platform", "team-1", action), action, inAnyTenant); err != nil {
			t.Errorf("a team member granted %s was refused it: %v", action, err)
		}
		// An action opens itself and no other.
		for _, other := range teamActions {
			if other != action && policy.Can(ctx, granted("platform", "team-1", action), other, inAnyTenant) == nil {
				t.Errorf("%s opened %s", action, other)
			}
		}
	}

	// And the customer's actions are not the team's.
	for _, action := range customerActions {
		if policy.Can(ctx, granted("platform", "team-1", action), action, inAnyTenant) == nil {
			t.Errorf("the team policy answered the customer's action %s", action)
		}
	}

	all := granted("platform", "team-1", teamActions...)
	cases := map[string]security.Subject{
		"a subject of another tenant": granted("acme", "team-1", teamActions...),
		"a subject viewing as somebody": func() security.Subject {
			s := all
			s.Impersonator = &security.Subject{ID: "operator"}
			return s
		}(),
	}
	for name, who := range cases {
		for _, action := range teamActions {
			if err := policy.Can(ctx, who, action, inAnyTenant); err == nil {
				t.Errorf("the team policy allowed %s %s", name, action)
			}
		}
	}
	// With no realm, nobody is the team.
	if err := (agenda.TeamPolicy{}).Can(ctx, granted("", "team-1", teamActions...), agenda.AgendaViewAll, inAnyTenant); err == nil {
		t.Error("a policy with no realm allowed somebody")
	}
}

func TestTheGrantsOfReadingOnlyReadAndWhatWritesWrites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, c := range []struct {
		policy security.Policy[agenda.Booking]
		who    security.Subject
		action security.Action
		writes bool
	}{
		{agenda.BookingPolicy{}, granted("acme", "u", agenda.AgendaViewOwn), agenda.AgendaViewOwn, false},
		{agenda.BookingPolicy{}, granted("acme", "u", agenda.AgendaViewSpace), agenda.AgendaViewSpace, false},
		{agenda.BookingPolicy{}, granted("acme", "u", agenda.AgendaBook), agenda.AgendaBook, true},
		{agenda.BookingPolicy{}, granted("acme", "u", agenda.AgendaChange), agenda.AgendaChange, true},
		{agenda.TeamPolicy{Realm: "platform"}, granted("platform", "u", agenda.AgendaViewAll), agenda.AgendaViewAll, false},
		{agenda.TeamPolicy{Realm: "platform"}, granted("platform", "u", agenda.AgendaManage), agenda.AgendaManage, true},
		{agenda.TeamPolicy{Realm: "platform"}, granted("platform", "u", agenda.AgendaConfigure), agenda.AgendaConfigure, true},
	} {
		g, err := security.Authorize(ctx, c.policy, c.who, c.action, agenda.Booking{})
		if err != nil {
			t.Fatalf("%s: %v", c.action, err)
		}
		// A Grant issued to read cannot write: the database refuses it.
		if got := auth.Writable(g) == nil; got != c.writes {
			t.Errorf("a Grant issued for %s is writable = %v, want %v", c.action, got, c.writes)
		}
	}
}

// nilHandle is a handle over no database.
//
// Any statement issued through it panics, which is what makes these tests
// prove that the refusal came first: a service that reached the Model before
// authorizing would crash here rather than pass.
func nilHandle() *data.DB { return data.Wrap(nil, data.DialectSQLite) }

func TestTheServicesRefuseBeforeReachingTheModel(t *testing.T) {
	t.Parallel()

	// A nil handle makes even construction of Bookings panic, at the
	// GetQueryGrammar the query asks the handle for. This catches moving the
	// generated query constructor -- not only its terminal -- ahead of
	// authorization.
	cfg := agenda.Config{TeamTenant: "platform", Zone: time.UTC}
	customers := agenda.NewBookingService(nil, cfg)
	team := agenda.NewTeamService(nil, cfg)
	app := agenda.NewAppService(nil, cfg)
	ctx := context.Background()
	nobody := administrator("acme")
	nobodyOfTheTeam := administrator("platform")
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	start := day.Add(10 * time.Hour)
	ordinary := security.SystemGrant(agenda.AgendaViewOwn, "acme")

	for name, err := range map[string]error{
		"Services": second(customers.Services(ctx, nobody)),
		"Slots":    second(customers.Slots(ctx, nobody, "reconfiguracao", day)),
		"Book":     second(customers.Book(ctx, nobody, agenda.BookInput{Service: "reconfiguracao", Start: start})),
		"Bookings": second(customers.Bookings(ctx, nobody, agenda.ListQuery{})),
		"Booking":  second(customers.Booking(ctx, nobody, "b-1")),
		"Confirm":  second(customers.Confirm(ctx, nobody, "b-1")),
		"Move":     second(customers.Reschedule(ctx, nobody, "b-1", start)),
		"Cancel":   second(customers.Cancel(ctx, nobody, "b-1", "")),
		"CanBook":  customers.CanBook(ctx, nobody),

		"team Bookings":  second(team.Bookings(ctx, nobodyOfTheTeam, agenda.TeamQuery{})),
		"team Booking":   second(team.Booking(ctx, nobodyOfTheTeam, "b-1")),
		"team Release":   second(team.Release(ctx, nobodyOfTheTeam, "b-1", "cortesia")),
		"team Complete":  second(team.Complete(ctx, nobodyOfTheTeam, "b-1", "")),
		"team NoShow":    second(team.NoShow(ctx, nobodyOfTheTeam, "b-1")),
		"team Cancel":    second(team.Cancel(ctx, nobodyOfTheTeam, "b-1", "")),
		"team Move":      second(team.Reschedule(ctx, nobodyOfTheTeam, "b-1", start)),
		"team Services":  second(team.Services(ctx, nobodyOfTheTeam, agenda.ConfigQuery{})),
		"team Service":   second(team.Service(ctx, nobodyOfTheTeam, "s-1")),
		"team NewSvc":    second(team.CreateService(ctx, nobodyOfTheTeam, agenda.ServiceInput{Key: "k", Name: "n", DurationMinutes: 40})),
		"team EditSvc":   second(team.UpdateService(ctx, nobodyOfTheTeam, "s-1", agenda.ServiceInput{Key: "k", Name: "n", DurationMinutes: 40})),
		"team DropSvc":   team.DeleteService(ctx, nobodyOfTheTeam, "s-1"),
		"team Hours":     second(team.Hours(ctx, nobodyOfTheTeam, agenda.ConfigQuery{})),
		"team NewHour":   second(team.CreateHour(ctx, nobodyOfTheTeam, agenda.HourInput{Weekday: 1, StartMinute: 540, EndMinute: 600})),
		"team DropHour":  team.DeleteHour(ctx, nobodyOfTheTeam, "h-1"),
		"team Closures":  second(team.Closures(ctx, nobodyOfTheTeam, agenda.ConfigQuery{})),
		"team NewClose":  second(team.CreateClosure(ctx, nobodyOfTheTeam, agenda.ClosureInput{StartsAt: start, EndsAt: start.Add(time.Hour)})),
		"team DropClose": team.DeleteClosure(ctx, nobodyOfTheTeam, "c-1"),
		"team Configure": team.CanConfigure(ctx, nobodyOfTheTeam),

		// The application's hooks take a Grant, and a Grant issued for any other
		// action is refused before the Model.
		"app Find":   second(app.Find(ctx, ordinary, "AG-000001")),
		"app Settle": second(app.Settle(ctx, ordinary, "b-1", "pix-1")),
	} {
		if !errors.Is(err, security.ErrForbidden) {
			t.Errorf("%s reached the Model before the policy refusal: %v", name, err)
		}
	}
}

// second is the error of a call that answers a value and an error.
func second[T any](_ T, err error) error { return err }

// recordingHandle runs nothing and keeps every statement it is handed, so a
// test reads what the table compiles instead of reading its settings. The table
// keeps its settings to itself; what it writes is the behaviour they decide.
type recordingHandle struct {
	*data.DB
	statements []recordedStatement
}

type recordedStatement struct {
	sql      string
	bindings []any
}

// newRecordingHandle borrows the grammar and the processor of an SQLite handle
// over no database, and answers every statement itself.
func newRecordingHandle() *recordingHandle { return &recordingHandle{DB: nilHandle()} }

func (h *recordingHandle) record(sql string, bindings []any) {
	h.statements = append(h.statements, recordedStatement{sql: sql, bindings: slices.Clone(bindings)})
}

func (h *recordingHandle) Select(_ context.Context, sql string, bindings []any, _ bool) ([]query.Record, error) {
	h.record(sql, bindings)
	return nil, nil
}

func (h *recordingHandle) Insert(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

func (h *recordingHandle) Update(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Delete(_ context.Context, sql string, bindings []any) (int64, error) {
	h.record(sql, bindings)
	return 1, nil
}

func (h *recordingHandle) Statement(_ context.Context, sql string, bindings []any) (bool, error) {
	h.record(sql, bindings)
	return true, nil
}

// first is the first recorded statement that starts with verb.
func (h *recordingHandle) first(t *testing.T, verb string) recordedStatement {
	t.Helper()
	for _, statement := range h.statements {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(statement.sql)), verb) {
			return statement
		}
	}
	t.Fatalf("no %s statement was issued; recorded %v", verb, h.statements)
	return recordedStatement{}
}

// TestBookingsReturnsAWiredTenantScopedQuery holds what bookingTable declares,
// through what it makes the generated query do: the table it reads, a key the
// application writes and the engine never fills, and a tenant_id taken from the
// Grant on the way in and on the way out.
func TestBookingsReturnsAWiredTenantScopedQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handle := newRecordingHandle()
	table := agenda.Bookings(handle).Base().Table()
	if table.Name() != "agenda_bookings" {
		t.Fatalf("Bookings table = %q, want agenda_bookings", table.Name())
	}
	if key := table.MorphModel(handle); key.GetKeyName() != "id" || key.GetKeyType() != "string" {
		t.Fatalf("Bookings key is %q of type %q; want id, as text", key.GetKeyName(), key.GetKeyType())
	}

	row, err := agenda.Bookings(handle).New()
	if err != nil {
		t.Fatalf("building a row: %v", err)
	}
	if row.Table() != table {
		t.Fatal("Bookings returned an entity whose embedded Model is not wired to its table")
	}

	// The key is application-generated text: the insert carries the one the row
	// was given, and the engine is never asked for one.
	row.ID = "record-1"
	if _, err := row.Save(ctx, security.SystemGrant(agenda.AgendaBook, "acme")); err != nil {
		t.Fatalf("saving through the recording handle: %v", err)
	}
	insert := handle.first(t, "insert")
	if !slices.Contains(insert.bindings, any("record-1")) || row.ID != "record-1" {
		t.Fatalf("the insert %q %v does not write the application key, or the row lost it (%q)", insert.sql, insert.bindings, row.ID)
	}
	if !strings.Contains(insert.sql, "tenant_id") || !slices.Contains(insert.bindings, any("acme")) {
		t.Fatalf("the insert %q %v does not stamp tenant_id with the Grant's tenant", insert.sql, insert.bindings)
	}

	// And the tenant column scopes a read by the Grant's tenant.
	handle.statements = nil
	grant := security.SystemGrant(agenda.AgendaViewOwn, "acme")
	if _, err := agenda.Bookings(handle).WhereKey("record-1").First(ctx, grant); err != nil {
		t.Fatalf("reading through the recording handle: %v", err)
	}
	read := handle.first(t, "select")
	if !strings.Contains(read.sql, "tenant_id") || !slices.Contains(read.bindings, any("acme")) {
		t.Fatalf("the read %q %v is not filtered by tenant_id with the Grant's tenant", read.sql, read.bindings)
	}
}

// TestTheTeamsOwnTablesCarryNoTenantColumn holds the other half of the
// declaration: the services, the hours and the closures belong to no customer,
// so a read of them is not filtered by a tenant, and a write does not stamp one.
func TestTheTeamsOwnTablesCarryNoTenantColumn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	handle := newRecordingHandle()
	grant := security.SystemGrant(agenda.AgendaViewAll, "platform")
	for name, run := range map[string]func() error{
		"services": func() error { _, err := agenda.Services(handle).First(ctx, grant); return err },
		"hours":    func() error { _, err := agenda.Hours(handle).First(ctx, grant); return err },
		"closures": func() error { _, err := agenda.Closures(handle).First(ctx, grant); return err },
		"counters": func() error { _, err := agenda.Counters(handle).First(ctx, grant); return err },
	} {
		handle.statements = nil
		if err := run(); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if read := handle.first(t, "select"); strings.Contains(read.sql, "tenant_id") {
			t.Errorf("the read of %s %q is filtered by a tenant, which the table does not have", name, read.sql)
		}
	}
}

// TestTheMissingRecordIsTheModelsNotFound holds the one link between this
// package's sentinel and the router's answer. The router answers
// model.ErrModelNotFound with 404 and has never heard of ErrNotFound, so the
// sentinel is a 404 only while it wraps the model's.
func TestTheMissingRecordIsTheModelsNotFound(t *testing.T) {
	t.Parallel()

	if !errors.Is(agenda.ErrNotFound, model.ErrModelNotFound) {
		t.Fatal("ErrNotFound does not wrap model.ErrModelNotFound, so the router would answer a missing record with 500")
	}
}

func TestASystemGrantWithoutATenantReachesNothing(t *testing.T) {
	t.Parallel()

	// A system grant with no tenant names no customer. The Model refuses it
	// while preparing the query, before the nil handle can issue a statement.
	_, err := agenda.Bookings(nilHandle()).WhereKey("record-1").First(
		context.Background(), security.SystemGrant(agenda.AgendaViewOwn, ""))
	if !errors.Is(err, model.ErrNoTenant) {
		t.Fatalf("a system grant with no tenant returned %v, want ErrNoTenant", err)
	}
}

func TestTheTenantComesFromTheGrant(t *testing.T) {
	t.Parallel()

	g := security.SystemGrant(agenda.AgendaViewOwn, "acme")
	if got := data.Tenant(g); got != "acme" {
		t.Fatalf("data.Tenant(g) = %q, want %q", got, "acme")
	}
	// And a Grant nobody issued carries no tenant at all, so a statement that
	// took its tenant from anywhere else would be reading rows this Grant does
	// not name.
	if got := data.Tenant(security.Grant{}); got != "" {
		t.Fatalf("the zero Grant carries the tenant %q, want none", got)
	}
}

func TestTheBookingsOwnRules(t *testing.T) {
	t.Parallel()

	now := mustTime("2026-11-10T12:00:00Z")
	start := now.Add(48 * time.Hour)
	end := start.Add(40 * time.Minute)
	hold := now.Add(24 * time.Hour)
	fresh := func(status agenda.Status) *agenda.Booking {
		b := &agenda.Booking{Code: "AG-000123", Status: status, StartsAt: start, EndsAt: end}
		if status == agenda.StatusAwaitingPayment {
			b.HoldUntil = &hold
		}
		return b
	}

	// What holds a slot.
	for status, want := range map[agenda.Status]bool{
		agenda.StatusAwaitingPayment: true, agenda.StatusScheduled: true, agenda.StatusConfirmed: true,
		agenda.StatusCompleted: false, agenda.StatusCancelled: false, agenda.StatusExpired: false, agenda.StatusNoShow: false,
	} {
		if got := fresh(status).Occupies(now); got != want {
			t.Errorf("a %s booking occupies = %v, want %v", status, got, want)
		}
	}
	lapsed := fresh(agenda.StatusAwaitingPayment)
	if lapsed.Occupies(hold) || !lapsed.Lapsed(hold) || lapsed.Occupies(hold.Add(time.Second)) {
		t.Error("a hold that is over still holds the slot")
	}

	// The protocol is the sentence people say.
	if got := fresh(agenda.StatusScheduled).Code; got != "AG-000123" {
		t.Errorf("the code is %q", got)
	}

	// Confirming.
	b := fresh(agenda.StatusScheduled)
	if err := b.Confirm(now); err != nil || b.Status != agenda.StatusConfirmed || b.ConfirmedAt == nil || !b.ConfirmedAt.Equal(now) {
		t.Fatalf("confirming: %v, %+v", err, b)
	}
	if b.Confirm(now) == nil || fresh(agenda.StatusAwaitingPayment).Confirm(now) == nil || fresh(agenda.StatusScheduled).Confirm(start) == nil {
		t.Error("a booking was confirmed twice, before its payment or after its start")
	}

	// A payment, or a courtesy, makes the hold permanent.
	b = fresh(agenda.StatusAwaitingPayment)
	if err := b.Settle("pix-1", now); err != nil || b.Status != agenda.StatusScheduled || b.HoldUntil != nil || b.PaymentRef != "pix-1" {
		t.Fatalf("settling: %v, %+v", err, b)
	}
	if b.Settle("pix-2", now) == nil {
		t.Error("a scheduled booking took a second payment")
	}
	b = fresh(agenda.StatusExpired)
	if err := b.Release("team-1", "cortesia", now); err != nil || b.Status != agenda.StatusScheduled || b.ReleasedBy != "team-1" || b.ReleasedReason != "cortesia" {
		t.Fatalf("releasing: %v, %+v", err, b)
	}
	if fresh(agenda.StatusCancelled).Release("team-1", "x", now) == nil || fresh(agenda.StatusConfirmed).Settle("p", now) == nil {
		t.Error("a booking that cannot take a payment took one")
	}

	// Moving keeps the payment and the code, and starts the reminder over.
	b = fresh(agenda.StatusConfirmed)
	reminded := now
	b.RemindedAt, b.PaymentRef = &reminded, "pix-1"
	if err := b.Move(start.Add(24*time.Hour), end.Add(24*time.Hour), now); err != nil {
		t.Fatalf("moving: %v", err)
	}
	if b.Status != agenda.StatusScheduled || b.ConfirmedAt != nil || b.RemindedAt != nil || b.Reschedules != 1 || b.PaymentRef != "pix-1" || b.Code != "AG-000123" {
		t.Errorf("moving left %+v", b)
	}
	if b.Move(end, start, now) == nil || fresh(agenda.StatusCompleted).Move(start, end, now) == nil {
		t.Error("a booking was moved to end before it starts, or after it was done")
	}

	// The window a customer can still change in.
	b = fresh(agenda.StatusScheduled)
	if !b.ChangeOpen(now, 2*time.Hour) || !b.ChangeOpen(start.Add(-2*time.Hour), 2*time.Hour) || b.ChangeOpen(start.Add(-2*time.Hour+time.Second), 2*time.Hour) {
		t.Error("the change window does not close exactly ChangeUntil before the start")
	}
	if fresh(agenda.StatusCompleted).ChangeOpen(now, time.Hour) {
		t.Error("a completed booking can still be changed")
	}

	// Cancelling, completing, absences and expiry.
	b = fresh(agenda.StatusScheduled)
	if err := b.Cancel(agenda.ByCustomer, "mudei de ideia", now); err != nil || b.Status != agenda.StatusCancelled || b.CancelledBy != agenda.ByCustomer {
		t.Fatalf("cancelling: %v, %+v", err, b)
	}
	if b.Cancel(agenda.ByTeam, "", now) == nil {
		t.Error("a cancelled booking was cancelled again")
	}
	b = fresh(agenda.StatusConfirmed)
	if err := b.Complete("team-1", "feito", now); err != nil || b.Status != agenda.StatusCompleted || b.CompletedAt == nil || b.CompletedBy != "team-1" {
		t.Fatalf("completing: %v, %+v", err, b)
	}
	if b.Complete("team-1", "", now) == nil || fresh(agenda.StatusAwaitingPayment).Complete("t", "", now) == nil {
		t.Error("a booking was completed twice, or before it held its slot")
	}
	b = fresh(agenda.StatusScheduled)
	if b.MarkNoShow(now) == nil {
		t.Error("an absence was marked before the start")
	}
	if err := b.MarkNoShow(start); err != nil || b.Status != agenda.StatusNoShow {
		t.Errorf("marking an absence: %v", err)
	}
	b = fresh(agenda.StatusAwaitingPayment)
	if b.Expire(now) == nil {
		t.Error("a hold that is not over expired")
	}
	if err := b.Expire(hold); err != nil || b.Status != agenda.StatusExpired {
		t.Errorf("expiring: %v", err)
	}
}

func TestTheConfigurationRefusesWhatCannotWork(t *testing.T) {
	t.Parallel()

	good := agenda.Config{TeamTenant: "platform", Zone: time.UTC}
	for name, change := range map[string]func(*agenda.Config){
		"no team tenant":     func(c *agenda.Config) { c.TeamTenant = "" },
		"no zone":            func(c *agenda.Config) { c.Zone = nil },
		"relative prefix":    func(c *agenda.Config) { c.Prefix = "agenda" },
		"page size too big":  func(c *agenda.Config) { c.PageSize = agenda.MaxPageSize + 1 },
		"negative page size": func(c *agenda.Config) { c.PageSize = -1 },
		"capacity too big":   func(c *agenda.Config) { c.Capacity = agenda.MaxCapacity + 1 },
		"step in seconds":    func(c *agenda.Config) { c.SlotStep = 90 * time.Second },
		"horizon too far":    func(c *agenda.Config) { c.Horizon = agenda.MaxHorizon + time.Hour },
		"negative hold":      func(c *agenda.Config) { c.PaymentHold = -time.Hour },
	} {
		cfg := good
		change(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("the configuration with %s was accepted", name)
		}
	}

	// The configuration an application that changes nothing writes names the
	// team's tenant and the zone, and no other: every customer's tenant comes
	// from the Grant.
	if err := good.Validate(); err != nil {
		t.Fatalf("the smallest configuration was refused: %v", err)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
