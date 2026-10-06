# Build list from the review

Things the part-by-part review found that should be built or fixed. Nothing here is built during
the review; it is all done together at the end. Each item says what, why, and where it came from.
Sizes: S (under an hour), M (a few hours), L (a day or more).

## Do first: broken on the live site

| # | What | Why | Size |
|---|---|---|---|
| 27 | **Make the sandbox work under the external schedule:** when a tenant's console asks for data, run that tenant's due checks first (the console polls every five seconds while open), so an active visitor drives their own schedule. | Sandbox monitors are meant to be checked every 10 seconds; on Cloud Run checks only run on the 15-minute tick. Verified live on 2026-10-06: nothing was checked 45 seconds after a pretend site was set to down. Lighthouse's own demo doesn't demonstrate anything. | M |
| 28 | **Stop every other tick being skipped:** treat a monitor as due if it will be within a few seconds, or set the owner's monitor interval below the tick period. | Interval and tick are both 900 s; each claim lands a few seconds after the tick, so the next tick arrives too early. The live monitors log about 58 checks a day instead of 96. | S |
| 29 | **Correct the site's detection time** ("about 45 minutes") once 28 is fixed, or state the real figure. | With every other tick skipped it is 45 to 90 minutes. | S |

## From Part 1a: the pipeline

| # | What | Why | Size |
|---|---|---|---|
| 1 | **Branch rules on the three release branches:** require a pull request and passing CI checks; block direct and force pushes. No required approval (a solo maintainer can't approve their own PR). | Today a direct push to a release branch deploys untested code. | S |
| 2 | **Gate the release on CI for the same commit:** the release workflow starts only after CI has passed on that exact commit. | A PR's checks run on the branch, which can differ from the merged result; and a rule can be bypassed, a dependency between workflows can't. | S |
| 3 | **Verify the signature before deploying:** `cosign verify` (this repository's release workflow as the expected identity) before the image is copied to Artifact Registry. | The image is signed, but nothing checks the signature, so it currently protects nothing. | S |
| 4 | **Pin third-party actions to commit hashes**, with Dependabot updating the pins. | Actions are referenced by movable tags (`@v3`); a compromised action would run inside the job that can deploy. | S |
| 5 | **Show the migration job's output in the workflow log when it fails.** | Today it has to be looked up in Cloud Logging. | S |
| 6 | **Roll back automatically when the smoke test fails:** send traffic back to the previous revision. | A failed smoke test only turns the job red; the broken revision keeps serving. | M |
| 7 | **Write down the migration rule** (a migration must work with the previous release's code: add first, remove in a later release), and consider a CI check. | Migrations run before the new code takes traffic. | S |
| 8 | **Cache the public pages at the edge for about a minute**, serving the cached copy if the origin fails. | Most visitors would never wake the app, and the site would survive a brief origin failure. Needs care: pages that depend on the signed-in owner must not be cached. | M |
| 9 | **Decide whether to keep building the arm64 image.** | Cloud Run only runs amd64; arm64 serves the Kubernetes path and Arm laptops. Keep (cheap, cross-compiled) or drop (simpler). A decision, not work. | S |

## From Part 2: monitoring

| # | What | Why | Size |
|---|---|---|---|
| 30 | **Lift the 32-monitors-per-round cap in external mode** (keep dispatching until nothing is due, within the tick's time limit). | In loop mode the next second picks up the rest; in external mode they wait 15 minutes. | S |
| 31 | *(Optional)* **Confirm an outage from a second place** before opening an incident (a small probe worker in another region or at the edge). | Every probe leaves from one region, so a network problem on the way looks like the site being down. | L |
| 32 | **Measure warm response time for sleeping demos** (probe twice and record the second, or mark the first as a wake-up). Replaces item 11's caption. | The recorded 3.4 s is the cold start, not the app. | M |

| 33 | **Confirm a failure quickly instead of waiting a full interval** (Martin's idea, 2026-10-06): after a failed check, re-check a few times, seconds apart, within the same round. | Three failures currently means three intervals: 45 to 90 minutes. Quick confirmation checks would open an incident within one tick without checking more often when all is well. | M |
| 34 | **Metrics, Grafana and an outside alarm:** a Prometheus-format `/metrics` view of Lighthouse (checks, their durations, incidents open, request latency, tick duration), pushed to Grafana Cloud's free tier at the end of each tick; dashboards and alert rules kept in the repository (Terraform); an alert when no tick has reported for 20 minutes, which is also item 10's "who watches Lighthouse". A second dashboard can read the checks table directly through a read-only database role. | Lighthouse sees the apps only from outside, and nothing outside sees Lighthouse. Also turns the Prometheus/Grafana line on the CV into public evidence. Free tier checked 2026-10-06: 10,000 series, 50 GB of logs, 14 days. | L |

| 35 | **Check a monitor the moment it's saved, and offer a "Test" button** (Martin's idea, 2026-10-06): after creating or editing a monitor, run its first check at once and show the result beside it; let the settings be tried before saving. The test must go through the same address guard and rate limits as a scheduled check. | A new monitor is created as due but nothing runs it until the next round: up to 15 minutes on the live site, so a mistyped address or wrong expected status is only discovered much later. Builds on item 27. | M |

## From Part 3: data model and tenant isolation

| # | What | Why | Size |
|---|---|---|---|
| 36 | **Daily summaries of the check history** (a small table of per-monitor, per-day counts and latency figures, filled by the clean-up step), used by the status pages. | Status figures are computed from the raw checks on every request, over a table that keeps 90 days. Fine now; it is the first thing that would slow down. | M |
| 37 | **Exercise the "down" migrations in CI** (migrate up, down, up again on a scratch database). | The rollback scripts exist and have never been run. | S |

## From Part 1b: Cloud Run and the edge

| # | What | Why | Size |
|---|---|---|---|
| 16 | **Make the demos edge-only too,** or decide they don't need it: Redacted and GhostChat would check an edge secret the way Lighthouse does (a small middleware in each). | They answer on their `*.run.app` addresses, bypassing Cloudflare and its client-IP header, so their rate limits can be dodged there. | M |
| 17 | **Send each origin only its own edge secret** (one secret per service, or send it to Lighthouse only). | The Worker currently adds Lighthouse's secret to requests for the demos as well. | S |
| 18 | **Longer-lived, smarter edge caching for static files** (fingerprinted file names with a long cache time). | The cache is per Cloudflare location and lasts an hour, so a low-traffic site still fetches static files from the origin often. | M |


| 25 | **Rate limits that survive a restart and are shared between instances:** keep the counters in the database (or Redis) instead of the instance's memory. | Today they reset whenever a container sleeps, and with item 21 each instance would count separately. Needs care: a database write on every limited request must not keep the free database awake. | M |
| 26 | **Keep secret values out of Terraform's state** where the providers allow it (write-only arguments for Secret Manager values, ephemeral generated passwords), and lock the state bucket down (versioning, access limited to one identity). | The state file holds every secret in plain text; today its only protection is being in a private bucket. Partial at best: values passed as plain environment variables stay in the state. To verify: provider support for write-only secret values. | M |

### Accepted, not planned

| What | Why it's accepted | What it would take |
|---|---|---|
| **GhostChat's database accepts connections from any address.** | Cloud Run has no fixed outbound address on the free tier, and a free Atlas cluster has no private networking. The protections in place: TLS, a long random password, a user limited to one database, and message contents encrypted in the browser before they're stored. | A fixed outbound address for Cloud Run (a VPC with Cloud NAT and a reserved IP, a few dollars a month) and an Atlas access list naming only it. |

## Resilience (costed in [resilience-plan.md](resilience-plan.md); all $0 a month to run)

| # | What | Why | Size |
|---|---|---|---|
| 12 | **Nightly backups** of both Postgres databases and GhostChat's MongoDB to a Cloud Storage bucket (free region, versioned, 30-day expiry), from a Cloud Run job on a second scheduler job. | Neon's free plan keeps six hours of history; Atlas M0 has no backups. | M |
| 19 | **A weekly automated restore test:** restore the latest backup into a scratch database, run checks, record how long it took. State the objectives (lose at most 24 hours; recover in N minutes). | A backup that has never been restored isn't known to work. | M |
| 20 | **Canary releases:** new revisions take 5% of traffic, then the rest; with item 6 (automatic rollback). | Safe releases, built into Cloud Run. | M |
| 21 | **Two instances with shared state:** Redacted on two instances (PostgreSQL LISTEN/NOTIFY), with a test proving an edit through one instance reaches a reader on the other; GhostChat with Redis (Upstash free tier). | Shows running more than one copy correctly; costs nothing while idle. | L |
| 22 | **The edge serves a saved copy when the origin fails** (with item 8, edge caching of pages). | Graceful degradation. | M |
| 23 | **A self-managed PostgreSQL pair as the recovery side** (separate project, on the Oracle account's free VMs): primary and streaming replica, point-in-time recovery with pgBackRest or WAL-G, fed nightly from the live dumps, with written failover and restore drills. | Shows operating a database without putting the live site on a small free VM. | L |
| 24 | *(Optional)* **A second region with failover in the Worker.** | Multi-region for the services only; the database stays in one region. | L |

Decided against: an always-warm instance (about $5–8 a month per service, demonstrates nothing), a
managed highly-available database, and a continuous standby (a connected replica keeps Neon awake
and breaks the free compute budget).

## Carried over (noted before the review started)

| # | What | Why | Size |
|---|---|---|---|
| 10 | **Something outside Lighthouse that notices when Lighthouse is down:** on each successful tick, ping a heartbeat service that raises the alarm when the pings stop (a "dead man's switch"); or a Cloud Monitoring uptime check with an email alert. | It watches the other apps and itself, but can't report its own outage. | S |
| 11 | **Caption the demos' response times** ("includes waking the demo"), or measure warm latency separately. | Each 15-minute check wakes a sleeping demo, so the table shows about 3.5 s and reads as a slow app. | S |
| 13 | **Switch on email alerts** (needs SMTP credentials and addresses). | Built, tested, not configured in the live deployment. | S |
| 14 | **Move the Terraform state** from the Oracle bucket to Google Cloud Storage. | Everything else is on Google; the Oracle account is otherwise unused. | S |
| 15 | **Infisical for Lighthouse's secrets** (replacing the local keyring helpers for this project). | Planned for the end of the review. | M |
