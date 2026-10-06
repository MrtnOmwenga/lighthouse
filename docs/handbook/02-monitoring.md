---
title: "Watching the apps: probes, the scheduler and incidents"
project: lighthouse
topics: [monitoring, scheduler, ssrf, state-machine, incidents, oidc, cloud-scheduler, postgres-as-queue]
sources:
  - internal/monitor/probe.go
  - internal/monitor/scheduler.go
  - internal/monitor/state.go
  - internal/store/monitors.go
  - internal/store/migrations/00001_initial.sql
  - internal/web/tick.go
  - internal/oidc/oidc.go
  - cmd/lighthouse/main.go
  - deploy/cloudrun/scheduler.tf
verified: 2026-10-06
---

# Watching the apps: probes, the scheduler and incidents

Lighthouse began as an incident tracker. This is the part that makes it a monitor: it checks each
project on a schedule, decides from the results whether the project is up or down, and opens and
closes incidents by itself.

## The words used on this page

- **Monitor:** one thing to watch: an address, how often to check it, and what counts as healthy.
- **Probe:** the act of checking: one HTTP request and the judgement of its answer.
- **Check:** the stored result of one probe (passed or failed, how long it took, why it failed).
- **Health:** what Lighthouse currently believes about a monitor: `unknown`, `up` or `down`.
- **Incident:** a record that something was down, from when to when, with a timeline of events.
- **Threshold:** how many results in a row it takes to change a belief.
- **Tick:** one round of "run whatever checks are due".

## What happens in one round?

Four steps, for every monitor that is due (`internal/monitor/scheduler.go`):

```
1. find    which monitors are due?                  (next_check_at has passed)
2. claim   take one, and push its next check into the future, in a single statement
3. probe   make the request, outside any database transaction
4. record  store the check, update the health, open or resolve an incident: one transaction
```

A bounded pool of workers (8 by default) runs steps 2 to 4 for several monitors at once.

## What does a probe check?

`Prober.HTTP` in `internal/monitor/probe.go` sends one GET request and passes it only if:

- a connection was made and an answer came back within the monitor's timeout;
- the status code is inside the expected range (for example 200 to 299);
- the body contains the expected text, if the monitor names one (at most 1 MiB is read).

It also records how long the request took and when the site's TLS certificate expires. Every probe
opens a fresh connection (`DisableKeepAlives`), so the time includes connecting and the TLS
handshake, the way a new visitor would experience it.

A failure is stored as a **category**, never as the raw error: `timeout`, `connection`, `status`,
`content`, `tls` or `blocked`. Raw errors can contain internal hostnames and addresses, and some of
these results are shown on a public page.

## What stops a monitor being pointed at something internal?

A monitor that makes real requests is a way to make *Lighthouse* send requests. Pointed at
addresses only Lighthouse can reach (the cloud's metadata service, other internal services), it
would leak their answers through the check results. That attack is called **server-side request
forgery (SSRF)**. There are two layers against it.

**First, who may create one.** Sandboxes can only create simulated monitors
(`normalize` in `internal/web/validate.go` rejects anything else, and rejects the
allow-private-network flag); only the signed-in owner can create a monitor that makes real
requests. A URL must also be plain `http` or `https`, with a hostname and no embedded credentials.

**Second, where a request may go,** whoever created the monitor. The guard (`guard` and `Blocked` in `probe.go`) refuses to connect to loopback, private,
link-local, multicast and carrier-grade-NAT addresses. Where it runs is the important part: as a
hook on the socket, **after** the hostname has been resolved, on the address actually being dialled.
So it also catches:

- a public hostname that resolves to a private address;
- a hostname that resolves to a public address first and a private one later (DNS rebinding);
- a redirect to an internal address (each hop makes a new connection, which is checked again; at
  most five redirects are followed).

This second layer matters even though only the owner can create real monitors: it covers a
compromised owner session, a mistake, and any future feature that lets others add addresses.

A blocked attempt is recorded as `blocked`. The owner can switch the guard off per monitor
(`AllowPrivate`), which the Kubernetes deployment uses to check services inside the cluster.

## What are simulated monitors?

A sandbox starts with three pretend sites (`SeedSandbox` in `internal/auth/auth.go`). Their probes
make no network request at all: `Prober.Simulated` invents a result from the monitor's mode (`up`,
`slow`, `flaky`, `down`). A visitor can break and fix a "site" and watch an incident open and
resolve, without Lighthouse contacting any real address on a stranger's behalf.

## Why is there no job queue?

"Which checks are due" is a column in the `monitors` table, `next_check_at`, and taking a job is one
statement (`ClaimMonitor` in `internal/store/monitors.go`):

```sql
UPDATE monitors SET next_check_at = now() + make_interval(secs => interval_seconds)
WHERE id = $1 AND NOT paused AND next_check_at <= now()
RETURNING …
```

The `WHERE` clause and the update happen atomically. If two workers (or two instances of
Lighthouse) try to claim the same monitor, the database lets one succeed; the other finds the
monitor no longer due and gets nothing. So each check runs once, with no separate queue, no lock
service and nothing extra to operate.

The trade-off is at-most-once: if the process dies between claiming and recording, that check is
skipped and the monitor is simply checked again at its next interval. For a monitor, a skipped
check is harmless; a duplicated or stuck one would be worse.

## Why is the probe made outside a transaction?

A probe can take up to 30 seconds. A database transaction holds a connection for as long as it is
open, and the pool has a limited number. Probing inside a transaction would cap the number of
simultaneous checks at the pool size and leave none for serving pages. So `Check` uses two short
transactions (claim; record) with the slow part between them.

## How does it decide that something is down?

One function decides, `Next` in `internal/monitor/state.go`. It takes the current state (health,
failures in a row, successes in a row), one new result and the thresholds, and returns the new
state and whether an incident should open or resolve:

| Health now | New result | What happens |
|---|---|---|
| unknown | pass | becomes `up` |
| up or unknown | fail, and failures in a row reach the failure threshold (default 3) | becomes `down`, **open an incident** |
| up or unknown | fail, below the threshold | nothing yet; the count goes up |
| down | pass, and successes in a row reach the recovery threshold (default 2) | becomes `up`, **resolve the incident** |
| down | pass, below the threshold | nothing yet |
| any | a pass resets the failure count; a fail resets the success count | |

Requiring several results in a row is called **hysteresis**: one blip doesn't open an incident, and
a site that flickers doesn't open and close one every minute.

`Next` is a *pure function*: it touches no database and no clock, so every combination can be
tested directly, and is.

## What happens when a check is recorded?

`record` in `scheduler.go`, inside one transaction:

1. Lock the monitor's row (`SELECT … FOR UPDATE`), so two results for the same monitor can't
   interleave.
2. Insert the check.
3. Call `Next`.
4. If it says **opened**: create an incident ("Storefront is down"), add a timeline event ("3
   checks in a row failed (status)"), and remember the incident on the monitor.
5. If it says **resolved**: close that incident and add an event ("Recovered: 2 checks in a row
   passed"), unless a person already resolved it by hand.
6. Save the monitor's new health and counts.

Because it is one transaction, the check, the health and the incident can never disagree.

Afterwards, outside the transaction, the scheduler tells the notifier, which emails the owner
(`internal/alert`) if alerts are configured. Sandboxes never send email.

## What makes a round happen?

Two modes (`SCHEDULE`, in `internal/config/config.go`):

- **`loop`** (the default; local runs and Kubernetes): Lighthouse runs its own clock, looking for
  due monitors every second.
- **`external`** (the live deployment): on Cloud Run an idle container gets no CPU, so a clock
  inside it would stall. Cloud Scheduler calls `POST /internal/tick` every 15 minutes, and each
  call runs one round (`externalTicker` in `cmd/lighthouse/main.go`), plus the hourly clean-up of
  old data when it's due.

## How does the tick endpoint know the caller is the scheduler?

The same idea as the keyless deploys in [the pipeline page](01a-pipeline.md), with the roles
reversed: here Google vouches for the caller and Lighthouse does the verifying
(`internal/oidc/oidc.go`).

Cloud Scheduler attaches an identity token signed by Google, saying "this call is made as the
service account `lighthouse-tick`, for the audience `https://martinomwenga.com/internal/tick`".
Lighthouse:

1. accepts only the RS256 algorithm (a token can't choose a weaker one, or "none");
2. fetches Google's public keys (cached for as long as Google says they may be) and verifies the
   signature; an unknown key triggers one refetch, since keys rotate;
3. checks the issuer is Google, the audience is its own, the token hasn't expired, and the email
   is exactly the scheduler's service account.

Anything else gets a 404, as if the endpoint didn't exist. There is no shared password to store or
leak. It is about 150 lines of standard library, with tests for a wrong key, a wrong audience,
another service account, an expired token, algorithm tricks and tampered claims.

## How does this compare with Uptime Kuma, Prometheus and Grafana?

- **Uptime Kuma** is the closest relative: a self-hosted uptime monitor with checks, status pages
  and notifications to dozens of channels. It does more kinds of check and far more kinds of
  alert. Lighthouse's monitoring core is a small version of the same idea, with things Kuma
  doesn't aim at: isolated tenants (the sandbox), a scale-to-zero deployment, and a portfolio
  built on the same data. For a company that just needs uptime monitoring, Kuma or a hosted
  service is the right choice; Lighthouse exists to show how such a thing is built.
- **Prometheus** answers a different question. Lighthouse looks at a service from outside ("does
  it answer?"), which is *black-box* monitoring. Prometheus collects numbers from inside services
  (request rates, error rates, latency distributions, memory), which is *white-box* monitoring: it
  stores them as time series, queries them with PromQL and evaluates alert rules.
- **Grafana** draws dashboards and manages alerts over data such as Prometheus's.

They complement Lighthouse rather than replace it: it has no view inside the apps, and nothing
watches Lighthouse itself. Both gaps are what metrics and an outside alerting system are for (see
the build list).

One constraint shapes any integration: Prometheus normally *pulls* metrics from a process that is
always running, and these services sleep. On Cloud Run the metrics have to be *pushed*, for
example at the end of each tick.

## Known gaps

- **The sandbox doesn't work on the live deployment.** Sandbox monitors are meant to be checked
  every 10 seconds, so a visitor who breaks a pretend site sees an incident within half a minute.
  With the external schedule, checks only run on a tick, every 15 minutes, and one tick runs each
  due monitor once. Verified on the live site on 2026-10-06: a sandbox's monitors were still
  `unknown`, never checked, 45 seconds after one was set to `down`. An incident would take three
  ticks. Fix: run a tenant's due checks when its console asks for data (the console polls every
  five seconds while open), so an active visitor drives their own schedule.
- **A new monitor isn't checked when it's created.** It is saved as due, but nothing runs it until
  the next round: within a second in loop mode, up to 15 minutes on the live site. A mistyped
  address is only discovered later. Fix: run the first check on save and show the result, and
  offer a test before saving.
- **Every other tick is missed.** The owner's monitors have a 900-second interval and the tick
  comes every 900 seconds. A claim sets the next check to "now + 900 s", a few seconds after the
  tick, so the following tick arrives a few seconds too early and skips it. The live monitors show
  about 58 checks in 24 hours instead of 96. Fix: make the interval shorter than the tick, or
  treat a monitor as due if it will be within the next few seconds.
- **Outages are noticed slowly.** Three failures in a row at one check per 15 to 30 minutes is 45
  to 90 minutes. The site's text says about 45.
- **A round handles at most 32 due monitors** (four times the worker count). In loop mode the next
  second picks up the rest; in external mode they wait for the next tick.
- **One vantage point.** Every probe leaves from one region. A network problem between there and a
  site is indistinguishable from the site being down; real monitors confirm from several places.
- **Nothing watches Lighthouse.** It checks itself, but if it is down it can't record that. Fix: a
  heartbeat to an outside service on each tick, which raises the alarm when the heartbeats stop.
- **Probes wake sleeping demos,** so the recorded response times (about 3.4 s) are cold starts.
- **At-most-once checks:** a crash between claim and record skips that check.

## Questions and answers

**Why is a failure stored as "timeout" and not the actual error?**
Raw errors can name internal hosts and addresses, and check results appear on a public status
page. A category says what went wrong without leaking where.

**Two instances of Lighthouse are running. Does each check run twice?**
No. Claiming is one atomic `UPDATE` that only succeeds if the monitor is still due; the second
instance finds nothing to claim.

**Why three failures before an incident?**
Networks blip. Opening an incident on one failed request would produce noise, and noise teaches
people to ignore alerts. The threshold is a setting per monitor.

**What does "pure function" buy you here?**
The rules for opening and resolving incidents can be tested exhaustively, with no database and no
waiting, because `Next` depends only on its inputs.

**A sandbox visitor tries to add a monitor for `http://localhost:5432`. What happens?**
It is rejected when it is created: sandboxes may only have simulated monitors. If the owner added
it, it would be saved, and every check would fail as `blocked`: the name resolves to a loopback
address, and the guard refuses the connection at the moment of dialling.

**What is SSRF, in one sentence?**
Tricking a server into making requests to places only the server can reach, and reading the
answers through it.

**Why check the address after DNS resolution rather than validating the URL?**
A URL's hostname says nothing about where it resolves, and the answer can change between the check
and the connection. Checking the address being dialled closes both gaps.

**Why does the tick answer 404 rather than 401 to a bad token?**
So the endpoint doesn't advertise that it exists. Unauthenticated callers can't tell it from any
other missing page.
