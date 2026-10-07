# Build list from the review

Things the part-by-part review found that should be built or fixed. Nothing here is built during
the review; it is all done together at the end. Each item says what, why, and where it came from.
Sizes: S (under an hour), M (a few hours), L (a day or more).

## Build phases

The order the list is built in. Each phase is one pull request: built, tested, then the next.
The pull requests are stacked (each builds on the one before), so they merge in order. The pages
of this handbook describe the code on `main`; their "known gaps" are updated as each phase merges.

Not built in phase 3: the count of server errors that can raise an alarm (part of 59) moves to
phase 6 with the metrics, and uptime is still computed from raw rows on each poll (part of 54)
until the daily summaries of item 36.

| Phase | Theme | Items | State |
|---|---|---|---|
| 1 | **Fix what is broken on the live site** | 27, 28, 29, 30 | Live (#19) |
| 2 | **Clean numbers and first impressions:** analytics that count real readers, link previews, a status page that survives the database | 48, 49, 50, 60, 61, 62 | Live (#21) |
| 3 | **A finished console** | 52, 53, 56, 58, 59, 35, 41, 54, 57 | Live (#22) |
| 4 | **A safer pipeline** | 1, 2, 3, 4, 5, 6, 7, 9, 37 | Live (#26); the same pipeline changes for RBAC-API and GhostChat come with their reviews |
| 5 | **Security and sign-in** | 38, 39, 40, 42, 17, 25 (16 moves to the demos' own reviews; 26 goes with Infisical in phase 8) | Live (#27, #28, #29). Not built: the daily digest of unusual log entries (part of 42) |
| 6 | **Better monitoring** | 33, 32 (replaces 11), 36, 10, 34, 13 | Live (#31, #32; 13 with #30), with the Grafana dashboard and its two alerts applied. Not built yet: 36 (daily summaries) |
| 7 | **Analytics for the job search** | 43, 44, 45, 46, 51 | Live (#33): 44, 45, and 43 (a link generator per tracker row, outside this repository). 46 decided against for now (the email link is counted instead). 51 waits for a few weeks of clean data |
| 8 | **Resilience and operations** | 12, 19, 20, 8, 22, 18, 14, 15, 21 | 15 live (#30: secrets kept in Infisical, read by Terraform without entering its state), with part of 26 |
| Later | Separate projects or optional | 23, 24, 31, 47, 55 | |

Found while verifying phase 5 on the live site, and fixed (#28): Cloud Run's session affinity
was on, which made Google's front end set a 30-day cookie on every visitor of all three sites.

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

## From Part 4: auth and sandboxes

| # | What | Why | Size |
|---|---|---|---|
| 38 | **Name the session cookie with the `__Host-` prefix** (and the sign-in state cookie likewise). | The demos share Lighthouse's parent domain; a compromised demo page could set a cookie for the whole domain and plant a session in a visitor's browser. Browsers refuse a `__Host-` cookie not set by the exact address. | S |
| 39 | **Record sign-ins:** an audit entry for each owner sign-in and each refused attempt, shown in the console, with an alert on refusals. | They are only in the request log today. | M |
| 40 | **List and end the owner's sessions from the console** ("sign out everywhere"). | Signing out ends only the current session. | S |

| 41 | **One sandbox per browser, resumable** (Martin's idea, 2026-10-06): if the browser already has a live sandbox, "Start a sandbox" returns to it; show the time left; offer "reset" to replace it. Recognise the visitor by the existing first-party session cookie, not by browser or device fingerprinting. | Each press creates a new tenant and abandons the old one, and the only per-person limit is by network address. Fingerprinting would contradict the site's privacy stance (no tracking beyond what's necessary) and needs consent in the EU; the cookie already does the job and counts as strictly necessary. | M |
| 42 | **Put the logs to work** (Martin's idea, 2026-10-06): log security events as their own structured entries (owner sign-in, refused sign-in, rejected tick, edge check failures, rate-limit hits, sandbox created); log-based alerts on the ones that matter (free in Cloud Logging); a daily digest of anything unusual, optionally summarised by an AI model. With items 34 (metrics) and 39 (audit trail). | Requests are logged and kept, and nothing reads them. | M |

## Analytics that serve the job search (Martin, 2026-10-06; to decide after Part 5)

The site's strict privacy stance was a design choice, not a requirement. It is the owner's
portfolio, and he wants to learn from its traffic, legally. Whatever is added, the privacy page
changes in the same commit to say exactly what is collected.

| # | What | Why | Size |
|---|---|---|---|
| 43 | **A tagged link per application, joined to the job-hunt tracker:** `?ref=<company>` on every application; a per-company view (came or not, what was read, for how long, which demos). | The capability exists (ref tags, a link maker); it isn't used systematically. No personal data. The strongest lever. | M |
| 44 | **A notification when a tagged visit happens** (email or Telegram). | Tells the owner when a follow-up would land well. No personal data. | S |
| 45 | **Richer events:** CV download, outbound clicks (GitHub, LinkedIn), story read to the end, guided tour completed. | Shows which projects hold attention, and so what to lead with. | M |
| 46 | **Let visitors identify themselves:** a clear "hiring? get in touch" option, or the CV in exchange for an email address. | The best data is given voluntarily, and is unambiguously lawful. | M |
| 47 | *(Last, with care)* **Company-level identification from the network address,** disclosed on the privacy page, with a written justification (legitimate interest). | Says which organisation visited. Addresses are personal data under EU and Kenyan law; limited value for people working from home. | M |

| 48 | **Don't count the status page's auto-refresh as new views** (count a page once per visit, or stop the reload from sending a view). | Found in the live data on 2026-10-06: 342 of 424 recorded views were `/status`, from four visitors; one open tab made 264 in a day. | S |
| 49 | **Let the owner exclude his own browsers,** signed in or not (a "don't count this browser" switch in the console that the counting script respects). | His visits are only excluded while signed in; from a phone or a changing address he looks like many new visitors, which swamps a small site's numbers. | S |
| 50 | **Report engaged visitors separately:** those who stayed at least a few seconds or interacted, beside the raw count. | A view is counted on load, so a glance, a bounce and a scanner in a real browser are indistinguishable: 51 of 61 visitor-days in the first week were one page, zero seconds. | S |
| 51 | **Review the front page for click-through once the data is clean** (after 43, 48, 49, 50): what share of real visitors go on to a project or a demo, and what would raise it. | Few visitors go past the front page, but today's numbers can't say whether that's the page or the noise. | M |
| 52 | **Edit a monitor from the console** (URL, interval, thresholds, timeout, expected status and text, public, pause), and offer the same fields when adding. | The API supports it and `api.ts` wraps it, but no screen uses it; today it means delete and re-add. | M |
| 53 | **Handle an expired session in the console:** on a 401, clear the session and go to the welcome page with a short message; have the sandbox banner say "ended" at zero. | Polls fail forever with an error once the session ends. | S |
| 54 | **Make console polling cheaper:** one combined endpoint for the monitors screen, back off when nothing changes, and stop recomputing uptime from raw rows on every poll (with 36). | Two requests every five seconds per open tab, each waking the database. | M |
| 55 | **Generate the TypeScript types from the Go API** (an OpenAPI description, or a generator from the structs), and fail CI when they differ. | The shapes are hand-written twice and can drift silently. | M |
| 56 | **Small console fixes:** group the behaviour switch's radio buttons by monitor id, not name; add error handling to mode changes on the detail screen and to "older incidents". | Two same-named monitors share a radio group; two actions fail silently. | S |
| 57 | **Browser tests for the owner's screens** (dev sign-in, add an HTTP monitor, open and resolve an incident by hand, the readers report), and unit tests for the views' logic. | Only the sandbox path and two components are tested. | M |
| 58 | **Recover when a release replaces the console under an open tab:** on a failed screen load (`vite:preloadError`), reload the page once. | Screens are loaded on first visit as separate hashed files; after a release the old files are gone, so an open tab can't open a screen it hasn't visited yet. | S |
| 59 | **Better error handling end to end:** a request id on every response and log line, shown with "Something went wrong"; validation errors keyed by field so forms can mark the input; the console reacting by status (401 sign in again, 429 wait, offline vs server fault); a count of 5xx responses that can raise an alarm (with 34 and 42). | Expected errors are specific, but an unexpected one can't be traced from what the user saw to the log line, validation stops at the first problem as one sentence, and the console treats every failure as a message to print. | M |
| 60 | **Link previews and search basics:** Open Graph and Twitter tags (title, description, a preview image per page and per project), a canonical address, `robots.txt` and a sitemap. | A link pasted into LinkedIn, Slack or a chat shows a bare address: no card, no image. Most visitors will arrive from exactly those places. | S |
| 61 | **Keep the status page up without the database:** remember the last built status in memory and serve it, marked with its time, when a rebuild fails (pairs with 22). Replace the full-page reload with a small fetch (pairs with 48). | The status page answers 503 when the database is unreachable, which is when it matters most. | S |
| 62 | **Build the public status once and share it** across the front page, projects, stories, status page and API for a few seconds, instead of per request (pairs with 8 and 36). | Every page view runs the same four queries over raw check rows. | S |

Decided against: device fingerprinting and third-party trackers (both need consent banners in the
EU, and add little beyond 43 to 45).

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
