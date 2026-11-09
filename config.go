package agenda

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	fhttp "github.com/arandu-io/framework/http"
	"github.com/arandu-io/framework/security"
	"github.com/arandu-io/hesape/translation"
	activitylog "github.com/hyz-is/arandu-activitylog"
)

// The defaults for the optional settings. They are constants rather than
// literals inside Config.withDefaults, so the value a reader finds here is the
// value the package uses.
const (
	// DefaultPrefix is where the customer's routes are mounted when Config
	// leaves Prefix empty.
	DefaultPrefix = "/agenda"
	// DefaultTeamPrefix is where the team's routes are mounted when Config
	// leaves TeamPrefix empty.
	DefaultTeamPrefix = "/admin/agenda"
	// DefaultPageSize is how many rows one page of a list answers with when
	// Config leaves PageSize at zero.
	DefaultPageSize = 25
	// MaxPageSize is the ceiling PageSize is refused above. A page nobody
	// bounded is a page that reads the whole table on the day the table is
	// large.
	MaxPageSize = 100
	// DefaultLocale is the language of the screens, the messages and the
	// activity descriptions when Config.Locale names none.
	DefaultLocale = "pt-BR"
	// FallbackLocale answers every line the locale in use does not have.
	FallbackLocale = "en"
	// DefaultCapacity is how many bookings one slot takes.
	DefaultCapacity = 1
	// MaxCapacity is the ceiling Capacity is refused above.
	MaxCapacity = 50
	// DefaultSlotStep is the distance between two starts offered on a day.
	DefaultSlotStep = 20 * time.Minute
	// DefaultMinNotice is how far ahead of now the earliest bookable start is.
	DefaultMinNotice = 24 * time.Hour
	// DefaultHorizon is how far ahead of now the latest bookable start is.
	DefaultHorizon = 30 * 24 * time.Hour
	// MaxHorizon is the ceiling Horizon is refused above.
	MaxHorizon = 366 * 24 * time.Hour
	// DefaultPaymentHold is how long an unpaid booking keeps its slot.
	DefaultPaymentHold = 24 * time.Hour
	// DefaultRemindBefore is how long before the start the reminder goes.
	DefaultRemindBefore = 3 * time.Hour
	// DefaultChangeUntil is how long before the start a customer can still
	// reschedule or cancel.
	DefaultChangeUntil = 2 * time.Hour
	// DefaultLocation is the name of the zone the screens are written in when
	// the application passes none. The zone is required, so it is only the
	// value a documentation example uses.
	DefaultLocation = "America/Sao_Paulo"
)

// reservedPrefix is the namespace the framework keeps for itself.
const reservedPrefix = "/_arandu"

// Config is what the application passes when it wires this package.
//
// A typed struct rather than a map: a misspelled key in a map is a setting that
// silently keeps its default, and the failure shows up as behaviour nobody
// asked for rather than as an error. Here a field that does not exist does not
// compile.
//
// It names no tenant for the customers, and that is deliberate. Every route
// sits behind RequireAuth, so the tenant of every customer statement is the one
// the Grant carries, from the session of whoever is asking; a tenant written
// here would be a second place for one to come from. The one tenant it does
// name is the team's own, TeamTenant, because the team is not a customer.
type Config struct {
	// TeamTenant is the tenant the platform's own team signs in to: the realm
	// of the operator console. A team member is a subject of this tenant who
	// holds the team's actions, and the team policy refuses any other tenant's
	// subject whatever actions they carry. The services, the weekly hours and
	// the closures are the team's, and only a subject of this tenant changes
	// them. It comes from the application's own configuration, never from a
	// request. Required.
	TeamTenant string

	// Prefix is the path the customer's routes are mounted under. Empty means
	// DefaultPrefix.
	Prefix string

	// TeamPrefix is the path the team's routes are mounted under. Empty means
	// DefaultTeamPrefix. The two sides are separate: the team's session is
	// valid on this side only, and the customer's on the other.
	TeamPrefix string

	// Paths are the segments every route is built from, under Prefix and
	// TeamPrefix. A field left empty keeps its DefaultPaths value;
	// PortuguesePaths is the whole set in Brazilian Portuguese.
	Paths Paths

	// Zone is the timezone the team's day is written in: the weekly hours are
	// "09:00 to 18:00" in it, a day is midnight to midnight in it, and every
	// screen writes an instant in it. Required: a schedule with no zone has no
	// meaning, and a default here would be a wrong answer for somebody.
	Zone *time.Location

	// Capacity is how many bookings one slot takes at the same time. Zero means
	// DefaultCapacity.
	Capacity int

	// SlotStep is the distance between two starts offered on a day: 20 minutes
	// offers 09:00, 09:20, 09:40. Zero means DefaultSlotStep. It has to be a
	// whole number of minutes.
	SlotStep time.Duration

	// MinNotice is the earliest bookable start from now: with 24 hours, nothing
	// that starts before this time tomorrow is offered. Zero means
	// DefaultMinNotice; a negative value means no notice at all.
	MinNotice time.Duration

	// Horizon is the latest bookable start from now. Zero means
	// DefaultHorizon, and anything above MaxHorizon is refused.
	Horizon time.Duration

	// PaymentHold is how long a booking that waits for payment keeps its slot
	// before ExpireHolds frees it. Zero means DefaultPaymentHold.
	PaymentHold time.Duration

	// RemindBefore is how long before the start Remind tells the customer. Zero
	// means DefaultRemindBefore.
	RemindBefore time.Duration

	// ChangeUntil is how long before the start a customer can still reschedule
	// or cancel. Zero means DefaultChangeUntil; a negative value means up to
	// the start.
	ChangeUntil time.Duration

	// PageSize is how many rows one page of a list answers with. Zero means
	// DefaultPageSize, and anything above MaxPageSize is refused rather than
	// clamped: a number somebody wrote and did not get is worse than a number
	// somebody wrote and was told about.
	PageSize int

	// Middleware runs on every route of this package after RequireAuth, in
	// order, and TeamMiddleware after it on the team's routes only. An
	// application whose permissions arandu-permission administers mounts its
	// Roles() in Middleware: RequireAuth puts the subject as the session
	// resolved it, without the actions arandu-permission resolves on each
	// request. A guard of the application's own side -- the operator console's
	// -- goes in TeamMiddleware. A guard answers "is this request on the right
	// side" and stops; the policy still decides.
	Middleware     []func(http.Handler) http.Handler
	TeamMiddleware []func(http.Handler) http.Handler

	// Notifier is told what happens to a booking. Nil tells nobody. See
	// Notifier. This package sends no message of its own.
	Notifier Notifier

	// Directory names the people of a booking. Optional; see Directory.
	Directory Directory

	// Subjects checks what a booking is about, and names it. Optional; see
	// SubjectResolver. Without one the label the request carried is kept as it
	// is, clipped.
	Subjects SubjectResolver

	// PaymentURL is the address where the customer pays a booking that waits for
	// payment: the application's own page, as the module knows nothing of money.
	// An empty answer shows the booking as waiting and offers no link. Nil is the
	// same as always empty.
	PaymentURL func(Summary) string

	// Activity is arandu-activitylog's logger, the application's one instance
	// (activityModule.Logger()). Every act that changes something is recorded
	// under the Grant it was authorized with, in the log named ActivityLogName.
	// Nil records nothing.
	Activity *activitylog.Logger

	// Translator is the application's own catalogue, asked before the one this
	// package ships, key by key: "agenda.screen.index_title". Optional.
	Translator *translation.Translator

	// Locale is the language every screen, validation message and activity
	// description of this package is written in. Empty means DefaultLocale. The
	// package ships the locales Locales() lists; any other reads the
	// application's Translator first and English after it.
	Locale string

	// TitleSeparator joins a screen's title to the application's name in the
	// document title: with " — ", the list is titled "Agendamentos — Alpr.sh".
	// Empty titles a screen with its own title alone.
	TitleSeparator string

	// Chrome is what the application's layout draws around a screen of this
	// package: its sidebar, its navigation, the account menu. It is called once
	// per screen, after RequireAuth and before the page is rendered, and what it
	// answers is carried on the page as Screen.Chrome, where a layout asserts
	// the interface it draws from. An error is returned as the handler's. Nil
	// draws the screens with the page hesape's view.New builds and nothing
	// more.
	Chrome func(ctx *fhttp.Context, req ChromeRequest) (any, error)

	// Now is the clock: every timestamp a row carries and every comparison with
	// the present is read from it. Nil means time.Now. A test holds it still; an
	// application leaves it nil.
	Now func() time.Time

	// Logger receives what the package cannot return to anybody: a Notifier
	// that failed after the change was stored. Nil means slog.Default().
	Logger *slog.Logger
}

// ChromeRequest is what Config.Chrome is asked about one screen.
type ChromeRequest struct {
	// Title is the screen's title, already in the screen's language, without
	// the application's name Config.TitleSeparator adds to the document title.
	Title string
	// Team says the screen is one of the team's, mounted under TeamPrefix.
	Team bool
	// Subject is who is asking, as RequireAuth and Config.Middleware left it.
	Subject security.Subject
}

// Paths are the segments the routes of this package are built from. Each one
// starts with "/" and holds no wildcard.
//
//	GET    Prefix                                 the customer's bookings
//	GET    Prefix + New                           the form that books, and the free times of a day
//	POST   Prefix                                 book
//	GET    Prefix + /{id}                         one booking
//	POST   Prefix + /{id} + Confirmation          confirm
//	GET    Prefix + /{id} + Rescheduling          the form that moves it
//	POST   Prefix + /{id} + Rescheduling          move it
//	POST   Prefix + /{id} + Cancellation          cancel
//
// and under TeamPrefix the list, one booking, Completion, Release, NoShow,
// Cancellation and Rescheduling, and under TeamPrefix + Settings the team's
// configuration: Services, Hours and Closures, each with a list, New, a
// creation, and a deletion by DELETE on /{id}. The settings sit under a
// segment of their own so that no booking identifier can be read as one.
type Paths struct {
	New, Confirmation, Rescheduling, Cancellation string
	Completion, Release, NoShow                   string
	Settings, Services, Hours, Closures           string
}

// DefaultPaths are the English segments.
var DefaultPaths = Paths{
	New: "/new", Confirmation: "/confirmation", Rescheduling: "/rescheduling", Cancellation: "/cancellation",
	Completion: "/completion", Release: "/release", NoShow: "/no-show",
	Settings: "/settings", Services: "/services", Hours: "/hours", Closures: "/closures",
}

// PortuguesePaths are the segments in Brazilian Portuguese, for an application
// whose addresses are written in it. Mounted at "/painel/agendamentos" and
// "/admin/agenda" they read /painel/agendamentos/novo,
// /painel/agendamentos/{id}/confirmacao and /admin/agenda/{id}/conclusao.
var PortuguesePaths = Paths{
	New: "/novo", Confirmation: "/confirmacao", Rescheduling: "/reagendamento", Cancellation: "/cancelamento",
	Completion: "/conclusao", Release: "/liberacao", NoShow: "/ausencia",
	Settings: "/configuracoes", Services: "/servicos", Hours: "/horarios", Closures: "/bloqueios",
}

// fields are the segments with their names, for validating and filling them.
func (p *Paths) fields() []struct {
	name  string
	value *string
} {
	return []struct {
		name  string
		value *string
	}{
		{"New", &p.New}, {"Confirmation", &p.Confirmation}, {"Rescheduling", &p.Rescheduling}, {"Cancellation", &p.Cancellation},
		{"Completion", &p.Completion}, {"Release", &p.Release}, {"NoShow", &p.NoShow},
		{"Settings", &p.Settings}, {"Services", &p.Services}, {"Hours", &p.Hours}, {"Closures", &p.Closures},
	}
}

// validate reports a segment that cannot be part of a route.
func (p Paths) validate() error {
	for _, f := range p.fields() {
		v := *f.value
		if v == "" {
			continue
		}
		if v[0] != '/' || strings.ContainsAny(v, "{} \t?#") || v == "/" || strings.HasSuffix(v, "/") {
			return fmt.Errorf("agenda: Config.Paths.%s is %q: a segment starts with /, ends without one and holds no wildcard", f.name, v)
		}
	}
	return nil
}

// withDefaults fills every empty segment from DefaultPaths.
func (p Paths) withDefaults() Paths {
	d := DefaultPaths
	defaults := d.fields()
	for i, f := range p.fields() {
		if *f.value == "" {
			*f.value = *defaults[i].value
		}
	}
	return p
}

// Validate reports what the configuration cannot be used with.
//
// It is called by New, so an application with a setting that cannot work fails
// where it is wired rather than on the first request that needed it.
func (c Config) Validate() error {
	if !security.ValidTenant(c.TeamTenant) {
		return fmt.Errorf("agenda: Config.TeamTenant is %q, which cannot be a tenant: pass the tenant the platform team signs in to (lowercase letters, digits, - and _, up to 64 characters)", c.TeamTenant)
	}
	if c.Zone == nil {
		return fmt.Errorf("agenda: Config.Zone is nil: the team's day is written in a timezone, and there is no default that would not be wrong for somebody (for Brazil, time.LoadLocation(%q))", DefaultLocation)
	}
	for _, prefix := range []struct{ name, value string }{{"Prefix", c.Prefix}, {"TeamPrefix", c.TeamPrefix}} {
		if prefix.value == "" {
			continue
		}
		if prefix.value[0] != '/' || (len(prefix.value) > 1 && strings.HasSuffix(prefix.value, "/")) {
			return fmt.Errorf("agenda: Config.%s is %q and has to start with / and end without one", prefix.name, prefix.value)
		}
		if prefix.value == reservedPrefix || strings.HasPrefix(prefix.value, reservedPrefix+"/") {
			return fmt.Errorf("agenda: Config.%s is %q, under %s/, which the framework keeps for itself", prefix.name, prefix.value, reservedPrefix)
		}
	}
	if err := c.Paths.validate(); err != nil {
		return err
	}
	for i, mw := range c.Middleware {
		if mw == nil {
			return fmt.Errorf("agenda: Config.Middleware[%d] is nil", i)
		}
	}
	for i, mw := range c.TeamMiddleware {
		if mw == nil {
			return fmt.Errorf("agenda: Config.TeamMiddleware[%d] is nil", i)
		}
	}
	if c.PageSize < 0 || c.PageSize > MaxPageSize {
		return fmt.Errorf("agenda: Config.PageSize is %d, and has to be between 0 and %d, where 0 means %d", c.PageSize, MaxPageSize, DefaultPageSize)
	}
	if c.Capacity < 0 || c.Capacity > MaxCapacity {
		return fmt.Errorf("agenda: Config.Capacity is %d, and has to be between 0 and %d, where 0 means %d", c.Capacity, MaxCapacity, DefaultCapacity)
	}
	if c.SlotStep < 0 || c.SlotStep%time.Minute != 0 {
		return fmt.Errorf("agenda: Config.SlotStep is %s, and has to be a whole number of minutes, where 0 means %s", c.SlotStep, DefaultSlotStep)
	}
	if c.Horizon < 0 || c.Horizon > MaxHorizon {
		return fmt.Errorf("agenda: Config.Horizon is %s, and has to be between 0 and %s, where 0 means %s", c.Horizon, MaxHorizon, DefaultHorizon)
	}
	if c.PaymentHold < 0 {
		return fmt.Errorf("agenda: Config.PaymentHold is %s: a hold cannot be negative, and 0 means %s", c.PaymentHold, DefaultPaymentHold)
	}
	if c.RemindBefore < 0 {
		return fmt.Errorf("agenda: Config.RemindBefore is %s: it cannot be negative, and 0 means %s", c.RemindBefore, DefaultRemindBefore)
	}
	return validateRoutes(c.withDefaults())
}

// validateRoutes asks the standard library to parse the exact patterns the
// module will register, every one of them. Its parser is not exported and
// reports an invalid pattern, or two that collide, by panic, so the throwaway
// mux turns that boot-time panic into the configuration error New promises.
func validateRoutes(c Config) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("agenda: Config.Prefix %q, Config.TeamPrefix %q and Config.Paths cannot be registered as routes: %v", c.Prefix, c.TeamPrefix, recovered)
		}
	}()

	handler := http.NotFoundHandler()
	mux := http.NewServeMux()
	for _, route := range routeTable(c) {
		mux.Handle(route.method+" "+route.path, handler)
	}
	return nil
}

// withDefaults returns the configuration with the optional fields filled in.
//
// It runs after Validate and never before: filling a default in first would
// hide the value somebody actually wrote from the check that would have refused
// it.
func (c Config) withDefaults() Config {
	if c.Prefix == "" {
		c.Prefix = DefaultPrefix
	}
	if c.TeamPrefix == "" {
		c.TeamPrefix = DefaultTeamPrefix
	}
	c.Paths = c.Paths.withDefaults()
	if c.PageSize == 0 {
		c.PageSize = DefaultPageSize
	}
	if c.Capacity == 0 {
		c.Capacity = DefaultCapacity
	}
	if c.SlotStep == 0 {
		c.SlotStep = DefaultSlotStep
	}
	if c.MinNotice == 0 {
		c.MinNotice = DefaultMinNotice
	}
	if c.MinNotice < 0 {
		c.MinNotice = 0
	}
	if c.Horizon == 0 {
		c.Horizon = DefaultHorizon
	}
	if c.PaymentHold == 0 {
		c.PaymentHold = DefaultPaymentHold
	}
	if c.RemindBefore == 0 {
		c.RemindBefore = DefaultRemindBefore
	}
	if c.ChangeUntil == 0 {
		c.ChangeUntil = DefaultChangeUntil
	}
	if c.ChangeUntil < 0 {
		c.ChangeUntil = 0
	}
	if c.Locale == "" {
		c.Locale = DefaultLocale
	}
	if c.Zone == nil {
		c.Zone = time.UTC
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// route is one line of the table this module registers.
type route struct {
	name   string
	method string
	path   string
	team   bool
}

// routeTable is the whole table, built from the prefixes and the segments. It
// is read by validateRoutes, which registers it on a throwaway mux, and by the
// router.
func routeTable(c Config) []route {
	p, t, s := c.Prefix, c.TeamPrefix, c.Paths
	return []route{
		{"agenda.index", http.MethodGet, p, false},
		{"agenda.create", http.MethodGet, p + s.New, false},
		{"agenda.store", http.MethodPost, p, false},
		{"agenda.show", http.MethodGet, p + "/{id}", false},
		{"agenda.confirm", http.MethodPost, p + "/{id}" + s.Confirmation, false},
		{"agenda.reschedule", http.MethodGet, p + "/{id}" + s.Rescheduling, false},
		{"agenda.rescheduled", http.MethodPost, p + "/{id}" + s.Rescheduling, false},
		{"agenda.cancel", http.MethodPost, p + "/{id}" + s.Cancellation, false},

		{"agenda.team.index", http.MethodGet, t, true},
		{"agenda.team.show", http.MethodGet, t + "/{id}", true},
		{"agenda.team.complete", http.MethodPost, t + "/{id}" + s.Completion, true},
		{"agenda.team.release", http.MethodPost, t + "/{id}" + s.Release, true},
		{"agenda.team.no_show", http.MethodPost, t + "/{id}" + s.NoShow, true},
		{"agenda.team.cancel", http.MethodPost, t + "/{id}" + s.Cancellation, true},
		{"agenda.team.reschedule", http.MethodPost, t + "/{id}" + s.Rescheduling, true},

		{"agenda.team.services", http.MethodGet, t + s.Settings + s.Services, true},
		{"agenda.team.services.create", http.MethodGet, t + s.Settings + s.Services + s.New, true},
		{"agenda.team.services.store", http.MethodPost, t + s.Settings + s.Services, true},
		{"agenda.team.services.edit", http.MethodGet, t + s.Settings + s.Services + "/{id}", true},
		{"agenda.team.services.update", http.MethodPost, t + s.Settings + s.Services + "/{id}", true},
		{"agenda.team.services.delete", http.MethodDelete, t + s.Settings + s.Services + "/{id}", true},

		{"agenda.team.hours", http.MethodGet, t + s.Settings + s.Hours, true},
		{"agenda.team.hours.create", http.MethodGet, t + s.Settings + s.Hours + s.New, true},
		{"agenda.team.hours.store", http.MethodPost, t + s.Settings + s.Hours, true},
		{"agenda.team.hours.delete", http.MethodDelete, t + s.Settings + s.Hours + "/{id}", true},

		{"agenda.team.closures", http.MethodGet, t + s.Settings + s.Closures, true},
		{"agenda.team.closures.create", http.MethodGet, t + s.Settings + s.Closures + s.New, true},
		{"agenda.team.closures.store", http.MethodPost, t + s.Settings + s.Closures, true},
		{"agenda.team.closures.delete", http.MethodDelete, t + s.Settings + s.Closures + "/{id}", true},
	}
}
