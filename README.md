<p align="center">
  <img src=".github/logo.svg" alt="Arandu" width="180">
</p>

# Arandu Agenda

Bookings on the agenda of a platform team, for a multi-tenant
[Arandu](https://github.com/arandu-io/framework) application: a customer of a
space picks a service, a day and a free time, pays in advance when the service
asks for it, is reminded near the time, and the team marks the visit as done.

There are two sides, and they are separate. The customer lists their own
bookings, books a time, confirms, reschedules and cancels. The team lists every
space's bookings, releases one without a payment as a courtesy, completes it,
marks an absence, moves and cancels, and keeps its own services, weekly hours
and closures. Each side has its own controllers, its own screens and its own
prefix, and the application mounts each where its own session is valid: the
team's session only on the operator console, the customer's only on the panel.

Every booking has a protocol number, `AG-000123`, taken from one sequence for
all the spaces.

## What it does not do

- **It takes no payment.** A balance, a charge and a Pix belong to the
  application and to [arandu-wallet](https://github.com/hyz-is/arandu-wallet).
  The module only knows a booking *waits for payment* until the application says
  the payment settled (`Settle`), or the team releases it as a courtesy
  (`Release`). The module has no price column either: what a service costs is
  the application's catalogue.
- **It sends no message.** A reminder, a confirmation and a cancellation are an
  `Event` handed to the application's `Notifier`, which sends it by WhatsApp,
  e-mail or whatever it has.
- It knows no table of users, no list of the team and no roles. Permissions are
  [arandu-permission](https://github.com/hyz-is/arandu-permission)'s, and the
  record of who did what is
  [arandu-activitylog](https://github.com/hyz-is/arandu-activitylog)'s.
- It runs nothing by itself. `Remind` and `ExpireHolds` are two calls the
  application schedules.

## Install

```bash
go get github.com/hyz-is/arandu-agenda
```

It requires [arandu-activitylog](https://github.com/hyz-is/arandu-activitylog)
and expects the application to administer permissions with
arandu-permission.

## Wire it

An Arandu application registers a module explicitly. There is no service
provider, no container and no discovery, so these are the lines to paste into
`bootstrap/app.go` and there are no others.

The import, with the other module imports:

```go
import (
	agenda "github.com/hyz-is/arandu-agenda"
)
```

The construction, in `Build`, after the session store and the activity log
exist and before `k.Register`:

```go
	zone, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return App{}, err
	}
	agendaModule, err := agenda.New(agenda.Config{
		// The tenant the platform team signs in to: the operator console's
		// realm. Required, with the zone the team's day is written in.
		TeamTenant: cfg.Auth.Tenant,
		Zone:       zone,

		// Two sides, two prefixes. The application's own addresses, in the
		// segments of its language.
		Prefix:     "/painel/agendamentos",
		TeamPrefix: "/admin/agenda",
		Paths:      agenda.PortuguesePaths,

		// The rules of the agenda. All optional; these are the defaults.
		Capacity:     1,               // bookings one time takes
		SlotStep:     20 * time.Minute, // distance between two starts offered
		MinNotice:    24 * time.Hour,   // earliest bookable start from now
		Horizon:      30 * 24 * time.Hour,
		PaymentHold:  24 * time.Hour,   // an unpaid booking keeps its slot this long
		RemindBefore: 3 * time.Hour,
		ChangeUntil:  2 * time.Hour,    // the customer may move or cancel until here

		Notifier:   agendaNotifier{whatsapp: whatsapp}, // the application's own channel
		Directory:  teamDirectory{users: users},        // names (optional)
		Subjects:   cameraSubjects{cameras: cameras},   // checks the camera is theirs (optional)
		PaymentURL: func(b agenda.Summary) string { return "/painel/pagamentos/agenda/" + b.ID },

		Activity: activityModule.Logger(), // arandu-activitylog

		// The actions arandu-permission fills are resolved on each request,
		// so its Roles() runs on every route; the console's guard runs on the
		// team's alone.
		Middleware:     []func(http.Handler) http.Handler{permissionModule.Roles()},
		TeamMiddleware: []func(http.Handler) http.Handler{consoleGuard},
	}, db, sessions)
	if err != nil {
		return App{}, err
	}
```

And the registration, inside the `k.Register(...)` call already there:

```go
		agendaModule,
```

An application that keeps the two sides on different routers mounts them
itself, each with its own guards, instead of registering the routes through
`Routes`:

```go
agendaModule.CustomerRoutes(panelRouter) // under Prefix
agendaModule.TeamRoutes(consoleRouter)   // under TeamPrefix
```

The package owns five tables, so run its migrations. They never run at boot:

```bash
aru migrate
```

`New` returns an error rather than starting half-wired: a team tenant that is
not one, no zone, a relative prefix, a prefix under `/_arandu`, a segment with a
wildcard, a step that is not whole minutes, a nil database or a nil session
store.

## Give people access

The policies are in `policy.go`, and the actions reach them through the
subject, which arandu-permission fills. **A role opens nothing**: a subject that
holds no action is refused everything, an administrator included.

Declare the actions in the permission catalogue beside the application's own.
The customer's and the team's are two lists, so a catalogue an organization
grants from never holds a team action:

```go
permission.Config{
	Actions: append(append(app.Actions(), permission.Actions()...), agenda.Actions()...),
	// ...
}
// and, in the catalogue of the platform's own team only:
//   agenda.TeamActions()
```

| action | side | what it opens |
| --- | --- | --- |
| `agenda.book` | customer | looking at the services and the free times, and booking one in the subject's own space |
| `agenda.view_own` | customer | reading the bookings the subject made |
| `agenda.view_space` | customer | reading every booking of the space, whoever made it; with `agenda.change`, changing them too |
| `agenda.change` | customer | confirming, rescheduling and cancelling a booking the subject can read |
| `agenda.view_all` | team | reading every space's bookings, and the team's services, hours and closures |
| `agenda.manage` | team | releasing a booking as a courtesy, completing it, marking an absence, moving and cancelling it |
| `agenda.configure` | team | creating, changing and deleting services, weekly hours and closures |
| `agenda.settle` | none | the application putting a payment on a booking from its own code; no person holds it |
| `agenda.run_tasks` | none | the scheduler running `Remind` and `ExpireHolds`; no person holds it |

Each action opens what it names and nothing else, and the Grants for reading
(`agenda.view_own`, `agenda.view_space`, `agenda.view_all`) only read, so the
database refuses a write made with one.

A member of the team is a subject of `Config.TeamTenant` who holds the team's
actions. `TeamPolicy` refuses a subject of any other tenant whatever it carries,
and a subject somebody else is viewing as.

## What the application provides

### Notifier

```go
type Notifier interface {
	Notify(ctx context.Context, e agenda.Event) error
}
```

`Notify` runs after the change is stored, and its error does not undo the
change: the booking is already made and the slot already taken. The failure is
logged with the kind of the event, the booking's identifier and the type of the
error, and no personal text. An application that must not lose a notification
enqueues it here and delivers it from its queue.

| `Event.Kind` | when | who is told |
| --- | --- | --- |
| `EventBooked` | a booking was made; `AwaitingPayment` says whether it waits for a payment | the team, and the customer |
| `EventScheduled` | the payment settled, or the team released it | the customer |
| `EventReminder` | `Remind` found it near; carries `ConfirmURL` and `RescheduleURL` | the customer |
| `EventConfirmed` | the customer confirmed | the team |
| `EventRescheduled` | moved; `By` says by whom | the side that did not act |
| `EventCancelled` | cancelled; `By` says by whom | the side that did not act |
| `EventCompleted` | the team did the work | the customer |
| `EventHoldExpired` | the hold of an unpaid booking lapsed | the customer |

`Event.ForTeam()` answers whether the team is the one to tell, and
`Kind.ForTeam()` does for the kinds that do not depend on who acted. The event
carries a `Summary` of the booking after the change: protocol, service, subject,
requester, start and end, status.

```go
type agendaNotifier struct{ whatsapp *whatsapp.Client }

func (n agendaNotifier) Notify(ctx context.Context, e agenda.Event) error {
	switch e.Kind {
	case agenda.EventReminder:
		phone, err := n.phoneOf(ctx, e.Booking.Tenant, e.Booking.RequesterID)
		if err != nil {
			return err
		}
		return n.whatsapp.Send(ctx, phone, fmt.Sprintf(
			"Lembrete: %s em %s. Confirme ou reagende: %s%s",
			e.Booking.ServiceName, e.Booking.StartsAt.In(zone).Format("02/01 15:04"), baseURL, e.ConfirmURL))
	}
	return nil
}
```

### Directory (optional)

Names the requester when a booking is made, and the team members who released or
completed one. Without one, the screens show the identifiers.

```go
type Directory interface {
	Person(ctx context.Context, tenant, id string) (agenda.Person, error)
}
```

### SubjectResolver (optional)

A booking is *about* something the application understands -- `"camera"`, its
identifier, a label -- and the module keeps it opaque. A `SubjectResolver` lets
the application check that the camera is the customer's and gives the label
itself, so the request cannot name another space's camera:

```go
type SubjectResolver interface {
	Resolve(ctx context.Context, tenant string, subject agenda.BookingSubject) (label string, err error)
}
```

`URLs().NewFor("reconfiguracao", agenda.BookingSubject{Type: "camera", ID: "42", Label: "Portaria"})`
is the address of the booking form with the booking already about that camera.

### PaymentURL (optional)

`Config.PaymentURL` answers the address where the customer pays a booking that
waits for it: the application's page, since the module knows nothing of money.
An empty answer shows the booking as waiting and offers no link.

## The payment

A service with `requires_payment` leaves a booking in `awaiting_payment`,
holding the slot until `HoldUntil` (`Config.PaymentHold` from now). The
application charges however it does, and when the payment is real it tells the
module, with a Grant it issues itself for that space:

```go
//arandu:system-grant the payment webhook was verified; the tenant is the booking's own
grant := security.SystemGrant(agenda.AgendaSettle, booking.Tenant)
booking, err := agendaModule.Settle(ctx, grant, bookingID, "pix-e2e-9f3a")
switch {
case errors.Is(err, agenda.ErrSlotTaken):
	// The hold lapsed and somebody booked the slot meanwhile: refund, or book again.
case errors.Is(err, agenda.ErrNotSettleable):
	// Cancelled, or already settled with another reference: refund.
}
```

`Settle` is idempotent: the same reference delivered twice answers the booking as
it is, and nothing is written or told again. A booking whose hold lapsed is
settled only if its slot is still free. `Find(ctx, grant, "AG-000123")` looks a
booking up by protocol or identifier in the Grant's space.

## Tasks

Two calls, to schedule every five minutes. Each returns how many it touched, and
neither sends anything itself:

```go
n, err := agendaModule.Remind(ctx)      // one Reminder per booking still to be confirmed that starts within RemindBefore
n, err := agendaModule.ExpireHolds(ctx) // marks the unpaid bookings past their hold as expired and emits HoldExpired
```

`Remind` reminds a `scheduled` booking once: the moment is written before the
Notifier is asked, by a statement that only one of two overlapping runs wins.
A confirmed booking is not reminded, and a booking moved to another time is
reminded again for the new one. A lapsed hold frees its slot as soon as the
clock passes it -- availability reads the clock, not the status -- so
`ExpireHolds` only keeps the statuses honest and tells the customer.

## The lifecycle

| status | meaning |
| --- | --- |
| `awaiting_payment` | holds the slot until `hold_until`, waiting for `Settle` or a courtesy |
| `scheduled` | holds the slot: paid, released, or a service that asks for no payment |
| `confirmed` | scheduled, and the customer confirmed |
| `completed` | the team did it |
| `cancelled` | cancelled by the customer or by the team |
| `expired` | the hold lapsed before a payment came |
| `no_show` | the customer did not turn up |

The customer moves or cancels a booking until `Config.ChangeUntil` before its
start; after that only the team does. A booking can be moved to a start that
`Slots` offers: inside the team's hours, outside its closures, with a place
free, no sooner than `MinNotice` and no later than `Horizon`.

Whether a time is free is a function of data, in `Schedule`, and an application
can ask the same question the screens do. Two people asking for the last place
take turns: the check is made again inside a transaction that holds the lock of
the protocol counter, and the second is told `ErrSlotTaken`.

## Publish the views

This package carries the markup of its own twelve pages and hands it over
instead of rendering it from the inside, because a page you cannot edit is a
page that says the wrong thing in your product.

Look at what would be written, then write it:

```bash
aru vendor:publish --tag=view
aru vendor:publish --tag=view --apply
```

Nothing is written without `--apply`. The files land under
`resources/views/modules/agenda/`, and from that point they are yours: the
customer's `index`, `form`, `show` and `reschedule`, and the team's `admin/index`,
`admin/show`, `admin/services`, `admin/service_form`, `admin/hours`,
`admin/hour_form`, `admin/closures` and `admin/closure_form`.

Two steps are left to you. Compile what was written:

```bash
aru view:build
```

and import the two directories it wrote into, with the other imports:

```go
	_ "your/module/path/storage/framework/views/modules/agenda"
	_ "your/module/path/storage/framework/views/modules/agenda/admin"
```

Without those imports the views are not in the binary, and the module refuses to
boot rather than answering the first request that reaches one of them with a
500. The pages extend your `layouts.app` and use the Kyse components: a
`DataTable` paged, searched, ordered and filtered on the server for every list,
`DatePicker` with the calendar in Portuguese for every date, `RadioGroup` for the
free times. They draw no inline style and no inline script, so a content
security policy of `'self'` holds. Every sentence is Brazilian Portuguese, in
`resources/lang/pt-BR/agenda.json`; override any line with the application's own
`Config.Translator`.

## Configuration

`TeamTenant` and `Zone` are the two required settings.

| field | meaning |
| --- | --- |
| `TeamTenant` | the tenant the team signs in to; its services, hours and closures are the agenda's |
| `Zone` | the timezone the team's day, the weekly hours and every screen are written in |
| `Prefix`, `TeamPrefix`, `Paths` | where the routes are mounted; `PortuguesePaths` is the whole set in Portuguese |
| `Capacity` | bookings one time takes; default 1 |
| `SlotStep` | distance between two starts offered; default 20 minutes, whole minutes |
| `MinNotice` | earliest bookable start from now; default 24 hours, negative for none |
| `Horizon` | latest bookable start from now; default 30 days |
| `PaymentHold` | how long an unpaid booking keeps its slot; default 24 hours |
| `RemindBefore` | how long before the start the reminder goes; default 3 hours |
| `ChangeUntil` | how long before the start the customer may still move or cancel; default 2 hours |
| `PageSize` | rows per page; default 25, refused above 100 |
| `Notifier`, `Directory`, `Subjects`, `PaymentURL` | what the application provides |
| `Activity` | arandu-activitylog's logger; every write is recorded under the Grant it was authorized with |
| `Middleware`, `TeamMiddleware` | guards that run after `RequireAuth`, on both sides and on the team's alone |
| `Translator`, `Locale`, `TitleSeparator`, `Chrome` | the sentences, the document title and the layout's chrome |
| `Now`, `Logger` | the clock and the log; a test holds the clock still |

## Routes

Every route sits behind `RequireAuth`, mounted by the package itself. A request
with no session is sent to `/auth/login`. Build addresses from `URLs()` and the
route names (`aru route:list`), never by writing a path a second time. Every GET
answers the screen to a browser and JSON to a client that asks for it.

| method | path (customer, under `Prefix`) | name |
| --- | --- | --- |
| `GET` | `/` | `agenda.index` |
| `GET` | `/new?service=&day=` | `agenda.create` |
| `POST` | `/` | `agenda.store` |
| `GET` | `/{id}` | `agenda.show` |
| `POST` | `/{id}/confirmation` | `agenda.confirm` |
| `GET`, `POST` | `/{id}/rescheduling` | `agenda.reschedule`, `agenda.rescheduled` |
| `POST` | `/{id}/cancellation` | `agenda.cancel` |

| method | path (team, under `TeamPrefix`) | name |
| --- | --- | --- |
| `GET` | `/?day=&status=&q=` | `agenda.team.index` |
| `GET` | `/{id}` | `agenda.team.show` |
| `POST` | `/{id}/completion`, `/release`, `/no-show`, `/cancellation`, `/rescheduling` | `agenda.team.complete`, `.release`, `.no_show`, `.cancel`, `.reschedule` |
| `GET`, `POST`, `DELETE` | `/settings/services`, `/new`, `/{id}` | `agenda.team.services*` |
| `GET`, `POST`, `DELETE` | `/settings/hours`, `/new`, `/{id}` | `agenda.team.hours*` |
| `GET`, `POST`, `DELETE` | `/settings/closures`, `/new`, `/{id}` | `agenda.team.closures*` |

The tenant is never a parameter of any of them: it is the Grant's.

## What is recorded

Every write is an entry of arandu-activitylog, in the log named `agenda`, under
the Grant the act was authorized with, so a space reads its own entries and the
platform administrator reads them all. The causer is the person who acted -- the
team member who released a booking, in the customer's space -- and a task has
none. The events are `agenda.booked`, `agenda.settled`, `agenda.released`,
`agenda.confirmed`, `agenda.rescheduled`, `agenda.cancelled`,
`agenda.completed`, `agenda.no_show`, `agenda.hold_expired`, `agenda.reminded`
and `agenda.service_created`, `_updated` and `_deleted`, `agenda.hour_created`
and `_deleted`, `agenda.closure_created` and `_deleted`. An entry carries the
protocol and identifiers, and never what a person typed: not the customer's
note, not a reason, not a name.

## The data path

`Booking` embeds `model.Model`, and each table is declared once, beside the
entity, in `model.go`. `aru model:build` generates `BookingQuery.go` and the
others from them: `Bookings(db)`, which starts a `*BookingQuery`, and
`BookingCollection`. The files are committed and never edited by hand;
regenerate them after changing an entity, and `aru model:build --check` fails
when one is out of date.

`BookingService`, `TeamService` and `AppService` own `*data.DB` and follow
`validate -> security.Authorize -> Grant -> Model terminal`; handlers never hold
the database or start a query. The tenant of every write is `data.Tenant(g)`,
from the Grant, never from the path, the body, the query string or a header.

The agenda is one for every space, so the schema is too:

- `agenda_bookings` has a `tenant_id`; a customer reads and writes it through
  the tenant's own model, and another space's booking is not found.
- `agenda_services`, `agenda_hours`, `agenda_closures` and `agenda_counters`
  belong to no customer and have no tenant column: the services, the week and the
  closures are the team's, written only after `TeamPolicy` answered, and read by
  a customer only to book.
- Whether a time is free depends on the bookings of every space, so the
  availability reads them across tenants through `internal/platform`, which no
  application can import -- for the columns that say *when* and *whether* a
  place is taken, and no name, no note, no space.

Two Grants are issued without a signed-in customer, both in `grants.go` and
nowhere else: the scheduler's, for the realm, and the team's, scoped to the one
tenant a booking is in, after `TeamPolicy` allowed the member (or the task Grant
was issued) and only for the action the realm's Grant was issued for. The
application's hooks take a Grant the application issued, which `appGrant`
checks.

## Layout

```
module.go        registration, routes, the contract with the framework
config.go        what the application passes in, and the route table
contracts.go     what the application implements: notifier, directory, subjects
model.go         the entities, their own rules, and what they may answer with
schedule.go      the team's availability as pure data: which times are free
policy.go        who may do what, for a customer and for the team
service.go       the customer's use cases, and Model access after authorization
team.go          the team's use cases and its settings
app.go           the application's hooks (Settle, Find) and the two tasks
grants.go        the Grants issued without a customer
activity.go      the activity log and the notifier
handlers.go      the controllers of both sides
pages.go         what the screens are handed
screens.go       building it from what a service answered
migrations.go    the schema
translation.go   the sentences, in resources/lang
views.go         the files the application takes ownership of
```

## Tests

```bash
export GOWORK=off
go test -race ./...
```

The denial suite constructs the services with a nil database, so even building
`Bookings(nil)` would panic. The structural twin reads every exported service
method and checks the same order on the allowed path, that a system Grant is
issued in `grants.go` alone, and that the table that reads every tenant is
reached only after a policy or the task Grant answered. The schedule is tested
as pure data, in the zone of Sao Paulo. The feature suite runs the whole thing
over a real SQLite database -- two people asking for the last place at once, the
hold that lapses, the payment that arrives twice, the tenant that is never the
request's -- and over the router with sessions. The views are published into a
scratch project, compiled with the view compiler an application runs, and drawn
once with sample data.

## Licence

MIT. See [LICENSE.md](LICENSE.md). Copyright HYZIS.
