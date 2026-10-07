# Migrations

Versioned SQL, applied in order by `lighthouse migrate` (goose). A release runs them as the
database owner **before** the new code takes traffic, while the previous release is still serving.

## The rule

**A migration must work with the release that is live when it runs**, as well as with the one
that follows it.

In practice:

- **Add first, remove later.** A new column, table, index or function argument (with a default)
  can go out with the code that uses it. Dropping or renaming something the live code still uses
  needs two releases: one that stops using it, then one that removes it.
- **No rewrites that lock a big table** for long: add the column, backfill in batches, then add
  the constraint.
- **Every migration has a `Down`** that undoes it, and the pair is exercised by
  `TestMigrationsRunDownAndUpAgain`: all the way down, and back up.

`00003_due_slack.sql` is the worked example: it replaces a function with one that takes an extra
argument, and gives the argument a default so the previous release's one-argument call still
resolves.

## If a release goes wrong

The deploy starts the new revision without traffic, tests it, moves traffic, tests through the
edge, and moves traffic back to the previous revision if that fails. The database is **not**
rolled back automatically: because of the rule above, the previous code works with the new
schema. Run a `Down` by hand only when the migration itself is the problem.
