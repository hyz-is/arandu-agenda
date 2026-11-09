package feature_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"
	hedatabase "github.com/arandu-io/hesape/database"
	sqliteconnector "github.com/arandu-io/hesape/database/connectors/sqlite"
	"github.com/arandu-io/hesape/database/migrations"
	activitylog "github.com/hyz-is/arandu-activitylog"

	agenda "github.com/hyz-is/arandu-agenda"
)

// dsn opens a database file the way the tests need it: the write lock taken
// when a transaction begins, readers beside a writer, and a wait instead of an
// error when the lock is held.
const dsn = "?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)"

// appKey is the key a session store is built over. Any thirty-two bytes will
// do here; a real application reads its own from the environment.
const appKey = "0123456789abcdef0123456789abcdef"

// realm is the tenant the platform team signs in to.
const realm = "platform"

// signedIn are the subjects the tests started sessions for, by tenant and
// identifier: what a session store's resolver answers, as an application's
// reads the account's roles and actions again on every request.
var signedIn sync.Map

// resolveSubjects is the session store's resolver for these tests.
func resolveSubjects(_ context.Context, who security.Subject) (security.Subject, error) {
	if s, ok := signedIn.Load(who.Tenant + "/" + who.ID); ok {
		return s.(security.Subject), nil
	}
	return who, nil
}

// remember makes a subject the one the resolver answers for its account.
func remember(s security.Subject) security.Subject {
	signedIn.Store(s.Tenant+"/"+s.ID, s)
	return s
}

// notifications records what the Notifier was told, and can be made to fail.
type notifications struct {
	mu     sync.Mutex
	events []agenda.Event
	fail   error
}

func (n *notifications) Notify(_ context.Context, e agenda.Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, e)
	return n.fail
}

func (n *notifications) all() []agenda.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]agenda.Event(nil), n.events...)
}

// of are the events of one kind.
func (n *notifications) of(kind agenda.EventKind) []agenda.Event {
	var out []agenda.Event
	for _, e := range n.all() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// directory is the application's Directory.
type directory map[string]agenda.Person

func (d directory) Person(_ context.Context, _, id string) (agenda.Person, error) {
	if p, ok := d[id]; ok {
		return p, nil
	}
	return agenda.Person{}, agenda.ErrNoAccount
}

// subjects is the application's SubjectResolver: the cameras of each space.
type subjects map[string]string

func (s subjects) Resolve(_ context.Context, tenant string, subject agenda.BookingSubject) (string, error) {
	if label, ok := s[tenant+"/"+subject.Type+"/"+subject.ID]; ok {
		return label, nil
	}
	return "", errors.New("not the tenant's")
}

// clock is a time the tests move by hand.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// zone is Sao Paulo, where the team works. It has had no daylight-saving time
// since 2019, so a local time of day is the same offset from UTC all year.
var zone = func() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("BRT", -3*60*60)
	}
	return loc
}()

// local is a time on the team's wall clock.
func local(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, zone)
}

// world is one application with this package installed over a real database.
type world struct {
	sql      *sql.DB
	conn     *hedatabase.Connection
	db       *data.DB
	sessions *security.SessionStore
	module   *agenda.Module
	bookings *agenda.BookingService
	team     *agenda.TeamService
	app      *agenda.AppService
	notices  *notifications
	logs     *syncBuffer
	clock    *clock
}

// Monday 9 November 2026, noon in Sao Paulo: the moment every test starts at.
// The first day with free times is Tuesday the 10th from noon, because of the
// day of notice, and Wednesday the 11th from 09:00.
func startOfTime() time.Time { return local(2026, time.November, 9, 12, 0).UTC() }

// install opens a fresh SQLite database, applies the migrations of the log and
// of this package, builds the module over it and gives the team a week to work
// in and the two services the application offers.
func install(t *testing.T) world {
	t.Helper()
	return installWith(t, nil)
}

// installWith is install with the module's configuration changed first.
func installWith(t *testing.T, change func(*agenda.Config)) world {
	t.Helper()
	w := bare(t, change)
	seed(t, w)
	return w
}

// bare is install without the team's week and services.
func bare(t *testing.T, change func(*agenda.Config)) world {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agenda.sqlite")
	handle, err := sql.Open(sqliteconnector.SQLiteConnector{}.DriverName(), "file:"+path+dsn)
	if err != nil {
		t.Fatalf("opening SQLite: %v", err)
	}
	handle.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = handle.Close() })

	connection := hedatabase.NewConnection(handle, path, "", map[string]any{
		"driver": string(hedatabase.DialectSQLite),
		"name":   "agenda-test",
	})
	db := data.Wrap(handle, data.DialectSQLite)
	sessions := security.NewSessionStore([]byte(appKey), time.Hour, false, security.NewMemoryBackend())
	sessions.ResolveSubjects(resolveSubjects)

	activity, err := activitylog.New(activitylog.Config{Tenant: realm, PlatformPolicy: nil}, db, sessions)
	if err != nil {
		t.Fatalf("building arandu-activitylog: %v", err)
	}

	notices := &notifications{}
	logs := &syncBuffer{}
	held := &clock{now: startOfTime()}
	cfg := agenda.Config{
		TeamTenant: realm,
		Zone:       zone,
		Notifier:   notices,
		Activity:   activity.Logger(),
		Directory: directory{
			"user-ana": {ID: "user-ana", Name: "Ana Souza"}, "user-bia": {ID: "user-bia", Name: "Bia Lima"},
			"user-gil": {ID: "user-gil", Name: "Gil Rocha"}, "team-1": {ID: "team-1", Name: "Rita Prado"},
		},
		Now:    held.Now,
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}
	if change != nil {
		change(&cfg)
	}
	module, err := agenda.New(cfg, db, sessions)
	if err != nil {
		t.Fatalf("building the module: %v", err)
	}
	migrations := hedatabase.ForMigrations(connection)
	for _, migration := range append(activity.Migrations(), module.Migrations()...) {
		if err := migration.Up(context.Background(), migrations); err != nil {
			t.Fatalf("applying %s: %v", migration.GetName(), err)
		}
	}
	return world{
		sql: handle, conn: connection, db: db, sessions: sessions, module: module,
		bookings: module.Bookings(), team: module.Team(), app: module.App(), notices: notices, logs: logs, clock: held,
	}
}

// seed gives the team a week -- Monday to Friday, 09:00 to 12:00 and 14:00 to
// 18:00 -- and the two services of the application: the reconfiguration, which
// is paid, and the installation, which is free.
func seed(t *testing.T, w world) {
	t.Helper()
	ctx := context.Background()
	who := agent("team-1")
	for _, day := range []int{1, 2, 3, 4, 5} {
		for _, window := range [][2]int{{9 * 60, 12 * 60}, {14 * 60, 18 * 60}} {
			if _, err := w.team.CreateHour(ctx, who, agenda.HourInput{Weekday: day, StartMinute: window[0], EndMinute: window[1]}); err != nil {
				t.Fatalf("seeding the week: %v", err)
			}
		}
	}
	for _, in := range []agenda.ServiceInput{
		{Key: "reconfiguracao", Name: "Reconfiguração de câmera", Description: "Ajuste de uma câmera já instalada.", DurationMinutes: 40, RequiresPayment: true, Active: true},
		{Key: "instalacao", Name: "Instalação de câmera", DurationMinutes: 40, RequiresPayment: false, Active: true},
	} {
		if _, err := w.team.CreateService(ctx, who, in); err != nil {
			t.Fatalf("seeding the services: %v", err)
		}
	}
}

// The subjects of the tests. A customer holds the customer's actions; the team
// member holds the team's. Roles are written on them to prove the policy does
// not read them: a role opens nothing.

// member is a customer of a space with the actions of someone who books for
// themselves: their own bookings, nothing of the rest of the space.
func member(tenant, id string) security.Subject {
	return security.Subject{ID: id, Tenant: tenant, Verified: true, Actions: []security.Action{
		agenda.AgendaBook, agenda.AgendaViewOwn, agenda.AgendaChange,
	}}
}

// owner is a customer who also reads the whole space's bookings.
func owner(tenant, id string) security.Subject {
	s := member(tenant, id)
	s.Actions = append(s.Actions, agenda.AgendaViewSpace)
	return s
}

// agent is a member of the platform team with every action of the team.
func agent(id string) security.Subject {
	return security.Subject{ID: id, Tenant: realm, Verified: true, Roles: []string{"admin"}, Actions: agenda.TeamActions()}
}

// tuesday is a day with free times from the first noon on: the day of notice
// has passed.
var (
	tuesday   = time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC)
	wednesday = time.Date(2026, time.November, 11, 0, 0, 0, 0, time.UTC)
	thursday  = time.Date(2026, time.November, 12, 0, 0, 0, 0, time.UTC)
	saturday  = time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)
)

// book books a start as the subject and fails the test if it cannot.
func book(t *testing.T, w world, who security.Subject, service string, start time.Time) *agenda.Booking {
	t.Helper()
	booked, err := w.bookings.Book(context.Background(), who, agenda.BookInput{
		Service: service, Start: start, Subject: agenda.BookingSubject{Type: "camera", ID: "42", Label: "Portaria"}, Note: "A imagem está escura.",
	})
	if err != nil {
		t.Fatalf("booking %s at %s: %v", service, start, err)
	}
	return booked
}

// firstSlot is the first free start of a day for a service.
func firstSlot(t *testing.T, w world, who security.Subject, service string, day time.Time) time.Time {
	t.Helper()
	slots, err := w.bookings.Slots(context.Background(), who, service, day)
	if err != nil || len(slots) == 0 {
		t.Fatalf("the free times of %s: %v, %v", day.Format("2006-01-02"), slots, err)
	}
	return slots[0]
}

// requireErr fails unless err wraps want.
func requireErr(t *testing.T, err, want error, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: got %v, want %v", what, err, want)
	}
}

// entry is a row of the activity log, as the tests read it.
type entry struct {
	Event, Description, Causer, Subject, Properties string
}

// entries are the rows the log holds for a tenant, oldest first. They are read
// with a statement of the test's own: the log's policy denies reading by
// default, and what these tests ask is only what was written.
func entries(t *testing.T, w world, tenant string) []entry {
	t.Helper()
	rows, err := w.sql.Query(`SELECT COALESCE(event, ''), description, COALESCE(causer_id, ''), COALESCE(subject_id, ''), COALESCE(properties, '')
		FROM activity_log WHERE tenant_id = ? ORDER BY created_at, id`, tenant)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	defer rows.Close()
	var out []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Event, &e.Description, &e.Causer, &e.Subject, &e.Properties); err != nil {
			t.Fatalf("reading a log row: %v", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return out
}

// events are the names of the entries of a tenant.
func events(t *testing.T, w world, tenant string) []string {
	t.Helper()
	var out []string
	for _, e := range entries(t, w, tenant) {
		out = append(out, e.Event)
	}
	return out
}

// lastEntry is the latest entry of a tenant with that event, or the test fails.
func lastEntry(t *testing.T, w world, tenant, event string) entry {
	t.Helper()
	found := entry{}
	ok := false
	for _, e := range entries(t, w, tenant) {
		if e.Event == event {
			found, ok = e, true
		}
	}
	if !ok {
		t.Fatalf("the log of %s has no %s entry; it has %v", tenant, event, events(t, w, tenant))
	}
	return found
}

// syncBuffer is a log the tests read what the package could not return.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// hedatabaseFor is the connection a migration is run on.
func hedatabaseFor(w world) migrations.Connection { return hedatabase.ForMigrations(w.conn) }
