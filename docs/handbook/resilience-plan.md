---
title: "Resilience: what it would cost, and what's worth building"
project: lighthouse (and the two demos)
topics: [backups, restore-testing, redundancy, shared-state, canary-releases, self-hosted-postgres, cost]
status: proposed 2026-10-06, nothing here is built yet
verified: 2026-10-06 (prices from the providers' pricing pages on this date)
---

# Resilience: what it would cost, and what's worth building

Today there is one instance per app, one region, free database plans, six hours of database
history and no tested restore. This page prices the ways to improve that and picks the ones worth
building for a portfolio: the ones that demonstrate a skill, not the ones that only cost money.

## What does each option cost?

| Option | What it demonstrates | Monthly cost | Notes |
|---|---|---|---|
| **Nightly backups to object storage** (Postgres dumps, a MongoDB dump) | Backups, retention | $0 | Cloud Storage is free up to 5 GB in `us-east1`, `us-central1`, `us-west1`; the dumps are megabytes. A second Cloud Scheduler job is free (3 per account). Cloud Run jobs have their own free tier |
| **An automated restore test** (weekly: restore the latest backup into a scratch database, run checks) | That the backups actually work; a known recovery time | $0 | The part most teams skip |
| **Traffic-split releases and automatic rollback** | Canary / blue-green deployment | $0 | Built into Cloud Run (revisions) |
| **Two instances with shared state** | Running more than one copy correctly | $0 while idle | With request-based billing a second instance costs nothing until it handles requests. Redacted shares state through PostgreSQL (LISTEN/NOTIFY) already; GhostChat needs Redis: Upstash's free tier is 256 MB and 500,000 commands a month |
| **Serve a saved copy when the origin fails** (edge) | Graceful degradation | $0 | In the Worker |
| **A second region with failover at the edge** | Multi-region | $0 while idle | The services scale to zero in both regions. The database stays in one region, so this protects against a regional Cloud Run outage only |
| **An always-warm instance** (no cold starts) | Nothing a reviewer can see | about $5–8 per service | Idle minimum instance: $0.0000025 per vCPU-second and per GiB-second; a 1-CPU, 256 MiB instance is about $8.20 a month before the free allowance |
| **A continuous standby database** fed by replication from Neon | Disaster recovery with a small data-loss window | not free | Possible on any Neon plan, but a connected replica keeps Neon's compute awake around the clock (about 180 compute-hours against the free 100) |
| **A managed highly-available database** (a paid Neon plan, Cloud SQL with a standby) | Little: it's a checkbox | paid, not priced here | Buys reliability, shows no skill |
| **Self-managed PostgreSQL** | Operating a database: backups with point-in-time recovery, replication, failover, upgrades | $0 on free VMs | See below |

## Should the databases be self-hosted?

Not the live ones. A self-managed database on one small free VM would be *less* reliable than Neon,
and the live site would depend on it.

But operating a database is exactly the skill worth showing, and it can be shown without putting
the live site at risk: run a **self-managed PostgreSQL pair as the recovery side**.

- Two always-free VMs (Oracle's AMD micro instances were available when checked on 2026-10-05; or
  Google's free `e2-micro`): a primary and a streaming replica.
- Continuous archiving with pgBackRest or WAL-G to object storage, giving point-in-time recovery.
- Fed every night from the live databases' dumps, so it is also the restore test's target.
- Drills, written up: restore to a point in time; promote the replica when the primary is stopped;
  measure how long each takes.

It stays out of the request path (Oracle's Stockholm region is about 100 ms from Cloud Run in
Virginia, far too slow per query), which is also what makes it safe to experiment on.

## What's worth building?

The $0 items that each prove something:

1. **Backups with a tested restore** and stated objectives: how much data could be lost (24 hours,
   the backup interval) and how long recovery takes (measured by the weekly test).
2. **Canary releases with automatic rollback.**
3. **Shared state across two instances,** with a test that proves an edit made through one instance
   reaches a reader connected to the other.
4. **The edge serving a saved copy** when the origin is down.
5. **The self-managed PostgreSQL pair** as a separate project on the Oracle account: replication,
   point-in-time recovery, failover drills.

Not worth it here: an always-warm instance and a managed highly-available database (they cost
money and demonstrate nothing), and a continuous standby (it breaks the free compute budget).
A second region is optional: cheap, but without a second database region it is partly for show.

**Total running cost of the recommended set: $0 a month.** The cost is build time.

## What this would let the portfolio say

- "Backups are restored and checked automatically every week; recovery takes N minutes."
- "Releases go to 5% of traffic first and roll back on their own if the health check fails."
- "The collaborative editor runs on two instances; permission changes reach both through
  PostgreSQL notifications, and a test proves it."
- "I run a PostgreSQL primary and replica with point-in-time recovery, and I've practised failing
  over and restoring."
