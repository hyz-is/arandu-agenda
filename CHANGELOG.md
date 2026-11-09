# Changelog

Everything worth knowing about a release of Arandu Agenda is recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and
the versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

A published module version is immutable: Go serves it from the proxy forever, so
a release is corrected by another release and never by moving a tag.

## [Unreleased]

## [0.1.0] - 2026-11-09

### Added

- Bookings on the agenda of a platform team, for the customers of a multi-tenant
  application, as two separate sides: each has its own controllers, screens and
  prefix, and the application mounts each where its own session is valid
  (`Module.CustomerRoutes`, `Module.TeamRoutes`, or `Module.Routes` for both).
  The module takes no payment and sends no message: money is the application's
  and arandu-wallet's, and a message is whatever the application's `Notifier`
  does with an `Event`.
- `Booking`: a protocol (`AG-000123`, from one sequence for every space, held
  unique by an index), a service, what it is about (`BookingSubject`: a type, an
  identifier and a label the application understands), the requester, the start
  and the end, a status (`awaiting_payment`, `scheduled`, `confirmed`,
  `completed`, `cancelled`, `expired`, `no_show`), the hold of an unpaid booking,
  the application's payment reference, who released it as a courtesy and why,
  and the instants of its confirmation, reminder and completion. Its own rules
  are pure methods: `Occupies`, `Lapsed`, `Confirm`, `Settle`, `Release`, `Move`,
  `Cancel`, `Complete`, `MarkNoShow`, `Expire`.
- `Service`, `Hour` and `Closure`: the team's catalogue (a key, a name, how long
  it takes, whether it asks for a payment, whether it is offered -- and no
  price), the windows of its week in minutes since midnight, and the ranges it
  takes no booking in. They belong to no customer and have no tenant column.
- `Schedule`: which times are free, as pure data -- inside the weekly windows of
  `Config.Zone`, outside the closures, with a place free (`Config.Capacity`), no
  sooner than `Config.MinNotice` and no later than `Config.Horizon` from now --
  that an application can ask as the screens do.
- The customer's side: `BookingService` with `Services`, `Slots`, `Book`,
  `Bookings` (a data table paged, searched, ordered and filtered by status on
  the server), `Booking`, `Confirm`, `Reschedule` and `Cancel`. Two people asking
  for the last place take turns: the check is made again inside a transaction
  that holds the lock of the protocol counter, and the second is told
  `ErrSlotTaken`. A customer reads the bookings they made, or the whole space's
  with `agenda.view_space`, and moves or cancels until `Config.ChangeUntil`
  before the start; a booking of another space is not found.
- The team's side: `TeamService` with `Bookings` (every space's, filtered by
  status, day, space and search), `Booking`, `Release` (a courtesy, with a
  reason), `Complete`, `NoShow`, `Cancel`, `Reschedule`, and the create, change
  and delete of services, weekly hours and closures. `TeamPolicy` refuses a
  subject of any tenant but `Config.TeamTenant`, and a subject somebody else is
  viewing as.
- The application's hooks and tasks: `AppService` with `Settle` (idempotent for
  the same payment reference; `ErrSlotTaken` when a lapsed hold's slot was taken
  and `ErrNotSettleable` when the booking takes no payment) and `Find`, both on a
  Grant the application issues for `agenda.settle`; `Remind`, one `EventReminder`
  per booking still to be confirmed that starts within `Config.RemindBefore`,
  once; and `ExpireHolds`, which marks the unpaid bookings past their hold as
  expired. `Module` delegates all four.
- The `Notifier` (`Notify(ctx, Event)`; the kinds `EventBooked`,
  `EventScheduled`, `EventReminder`, `EventConfirmed`, `EventRescheduled`,
  `EventCancelled`, `EventCompleted` and `EventHoldExpired`, with `ForTeam`),
  the optional `Directory` and the optional `SubjectResolver`, which the
  application implements, and `Config.PaymentURL`.
- Nine actions: `AgendaBook` (`agenda.book`), `AgendaViewOwn`
  (`agenda.view_own`), `AgendaViewSpace` (`agenda.view_space`) and `AgendaChange`
  (`agenda.change`) for the customer, listed by `Actions`; `AgendaViewAll`
  (`agenda.view_all`), `AgendaManage` (`agenda.manage`) and `AgendaConfigure`
  (`agenda.configure`) for the team, listed by `TeamActions`; and `AgendaSettle`
  (`agenda.settle`) and `AgendaRunTasks` (`agenda.run_tasks`), which no person
  holds. A role opens nothing.
- Every write is an entry of arandu-activitylog in the log named `agenda`, under
  the Grant it was authorized with and with the person who acted as the causer;
  an entry carries the protocol and the identifiers and never what a person
  typed.
- Twelve screens as Kyse views, published with `aru vendor:publish`, in Brazilian
  Portuguese and English (`resources/lang`): the customer's list, booking form
  (with the Kyse date picker), booking page and rescheduling form, and the team's
  list, booking page and the lists and forms of services, hours and closures.
  Every list is a data table with its query on the server; no view carries an
  inline script or style.
- One migration, `20261109_0001_create_agenda`, creating `agenda_services`,
  `agenda_hours`, `agenda_closures`, `agenda_bookings` and `agenda_counters`.
  Run it with `aru migrate`; it never runs at boot.
