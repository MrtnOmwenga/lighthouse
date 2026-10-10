---
title: "Data model and tenant isolation"
project: lighthouse
topics: [postgresql, row-level-security, multi-tenancy, least-privilege, constraints, migrations, connection-pooling]
sources:
  - internal/store/migrations/00001_initial.sql
  - internal/store/db.go
  - internal/store/monitors.go
  - internal/store/store_test.go
  - internal/web/server.go
verified: 2026-10-07
---

# Data model and tenant isolation

One PostgreSQL database holds the owner's real monitors and incidents, and the throwaway data of
every visitor who starts a sandbox. This page is about how those are kept apart, and how the
database itself refuses data that doesn't make sense.

## The words used on this page

- **Tenant:** one isolated customer of the system. Here: the single *owner* tenant (the real
  monitors) and one *sandbox* tenant per visitor.
- **Row-level security (RLS):** a PostgreSQL feature where the database itself filters which rows
  a query may see or change, according to a rule (a *policy*) attached to the table.
- **Role:** a database user or group, with permissions.
- **Transaction:** a group of statements that take effect together or not at all.
- **Constraint:** a rule the database enforces on every row (`CHECK`, `UNIQUE`, foreign keys).
- **Migration:** a versioned script that changes the database's structure.

## What tables are there?

```
tenants ──┬── monitors ──── checks              (one row per probe result)
          │       │
          ├── incidents ─── incident_events     (the timeline of each incident)
          │
          └── sessions                          (who is signed in, as which tenant)
```

Every table except `tenants` carries a `tenant_id` column saying whose row it is. Later migrations
add the analytics tables (see the analytics page), a record of sign-ins (`auth_events`, see the
auth page), and one table that belongs to no tenant: `rate_limits`, counters the application can
reach only through a function.

## What is the risk in sharing one database?

The usual way to separate tenants is to add `WHERE tenant_id = …` to every query. It works until
one query forgets it, and then one visitor sees another's data, or the owner's. The protection is
only as good as the most careless query, now and in every future change.

## How does row-level security remove that risk?

The rule moves from the queries into the database. Each tenant table has a policy
(`00001_initial.sql`):

```sql
CREATE POLICY tenant_isolation ON monitors
  USING      (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
```

- `USING` filters what can be read, updated or deleted: only rows whose `tenant_id` equals the
  current setting `app.tenant_id`.
- `WITH CHECK` applies the same rule to rows being written: a tenant can't insert a row for
  another.

The application sets `app.tenant_id` at the start of every transaction (`WithTenant` in
`internal/store/db.go`):

```go
tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID)
```

After that, `SELECT * FROM monitors` returns only that tenant's monitors. A query that forgets its
`WHERE` clause is still safe, because the database applies the filter regardless.

## What if the application forgets to set the tenant?

Then the setting is empty, `nullif` turns it into NULL, nothing equals NULL, and the query sees
**no rows at all**. The failure mode is "everything looks empty", never "everything is visible".
This is called failing closed, and a test asserts it (`TestNoTenantSeesNothing`).

## Why is the setting made "transaction-local"?

Connections are reused: the application keeps a small pool of open connections and lends one to
each transaction. If the tenant setting stayed on the connection, the next request to borrow it
could inherit the previous visitor's tenant. The third argument to `set_config`, `true`, makes the
setting disappear when the transaction ends, so nothing can leak from one use to the next.

## Can't the application just turn the policy off?

Not with the permissions it has.

- **The application connects as a role that owns nothing.** `lighthouse_api` is a member of the
  group `lighthouse_app`, which has been granted only the operations it needs. It can't drop a
  policy, alter a table or disable row security, because only a table's owner can.
- **`FORCE ROW LEVEL SECURITY`** makes the policies apply even to the tables' owner, who would
  otherwise be exempt. After that, only a role with PostgreSQL's special `BYPASSRLS` attribute
  ignores them. The migration role has it (it must, to run the cross-tenant functions below); the
  application role doesn't.
- **Migrations run as a different role**, the database owner, whose password only the migration
  job is given (see [the pipeline page](01a-pipeline.md)).

A test connects as the application role and tries: dropping a policy, switching row security off,
deleting checks, rewriting an incident's timeline. Every one must fail
(`TestAppRoleCannotBypassRLS`).

The grants also make two tables append-only for the application: it may insert and read `checks`
and `incident_events`, but not update or delete them. History can't be rewritten through the API,
even by a bug.

## Some questions cross tenants. How are those answered?

A few things must be asked before a tenant is known, or across all of them:

| Function | Answers |
|---|---|
| `lighthouse_due_monitors` | Which monitors are due, in any tenant? (the scheduler) |
| `lighthouse_session` | Which tenant does this session cookie belong to? |
| `lighthouse_owner_tenant` | Which tenant is the owner's? (created on first use) |
| `lighthouse_prune` | Delete old checks, expired sessions and expired sandboxes |
| `lighthouse_daily_salt`, `lighthouse_prune_analytics` | The day's secret for counting visitors; delete old visit records |
| `lighthouse_rate_hit`, `lighthouse_prune_security` | Count one more use against a limit; delete old sign-in records and counters |

Each is a `SECURITY DEFINER` function: it runs with its creator's permissions instead of the
caller's, like a clerk who may fetch one specific thing from a room the public can't enter. Each
returns only what its caller needs (the due-monitors function returns two ids per row, not the
monitors), has its search path pinned so it can't be tricked into calling a look-alike function,
and can be executed only by the application role. The scheduler takes the ids, then does the real
work inside each monitor's own tenant.

These eight functions are the only code that sees across tenants, which makes them the code to
review most carefully. A test checks they answer exactly what they should
(`TestCrossTenantFunctions`).

## Is there a second line of defence?

Yes: the foreign keys include the tenant.

```sql
UNIQUE (tenant_id, id),                                                -- on monitors
FOREIGN KEY (tenant_id, monitor_id) REFERENCES monitors (tenant_id, id)  -- on checks
```

A check can only point at a monitor *in the same tenant*; an incident's timeline can only belong
to an incident in the same tenant. Even if a policy were somehow bypassed, the database would
refuse a row that links two tenants' data together.

## What else does the database refuse?

Rules that make nonsense impossible to store, whatever the code does:

| Rule | Prevents |
|---|---|
| `CHECK (ok = (failure IS NULL))` | A passed check with a failure reason, or a failed one without |
| `CHECK ((status = 'resolved') = (resolved_at IS NOT NULL))` | A resolved incident with no resolution time, or the reverse |
| `CHECK ((kind = 'http') = (url IS NOT NULL))` | A real monitor without an address, a simulated one with |
| A unique index on tenants where `kind = 'owner'` | A second owner tenant |
| `UNIQUE (tenant_id, slug)` | Two monitors with the same slug in one tenant |
| Ranges on intervals, timeouts, thresholds, lengths | Out-of-range settings |

The same ranges are checked in the application first (`internal/web/validate.go`) so the user gets
a helpful message; the database is the guarantee.

Two choices about history: deleting a monitor deletes its checks but **keeps its incidents** (the
record of what happened survives); and whether an incident is public is **fixed when it's
created**, so deleting a private monitor can never make its old incidents appear on the public
status page.

## Why does "not found" also mean "not yours"?

With row-level security, a row in another tenant simply isn't visible, so asking for it by id gives
the same answer as asking for one that doesn't exist. The application reports both as "not found"
(`ErrNotFound`). That is deliberate: a visitor can't even learn that someone else's monitor exists.

## How is it known to work?

Tests run against a real PostgreSQL (started in a container), connecting as the real application
role (`internal/store/store_test.go`):

- two tenants can't read, change or delete each other's rows, or insert rows for each other;
- with no tenant set, every table looks empty;
- the application role can't remove or disable the policies, or rewrite history;
- the cross-tenant functions return exactly what they should.

Each was also checked the other way round: with the protection removed, the test fails.

## Known gaps

- **The check history grows and is scanned.** One table holds every result for 90 days, and the
  status figures are computed from the raw rows on each request. Fine at this size; the next steps
  would be daily summaries, then partitioning the table by time.
- **The eight cross-tenant functions are trusted code.** A mistake in one would cross tenants, and
  the migration role they run as has `BYPASSRLS`. They are small and tested; they must stay few.
- **Isolation is by row, not by resource.** Tenants share the same database, tables and
  connection pool, so one very busy tenant could slow the others. Sandboxes are bounded (ten
  monitors, two hours, a creation limit per visitor), not metered.
- **Everything depends on the right tenant id being set.** That comes from the session
  (see the auth page): the database isolates tenants perfectly and has no opinion on who is who.
- **Rolling a migration back has only been done in a test.** Every migration's "down" script is run
  by `TestMigrationsRunDownAndUpAgain` (all the way down, and up again); none has been needed on
  the live database.

## Questions and answers

**What is a tenant here?**
An isolated space of monitors, checks and incidents: one for the owner, and one per sandbox
visitor, created when they start and deleted two hours later.

**A developer adds a query and forgets the tenant filter. What happens?**
Nothing bad: the policy filters the rows anyway. In the usual design that forgotten filter would be
a data leak.

**Why not give each tenant its own database or schema?**
Sandboxes are created by anonymous visitors, in seconds, and thrown away in two hours. Creating a
database or schema per visitor is heavier, harder to migrate and slower to clean up. Rows with a
policy cost nothing to create and vanish with one `DELETE` (everything else cascades).

**Isn't answering "not found" for someone else's row security through obscurity?**
No. Security through obscurity means the *only* protection is that an attacker doesn't know
something (an address, how the system works). Here the protection is access control: knowing
another tenant's monitor id gets you nothing, because the policy makes the row unreachable. Not
revealing that the row exists is an extra privacy property on top: it stops someone confirming
guesses about what other tenants have.

**When would a database per tenant be the better design?**
When tenants are few, large and long-lived, need their own backups and restores, must be kept
apart for legal reasons, or could slow each other down. Here tenants are anonymous, created in
seconds and deleted in two hours, so rows with a policy fit better.

**What is the difference between `USING` and `WITH CHECK`?**
`USING` decides which existing rows a statement can see and touch; `WITH CHECK` decides which new
or changed rows it may write. Without `WITH CHECK`, a tenant could insert rows labelled as someone
else's.

**Why is `FORCE ROW LEVEL SECURITY` needed?**
By default a table's owner is exempt from its policies. `FORCE` removes that exemption, leaving
only roles with the `BYPASSRLS` attribute (here, the migration role) able to see across tenants.

**What does `SECURITY DEFINER` mean, and why is it dangerous?**
The function runs with its creator's permissions, not the caller's. That is how a narrow question
can be answered across tenants, and why each such function must do exactly one small thing.

**Why are checks append-only for the application?**
The application role was never granted `UPDATE` or `DELETE` on that table. A monitor's history
can't be altered through the API; only the clean-up function removes rows, by age.
