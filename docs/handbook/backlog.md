# Build list from the review

Things the part-by-part review found that should be built or fixed. Nothing here is built during
the review; it is all done together at the end. Each item says what, why, and where it came from.
Sizes: S (under an hour), M (a few hours), L (a day or more).

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

## From Part 1b: Cloud Run and the edge

| # | What | Why | Size |
|---|---|---|---|
| 16 | **Make the demos edge-only too,** or decide they don't need it: Redacted and GhostChat would check an edge secret the way Lighthouse does (a small middleware in each). | They answer on their `*.run.app` addresses, bypassing Cloudflare and its client-IP header, so their rate limits can be dodged there. | M |
| 17 | **Send each origin only its own edge secret** (one secret per service, or send it to Lighthouse only). | The Worker currently adds Lighthouse's secret to requests for the demos as well. | S |
| 18 | **Longer-lived, smarter edge caching for static files** (fingerprinted file names with a long cache time). | The cache is per Cloudflare location and lasts an hour, so a low-traffic site still fetches static files from the origin often. | M |


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
| 10 | **Something outside Lighthouse that notices when Lighthouse is down.** | It watches the other apps and itself, but can't report its own outage. To be designed in Part 2. | M |
| 11 | **Caption the demos' response times** ("includes waking the demo"), or measure warm latency separately. | Each 15-minute check wakes a sleeping demo, so the table shows about 3.5 s and reads as a slow app. | S |
| 13 | **Switch on email alerts** (needs SMTP credentials and addresses). | Built, tested, not configured in the live deployment. | S |
| 14 | **Move the Terraform state** from the Oracle bucket to Google Cloud Storage. | Everything else is on Google; the Oracle account is otherwise unused. | S |
| 15 | **Infisical for Lighthouse's secrets** (replacing the local keyring helpers for this project). | Planned for the end of the review. | M |
