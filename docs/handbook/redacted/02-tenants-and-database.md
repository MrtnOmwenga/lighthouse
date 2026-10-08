---
title: "Tenants and the database"
project: redacted
topics: [postgresql, row-level-security, multi-tenancy, transactions, async-local-storage, composite-foreign-keys, least-privilege, kysely, migrations]
sources:
  - src/database/migrations.ts
  - src/database/tenant.ts
  - src/database/migrate.ts
  - src/auth/authentication.ts
  - src/common/lookups.ts
  - test/tenancy.e2e-spec.ts
verified: 2026-10-08
---

# Tenants and the database

Every organization's data sits in the same PostgreSQL tables. The permission model
([Part 1](01-permissions.md)) says no to another organization first; this page is about the second
lock, which works even if that code is wrong: the database itself.

## The words used on this page

- **Tenant:** one customer organization.
- **Row-level security (RLS):** a PostgreSQL feature where the database filters which rows a
  query may see or change, by a rule attached to the table.
- **Transaction:** a group of statements that take effect together or not at all.
- **Connection pool:** a small set of open database connections that requests take turns using.
- **Foreign key:** a rule that a value in one table must exist in another.
- **Interceptor:** in NestJS, code that wraps every request handler.

## How are organizations kept apart?

The same way Lighthouse keeps its tenants apart (see
[its page](../03-data-and-tenants.md)), with a different setting name:

- every tenant table has a policy: a row is visible, and writable, only when its `org_id` equals
  the setting `app.org_id`;
- the setting is made **inside a transaction**, so it disappears when the transaction ends and can
  never leak to the next request that borrows the same connection;
- with no setting, a query sees **no rows** (it fails closed);
- the API connects as a role that owns nothing and can't change the policies; `FORCE ROW LEVEL
  SECURITY` applies them even to the tables' owner.

So a query that forgets its `where org_id = …` still can't read another organization's data.

## What is different here: one transaction per request

In Lighthouse, each piece of code opens a tenant transaction when it needs one. Here, **the whole
request runs inside one**, set up before the handler starts (`TenantInterceptor` in
`src/auth/authentication.ts`):

1. The request's credential (Part 3) names an organization.
2. The interceptor opens a transaction and sets `app.org_id` to it.
3. **Inside that transaction, it loads the principal:** the member's current role, department and
   clearance, or the API key's current scopes. A disabled member or a revoked key isn't found, and
   the request ends with 401.
4. It runs the handler.
5. If the handler returns, everything commits. If it throws, **everything rolls back**, audit
   events included.

Two things follow from this design:

- **Changes take effect on the very next request.** A token says who you are; your role is read
  from the database every time. Demote or disable someone and their next request sees it, with no
  waiting for a token to expire. The cost is one lookup by primary key per request, inside a
  transaction the request needed anyway.
- **A request is all or nothing.** A document can't be created without its audit event, or the
  other way round.

## Why does Lighthouse do this differently, and which is right?

Each fits its own workload.

| | Redacted: one transaction per request | Lighthouse: a transaction where one is needed |
|---|---|---|
| What a request is | A short read or write on behalf of a signed-in member | Often something slow: a probe of another site can take 30 seconds |
| What it gains | The role is fresh on every request; a write and its audit event commit together; no handler can forget to scope its queries | No database connection is held while waiting on the network; anonymous pages need no tenant at all |
| What it costs | A connection is held for the whole request, and a failure rolls back everything, including the record that it was attempted | Each piece of code must open its own tenant transaction; a multi-step operation is only atomic if it is written inside one |
| Why that cost is acceptable | Requests are short, and every request is authenticated, so there is always a tenant | Most traffic is public pages and background checks, where holding a transaction would be waste or harm |

Holding a transaction open across a 30-second probe would pin a connection and keep a row locked
for no reason, so Lighthouse claims a monitor in one short transaction, probes with none, and
records in another. Redacted has no such waits in a request, and what it sells is correctness of
access and of the audit record, which one transaction gives for free.

The rule underneath both: **keep a transaction as short as the work that must be atomic, and
never hold one across something slow that you don't control.**

## How does deep code find the transaction?

A service three calls down needs the request's transaction and principal. Passing them through
every function would be noise, and a global variable would be shared between requests running at
the same time.

`TenantContext` (`src/database/tenant.ts`) uses Node's `AsyncLocalStorage`: a value attached to
one chain of asynchronous work. The interceptor stores `{ trx, principal }` for the request; any
code that runs as part of that request, however deep, reads them back:

```ts
const { db, principal } = this.tenant;
```

Code that runs outside a request gets an error, not someone else's transaction.

## What do the foreign keys add?

They include the organization, so the database refuses a row that links two organizations:

```sql
unique (org_id, id),                                           -- on users, departments…
foreign key (org_id, department_id) references departments (org_id, id)
```

One goes a step further. A document names its project *and* its department, and a three-column
key ties them together:

```sql
foreign key (org_id, project_id, department_id) references projects (org_id, id, department_id)
```

So **a document's department is always its project's department.** The permission model reads a
document's department to decide; this guarantees that value can't be made to disagree with where
the document actually lives.

Other rules the database enforces whatever the code does:

| Rule | Prevents |
|---|---|
| `check ((role in (department roles)) = (department_id is not null))` | A department role without a department, or an organization-wide role with one |
| `check ((author_id is null) <> (api_key_id is null))` | A document with no author, or with two |
| A unique index on `lower(email)` | Two accounts with the same email, in any organization |
| `clearance between 0 and 3` | A clearance level that doesn't exist |

## What may the application role do?

Only what it was granted, table by table:

| Tables | Granted | Notable absence |
|---|---|---|
| organizations, departments | read, insert | No update or delete |
| users, refresh tokens, API keys | read, insert, update | **No delete:** people are disabled and keys revoked, never erased |
| projects, documents, shares, sections | read, insert, update, delete | |
| audit events | read, insert | **No update or delete:** the log is append-only (Part 4) |

## Which questions are asked before the organization is known?

Three, each a `SECURITY DEFINER` function that returns the minimum:

| Function | Answers |
|---|---|
| `auth_login_lookup(email)` | Which member and organization does this email belong to? Returns two ids, never the password hash |
| `auth_refresh_lookup(hash)` | Which organization holds this refresh token? |
| `auth_api_key_lookup(prefix)` | Which organization holds this key, and what is its hash, so the secret can be checked? |

A fourth, `demo_cleanup`, deletes expired demo organizations: the one thing the application
couldn't otherwise do.

## How are queries written?

With Kysely, a typed query builder: the table and column names are checked by the TypeScript
compiler against the schema's types (`src/database/schema.ts`), and values are always sent as
parameters, never spliced into the SQL. It is not an ORM: there are no models with hidden
queries, and what is written is what runs.

## How is the isolation tested?

`test/tenancy.e2e-spec.ts`, connecting as the real application role:

- with no organization set, the role sees nothing at all;
- scoped to one organization, a query with no filter returns only that organization;
- writing into another organization's rows is refused by the database;
- the role can't change or erase the audit log;
- the lookup functions return only ids;
- over HTTP, another organization's ids are simply not found.

## Known gaps

- **Migrations can't be undone.** Each has an `up` and no `down`. Rolling one back means writing
  the reverse by hand, under pressure.
- **A request holds a database connection for its whole length.** One slow handler occupies one
  of a small pool; enough of them and other requests wait.
- **An email belongs to one organization.** The unique index is across all tenants, which keeps
  sign-in simple (an email alone finds the account) and means one person can't be a member of two
  organizations with the same address.
- **Members are never deleted by the application.** That protects the audit log's references and
  makes "erase my data" a job for the database owner.
- **An API key's every request is a write.** Loading a key updates its `last_used_at`, so
  read-only traffic from an integration still writes a row each time.
- **A refused request leaves no trace in the audit log,** if the refusal is thrown inside the
  handler: the rollback takes any audit event with it. (Part 4 looks at what is and isn't
  recorded.)
- **Isolation is by row, not by resource.** Organizations share tables, indexes and the pool; one
  very busy tenant could slow the others.

## Questions and answers

**Why set the organization inside a transaction?**
Connections are reused. A setting left on a connection would be inherited by whichever request
borrowed it next. A transaction-local setting is gone when the transaction ends.

**A developer writes a query and forgets the organization filter. What happens?**
It returns only the current organization's rows: the policy filters regardless.

**Why load the principal on every request, when the token could carry the role?**
So that disabling a member or changing their role takes effect on their next request. A role
inside a token stays true until the token expires.

**What does the three-column foreign key on documents guarantee?**
That a document's department is its project's department. Without it, a document could claim one
department while sitting in a project of another, and the permission check, which trusts that
column, could be steered.

**What is `AsyncLocalStorage` for?**
Giving every piece of code in one request the same transaction and principal without passing them
as arguments, and without a global that concurrent requests would share.

**Why can't the application delete a member?**
It was never granted `DELETE` on that table. Disabling keeps the history intact: audit events and
documents still point at a real row.

**What happens if the handler fails halfway?**
The transaction rolls back: nothing the request wrote remains, including its audit events.
