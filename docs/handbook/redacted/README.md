# Redacted (the RBAC-API repository)

A multi-tenant access-control API, and *Redacted*, the live demo built on it: a shared document
in which classified sections and words black out for readers without clearance, while people
type. NestJS, PostgreSQL, Yjs. About 3,100 lines of server code and 605 tests.

Source: [github.com/MrtnOmwenga/RBAC-API](https://github.com/MrtnOmwenga/RBAC-API). File paths on
these pages are in that repository.

| # | Page | What it covers |
|---|---|---|
| 1 | [The permission model](01-permissions.md) | Roles, actions and reach as one table; `can()`; what the table can't say; how the documentation and the tests are generated from it |
| 2 | Tenants and the database | *to be written* |
| 3 | Signing in: passwords, tokens and API keys | *to be written* |
| 4 | The audit log | *to be written* |
| 5 | Sections, clearances and redaction | *to be written* |
| 6 | Live collaboration | *to be written* |
| 7 | The demo | *to be written* |
| 8 | Testing and the pipeline | *to be written* |

What the review finds that should be built or fixed is in the [build list](backlog.md).
