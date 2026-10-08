# Redacted (the RBAC-API repository)

A multi-tenant access-control API, and *Redacted*, the live demo built on it: a shared document
in which classified sections and words black out for readers without clearance, while people
type. NestJS, PostgreSQL, Yjs. About 3,100 lines of server code and 605 tests.

Source: [github.com/MrtnOmwenga/RBAC-API](https://github.com/MrtnOmwenga/RBAC-API). File paths on
these pages are in that repository.

| # | Page | What it covers |
|---|---|---|
| 1 | [The permission model](01-permissions.md) | Roles, actions and reach as one table; `can()`; what the table can't say; how the documentation and the tests are generated from it |
| 2 | [Tenants and the database](02-tenants-and-database.md) | Row-level security, one transaction per request, how deep code finds it, foreign keys that include the organization, what the application role may do |
| 3 | [Signing in: passwords, tokens and API keys](03-signing-in.md) | Argon2id, one answer for every failed sign-in, what the access token is for, refresh-token rotation and what it catches, API keys, lockout and rate limits |
| 4 | [The audit log](04-audit-log.md) | What is recorded, why it commits with the change, the hash chain and what it can and can't detect, keeping order under concurrency |
| 5 | [Sections, clearances and redaction](05-redaction.md) | Two independent checks, why a section is a separate document, word-level marks and server-made projections, what a redaction still reveals |
| 6 | [Live collaboration](06-live-collaboration.md) | How simultaneous edits merge (a CRDT), who may connect to what, re-checking open connections when permissions change, the race that was found and closed |
| 7 | The demo | *to be written* |
| 8 | Testing and the pipeline | *to be written* |

What the review finds that should be built or fixed is in the [build list](backlog.md).
