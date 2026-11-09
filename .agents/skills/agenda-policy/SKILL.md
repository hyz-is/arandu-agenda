---
name: agenda-policy
description: Decide who may do what in this Arandu package, and change an action. Use when the request is to "add an action", "add a permission", "let the owner cancel bookings", "everything returns 403", "the tests say ErrForbidden", "add a Model-backed service method", "why does Booking authorize twice", "filter the list by requester", "let the team do X", "a system grant", "who may settle a payment", or when a change touches policy.go, service.go, team.go, app.go or grants.go. Covers the two policies, the Grant, authorization before the Model, the custom block that survives regeneration, why the tenant is never read from the request, the two Grants issued without a customer, the application's own Grant, and the shapes that look like authorization and are not.
license: MIT
---

# The policies

There are two, and they answer for different people.

- `BookingPolicy` is the customer's. It allows an action to a subject that was
  granted it (`s.Can(action)`, filled by arandu-permission), on a booking of the
  subject's own tenant, and for the actions about a stored booking only when the
  subject made it or holds `agenda.view_space`.
- `TeamPolicy` is the platform team's. It allows an action to a subject of
  `Config.TeamTenant` that was granted it, and refuses a guest and a subject
  somebody else is viewing as. It is the one policy that does not compare the
  record's tenant with the subject's: crossing tenants is what the team does.

Neither has a branch that allows an action by role. A subject that holds no
action, an administrator included, is refused everything; the default-deny test
walks every action with one. `agenda.settle` and `agenda.run_tasks` are answered
by neither: no person holds them.

## How a request actually gets through

1. The service validates the input. A rejected input never reaches a policy
   (the customer's side; the team's `reach` runs first because it also finds the
   booking).
2. The service calls `security.Authorize(ctx, s.policy, actor, ACTION, record)`.
3. `Can` returns nil, or the reason it will not.
4. Only then does a `security.Grant` exist, and the service reaches
   `Bookings(s.db)` with it.

The Model requires a Grant and applies its tenant, but it does not decide which
action the Grant represents, so `Authorize` before the first `Bookings(s.db)` is
mandatory even though the terminal also takes `g`.

## The procedure for an action

**1. Declare it** in `policy.go` with a doc comment, and put it in `Actions()`
or `TeamActions()`, never both. Say in `reads` whether it only looks: an action
not listed there issues a Grant that writes.

**2. Write the rule inside the custom block.** Nothing regenerates the file in
this repository, so the markers cost nothing and buy one thing: the same policy
regenerated elsewhere keeps what is between them. What is not written there
stays closed.

**3. Leave the tenant check above the block alone.** It runs before every rule:
a rule that allows an owner to read their own record would otherwise allow it
across customers. The test asserts the message contains `another tenant`.

**4. Add it to the tests.** `everyAction` in `tests/Unit/policy_test.go` is the
whole set the default-deny tests walk; a list holding six of seven passes while
the seventh is open. Add the rule's cases to the table beside it, and name the
action in the changelog (`TestEveryActionIsNamedInAReleasedChangelogEntry`).

## Reads are authorized like writes

`Booking` authorizes twice: the first call sees an empty booking, so every rule
about the record never ran; the second, on the row, is the one that keeps the
policy honest. A booking the second call refuses is answered `ErrNotFound`, as
one of another space is: telling a customer that a booking exists and is not
theirs tells them what exists. `Bookings` authorizes once and narrows the
statement by a predicate (`requester_id`), because a policy call per row would
still read the rows.

## The team crosses tenants in two steps

`TeamService.reach` asks `TeamPolicy` for a realm Grant, finds the booking's
tenant through `internal/platform` (the bookings table with no tenant filter),
and then acts with a Grant scoped to that one tenant. The scoped Grant is issued
by `scopedGrant` in `grants.go`, which checks the realm Grant was issued for the
same action. Nothing a request names chooses the tenant. The team's services,
hours and closures belong to no customer, and are read and written with the
realm Grant.

## The Grants issued without a customer

`grants.go` is the only file that builds a Grant without a Policy answering for
it, and `TestAGrantWithoutACustomerIsIssuedInGrantsGoAlone` holds that to two,
each marked `//arandu:system-grant` with the reason:

- `taskGrant`, for `Remind` and `ExpireHolds`, in the team's realm;
- `scopedGrant`, after `TeamPolicy` allowed the member or after `taskGrant`.

The application's own hooks, `Settle` and `Find`, take a Grant the application
issued for `agenda.settle`, after it decided that the payment is real. `appGrant`
checks it is for that action and names a tenant, and the booking is read in that
tenant alone: a Grant for another space finds nothing.

A third Grant built here is a decision to take in review, not a line to find in
a diff. Do not reach for `security.SystemGrant` anywhere else in this package.

## Shapes that look like authorization and are not

- **A tenant taken from the request.** The path, the body, the query string, a
  header: values the caller chose. `data.Tenant(g)` is the only source, and
  `TestNoTenantIsReadOutOfTheRequest` fails on the others. The team's `space`
  filter is a predicate for someone who already reads every tenant, never a
  scope; `BookInput` has no field that could carry one.
- **A check in the handler.** Handlers read the input and who is asking, ask the
  service, and return what it returned. `TestHandlersLeaveTheSubjectAndTheStatusToTheFramework`
  fails on one that holds the database or constructs a query.
- **A refusal that explains itself to the client.** The router answers with the
  status and its standard sentence; the reason is in the log.
- **A button hidden by `auth.Allows`.** It decides what to draw, never whether to
  do it; the route behind the button asks the policy again.
- **A free time the screen offered.** The screen lists the starts; the server
  checks again, in a transaction, that the start is one `Schedule.Offers` still
  allows.

## Run the gates

```sh
export GOWORK=off
gofmt -l $(find . -name '*.go' -not -path '*/testdata/*' -not -name '*.kyse.go') \
  && go build ./... && go vet ./... && go test -race -p 2 ./...
```
