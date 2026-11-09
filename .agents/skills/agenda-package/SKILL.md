---
name: agenda-package
description: Install, wire and use the Arandu Agenda package (Go, Arandu) in an application. Use when the request is to "install Arandu Agenda", "add bookings to the app", "let customers book a time with the team", "go get github.com/hyz-is/arandu-agenda", "wire it into bootstrap/app.go", "register the module", "write the Notifier", "settle a booking when the payment arrives", "schedule the reminders", "everything under /agenda returns 403", "it redirects to /auth/login", "the table does not exist", "no such table", "move the bookings to /painel/agendamentos", or when a project's go.mod already requires github.com/hyz-is/arandu-agenda. Covers the wiring and where each line goes, the Config fields, the two sides and their prefixes, the actions to declare in arandu-permission, the Notifier, Directory and SubjectResolver the application writes, Settle and the two tasks to schedule, the migration step that is not optional, and the views to publish.
license: MIT
metadata:
  audience: app
---

# Using Arandu Agenda

Bookings on the agenda of a platform team, for a multi-tenant application: a
customer of a space picks a service, a day and a free time, pays in advance when
the service asks for it, is reminded, and the team marks the visit as done. Two
separate sides, each with its own routes and screens. It is registered by hand:
there is no service provider, no container and no discovery, so if a line is not
written in the application, it does not happen.

**It takes no payment and sends no message.** Money is the application's and
arandu-wallet's; a message is whatever the application's `Notifier` does with an
`Event`.

## Install

```bash
go get github.com/hyz-is/arandu-agenda
```

## The wiring, and where each line goes

All of it is in `bootstrap/app.go`. Nothing else in the application changes,
and `aru make:module` does not edit this file for you.

The import:

```go
import (
	agenda "github.com/hyz-is/arandu-agenda"
)
```

The construction, in `Build`, after the session store and the activity log exist
and before `k.Register`:

```go
	zone, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return App{}, err
	}
	agendaModule, err := agenda.New(agenda.Config{
		TeamTenant: cfg.Auth.Tenant, // the tenant the platform team signs in to
		Zone:       zone,            // the team's day is written in it
		Notifier:   agendaNotifier{whatsapp: whatsapp},
		Activity:   activityModule.Logger(),
	}, db, sessions)
	if err != nil {
		return App{}, err
	}
```

The registration, inside the `k.Register(...)` call already there:

```go
		agendaModule,
```

`New` refuses a configuration that cannot work, a nil database handle and a nil
session store, so a wiring mistake costs one restart. `TeamTenant` and `Zone`
are the two required settings; the customers' tenant is nothing the
configuration names, because it comes from the Grant of whoever is signed in.

## Then run the migrations

```bash
aru migrate
```

Not optional, and not automatic. The package owns five tables, which is why its
`arandu.mod.toml` declares `migrations = true`. `aru migrate` is a step in the
pipeline and never a call at the start of the process.

**Symptom to recognise:** `no such table`, or `relation does not exist`, from a
route that authorized correctly. The migration has not run.

## The two sides

`Prefix` (default `/agenda`) is the customer's, `TeamPrefix` (default
`/admin/agenda`) is the team's. `agenda.PortuguesePaths` writes the segments in
Portuguese. Every route sits behind `RequireAuth`, mounted by the package
itself: a request with no session is sent to `/auth/login`, and comes back to
the address it asked for after signing in.

An application whose two sides are two routers mounts them itself:
`agendaModule.CustomerRoutes(panel)` and `agendaModule.TeamRoutes(console)`.
`Config.Middleware` runs after `RequireAuth` on both sides and
`Config.TeamMiddleware` on the team's alone. When arandu-permission administers
the permissions, mount its `Roles()` in `Middleware`: it fills the subject's
actions on each request.

**Symptom to recognise:** every route redirects to `/auth/login`. Nobody is
signed in on that request: the cookie did not arrive, or the session store
passed to `New` is not the one the application signs people in with.

## Every route refuses everybody until the actions are granted

The policies deny every action and have no branch that opens one by role. Put
the actions in the permission catalogue and grant them:

```go
permission.Config{Actions: append(append(app.Actions(), permission.Actions()...), agenda.Actions()...)}
// and, in the platform team's catalogue only:
//   agenda.TeamActions()
```

`agenda.book`, `agenda.view_own`, `agenda.view_space` and `agenda.change` are the
customer's; `agenda.view_all`, `agenda.manage` and `agenda.configure` are the
team's, and `TeamPolicy` refuses them to any subject outside `Config.TeamTenant`.
`agenda.settle` and `agenda.run_tasks` are nobody's: the application's code and
the scheduler issue Grants for them.

**Symptom to recognise:** `403` on a signed-in administrator. They hold no
action. Grant one, do not look for a role check to add.

## What the application writes

- **`Notifier`**: `Notify(ctx, agenda.Event) error`, sending the reminder, the
  confirmation, the cancellation. Its failure is logged without personal text and
  never undoes the change. The `EventReminder` carries `ConfirmURL` and
  `RescheduleURL` as paths; put the host in front.
- **`Directory`** (optional): `Person(ctx, tenant, id)`, for names.
- **`SubjectResolver`** (optional): checks that the thing a booking is about --
  `"camera"`, an identifier -- is the tenant's, and gives its label.
- **`PaymentURL`** (optional): where the customer pays a booking that waits for
  it. `URLs().NewFor("reconfiguracao", agenda.BookingSubject{Type: "camera", ID: "42", Label: "Portaria"})`
  links to the form with the booking already about something.

## When the payment settles

```go
//arandu:system-grant the webhook was verified; the tenant is the booking's own
grant := security.SystemGrant(agenda.AgendaSettle, booking.Tenant)
_, err := agendaModule.Settle(ctx, grant, bookingID, paymentRef)
```

Idempotent for the same reference. `agenda.ErrSlotTaken` means the hold lapsed
and somebody took the slot: refund or book again. `agenda.ErrNotSettleable`
means cancelled, or already settled with another reference: refund.

## Schedule the two tasks every five minutes

```go
agendaModule.Remind(ctx)      // the reminders that are due, once each
agendaModule.ExpireHolds(ctx) // the unpaid bookings past their hold
```

Without `Remind` nobody is reminded; without `ExpireHolds` the statuses of the
lapsed holds stay `awaiting_payment` (their slots are free anyway) and the
customer is not told.

## Publish the views

```bash
aru vendor:publish --tag=view --apply
aru view:build
```

and import the two directories it wrote into
(`storage/framework/views/modules/agenda` and `.../admin`) in
`bootstrap/app.go`. Without them the module refuses to boot and names the
missing views, the command and the imports.
