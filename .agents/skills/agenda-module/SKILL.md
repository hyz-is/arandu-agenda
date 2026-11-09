---
name: agenda-module
description: Change what this Arandu package registers — its routes, handlers, configuration, response shape, screens, sentences, availability rules or schema. Use when the request is to "add a route", "add a handler", "add a config option", "change the prefix", "add a field to the response", "add a column", "write a migration", "add a screen", "add a sentence", "add a rule to the entity", "change how the free times are computed", "regenerate the query", "return 404", or when a change touches module.go, config.go, handlers.go, model.go, schedule.go, migrations.go, screens.go or a *Query.go file. Covers foundation.Module and its optional interfaces, the route table, thin handlers, ctx.User, returning errors for the router to answer, the generated queries and aru model:build, where the rules of an entity live, the migration name that carries the order, what a response may carry, and the catalogue.
license: MIT
---

# The module contract

`foundation.Module` is `Name()` and `Routes()` and nothing else. That pair is the
whole public contract between a package and the framework. This package also
takes on `Migratable`, `Bootable` (it refuses to boot until its views are
linked) and `Publishable`; the compile-time proofs sit at the top of `module.go`
and a new contract adds a line there.

## Adding a route

**1. Add a line to `routeTable` in `config.go`**, with a name, a method and a
path built from `c.Prefix` or `c.TeamPrefix` and a `Paths` segment, never a
literal. The same table is registered on a throwaway mux by `Validate`, so a
collision fails in `New`: the team's settings sit under `Paths.Settings` so that
no booking identifier can be read as one. Add the segment to `Paths`,
`DefaultPaths`, `PortuguesePaths` and `fields()`.

**2. Add the handler** to `handlers()` in `handlers.go`, thin:

```go
func (m *Module) teamHourDelete(ctx *fhttp.Context) error {
	who, _ := ctx.User()
	if err := m.team.DeleteHour(ctx.Ctx(), who, ctx.Param("id")); err != nil {
		return err
	}
	return deleted(ctx)
}
```

`register` mounts `RequireAuth` before the first route, so a route added to the
table needs a session like every other. `ctx.User()` reads the subject the guard
put on the request; no handler loads the session again or takes an identity from
the input. Behind the guard there is always a subject, and the zero one is
refused by `security.Authorize` before any policy runs.

**3. Return the error; the router answers it.** `validation.Errors` goes back
with the messages in the flash (422 for JSON), `ErrNotFound` is 404 because it
wraps `model.ErrModelNotFound`, a policy refusal is 403. `ErrSlotTaken` reaches
a form as a message on the `start` field and a caller as the sentinel, because
`Labels.taken` wraps both.
`TestHandlersLeaveTheSubjectAndTheStatusToTheFramework` fails on a handler that
calls `errors.Is`, `fhttp.Refuse` or `http.Error`, that does not read
`ctx.User()`, or that constructs a query.

**4. Add the case to the tests.** `everyRoute` in `tests/Feature/routes_test.go`
is a table of every route; the no-session, wrong-side and registered-routes tests
read it.

## Changing a Model

Each entity embeds `model.Model`; its table is declared once beside it
(`bookingTable`, `serviceTable`, `hourTable`, `closureTable`, `counterTable`),
with `ManualKey: true` and `NoTimestamps: true` because the key is text the
service writes and the timestamps come from `Config.Now`. After changing a struct
or a table run

```sh
aru model:build
```

and commit what it rewrote; `aru model:build --check` exits 1 when a file is
missing, stale or edited by hand. Keep rows as pointers after `New`, `First` or
`Get`: a copy keeps the model of the row it was taken from and refuses to write.
Every query starts at `Bookings(s.db)`; the constructor returns one mutable
query.

Only `bookingTable` is scoped by tenant. The other four are `Global`, and that is
a decision to take in review: a table with no tenant column is read with any
Grant, so what is written to one is written only after `TeamPolicy` answered.

The bookings of every space are read across tenants in `internal/platform`,
which has its own generated query and is imported from `team.go`, `app.go` and
the one `schedule` helper of `service.go`. Keep it that way, and read there only
the columns a use case needs: the customer's availability reads
`OccupancyColumns` and nothing else.

The rules of an entity go in the custom block of `model.go` as pure methods (no
database, no clock read inside: the time arrives as a parameter). The service
orchestrates and holds none. Which times are free is `Schedule` in
`schedule.go`, also pure; test a change to it in `tests/Unit/schedule_test.go`,
in the zone of Sao Paulo.

## What may leave in a response

`customerFields`, `teamFields`, `serviceFields`, `hourFields` and
`closureFields` in `model.go` are the declared lists. The customer's drops what
the team wrote (its note, who released the booking and why, who did the work),
and `customerView` drops it again from the `Summary` the customer's use cases
answer with; the team's adds the tenant. A new column that should be seen is
added in the struct and in the right list; one that should not is added in the
struct only. `TenantID` is in no customer response.

## Adding a configuration field

A doc comment on the field; a rule in `Validate` if there is a value it cannot be
(a value out of range is refused, not clamped); a default in `withDefaults` as a
named constant, which runs after `Validate`; and a case in
`TestNewRefusesAWiringThatCannotWork`.

## Sentences and screens

Every sentence is a line of `resources/lang/<locale>/agenda.json`, in every
shipped locale with the same placeholders; `tests/Unit/translation_test.go`
fails on a missing, extra or unused line. A screen is a `.kyse.go` file under
`resources/views/modules/agenda/`, with its page data in `pages.go` and its
builder in `screens.go`; add it to the render probe in
`tests/Unit/published_views_compile_test.go`, which also fails on an inline
script, style or event attribute. A list is a `components.DataTable` with its
query on the server, never a bare table; a date is `Screen.DatePicker`, never a
bare date input.

## Adding a migration

Append it to `Migrations()` with a name that sorts after the last one:
`20261109_0001_create_agenda` is fixed once published, so a change is a new
migration. `TestTheModuleDeclaresItsSchema` requires sorted names and a `Down`
with the right signature. Use types that spell the same in SQLite, PostgreSQL
and MySQL, give timestamps no database default, and index to match the `ORDER BY`
of the listing, tenant first. A new column is nullable or has a default, and
removing one takes two releases. Migrations never run at boot.
