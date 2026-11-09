# Security policy

## Supported versions

The latest minor release of Arandu Agenda receives security fixes. Older minor
releases do not: a published Go module version is immutable, so a fix is a new
release rather than a moved tag.

## Reporting a vulnerability

Report it privately, through GitHub's advisory form:

<https://github.com/hyz-is/arandu-agenda/security/advisories/new>

Do not open a public issue and do not describe the problem in a pull request.
A report that arrives in public is a report every reader of this repository can
act on before there is a release to upgrade to.

Include what you need to reproduce it: the version, the wiring, and the request
or call that triggers it.

Expect an acknowledgement within a few days. If the report is confirmed, the
fix, the release and the advisory are published together, and you are credited
unless you ask not to be.

## What is in scope

Anything that lets a caller reach data a policy did not authorize. In
particular:

- a path from a handler to a Model or database handle that does not pass through
  the Service and its Policy;
- a route reachable without `RequireAuth`, or a handler that takes who is
  asking from anywhere but the subject the guard put on the request;
- a Model terminal reached before `security.Authorize`;
- a query whose tenant scope is disabled, or a tenant taken from anywhere but
  `data.Tenant(g)`;
- a `Grant` that can be produced without a policy returning nil;
- a field reaching a response that the declared lists in `model.go` do not name,
  and above all the team's note, who released a booking and why, or the name,
  note or space of another customer's booking reaching a customer;
- a Grant built outside `grants.go`, or a team action reaching a subject outside
  `Config.TeamTenant`;
- the table that reads every tenant reached for more than the columns that say
  when a place is taken, or before a policy or the task Grant answered;
- a booking taken twice for the last place, or a payment settling a booking
  twice, or one for a slot that is not its own;
- text a person typed reaching a page unescaped, the activity log or a log line.

## What is not

- A vulnerability in the framework itself. Report that to the framework.
- A policy that is too permissive in an application that opened it. What this
  package ships denies everything; what an application opens is its own.
