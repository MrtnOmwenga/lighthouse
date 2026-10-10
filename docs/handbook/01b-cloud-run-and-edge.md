---
title: "Shipping the apps: Cloud Run and the edge"
project: lighthouse (hosts Lighthouse, Redacted and GhostChat)
topics: [cloud-run, cloudflare-workers, terraform, scale-to-zero, secrets, infisical, backups, restore-testing, edge-caching, least-privilege, neon, free-tier]
sources:
  - deploy/cloudrun/services.tf
  - deploy/cloudrun/edge.tf
  - deploy/cloudrun/edge/worker.js
  - deploy/cloudrun/identity.tf
  - deploy/cloudrun/secrets.tf
  - deploy/cloudrun/databases.tf
  - deploy/cloudrun/registry.tf
  - deploy/cloudrun/infisical.tf
  - deploy/cloudrun/backups.tf
  - deploy/cloudrun/backup/backup.sh
  - deploy/cloudrun/grafana.tf
  - deploy/cloudrun/alerts.tf
  - internal/web/server.go
verified: 2026-10-09
---

# Shipping the apps: Cloud Run and the edge

The [pipeline](01a-pipeline.md) produces an image and points a service at it. This page is about
what that service is: where the three apps run, how a visitor's request reaches them, and how the
whole arrangement is defined in Terraform (`deploy/cloudrun`).

## The words used on this page

- **Hostname:** the name part of an address: `martinomwenga.com`, `redacted.martinomwenga.com`,
  `ghostchat.martinomwenga.com`. One per app.
- **Origin:** the server that actually runs an app. Here, a Cloud Run service, reachable at its own
  address ending in `.run.app`.
- **Edge:** the servers that sit between visitors and the origin, at the outer boundary of the
  system. Cloudflare runs them in hundreds of cities, so a visitor's request first lands close to
  them. The edge handles HTTPS, caching and routing before anything reaches the origin.
- **Worker:** a small program of ours that Cloudflare runs at the edge for every request.
- **Edge secret:** a long random password known only to the Worker and to Lighthouse. The Worker
  attaches it to every request it forwards; Lighthouse refuses requests that don't carry it. It
  proves "this came through our edge".
- **Service (Cloud Run):** one app, run from one container image, at one address. Each of the three
  projects is its own service.
- **Instance:** a running copy of a service's container. A service has zero, one or more instances
  at any moment.

## How does a request get from the browser to the app?

```
browser ── https://martinomwenga.com/projects
   │  DNS: the name resolves to Cloudflare (the record's own address, 100::, is a placeholder
   │       that discards traffic; it only exists so Cloudflare answers for the name)
   ▼
Cloudflare edge ── TLS ends here ── a route (martinomwenga.com/*) hands the request to the Worker
   ▼
Worker (edge/worker.js) ── looks the hostname up in ORIGINS ── adds three headers ──┐
                                                                                   ▼
Cloud Run ── https://lighthouse-….run.app/projects ── starts a container if none is running
   ▼
container (the Go binary) ── checks the edge secret ── renders the page ── Neon Postgres
```

There are three hostnames (`martinomwenga.com`, `redacted.…`, `ghostchat.…`), three routes, one
Worker, three Cloud Run services.

## What does the Worker do?

About a hundred lines (`deploy/cloudrun/edge/worker.js`), with its own tests
(`worker.test.js`):

1. **Routes by hostname.** `ORIGINS` is a JSON map from hostname to the service's `*.run.app`
   address, filled in by Terraform from the services it created. Unknown hostname: 404.
2. **Rewrites the address, keeps everything else:** same path, query, method, headers and body, so
   WebSocket upgrades pass through untouched.
3. **Sets three headers,** replacing anything the visitor sent under those names:
   - `X-Client-IP`: the visitor's address, which Cloudflare knows (`CF-Connecting-IP`) and the
     origin otherwise wouldn't (it would only see Cloudflare);
   - `X-Forwarded-Host`: the hostname the visitor used;
   - `X-Edge-Secret`: proof the request came through this Worker, **sent only to Lighthouse**,
     the one origin that checks it. A secret goes only where it is needed.
4. **Keeps a copy of Lighthouse's public pages** (next section).
5. **Caches static files** at the edge: for a year when the address carries a fingerprint of the
   file's contents (`style.css?v=349ae5…`) or is a font, otherwise for an hour.
   `redirect: "manual"` hands redirects back to the browser instead of following them at the edge.

## What does the edge do with pages?

For the public pages (front page, projects, stories, about, status, privacy, and `/api/status`),
asked for anonymously:

- **A copy less than a minute old is served as it is,** without contacting the origin. Most
  visits never wake the app or its database.
- **When the origin fails or can't be reached, the last copy is served** (kept up to a day), with
  a line at the top of the page: "The site isn't responding right now. This is a copy saved at …".
  A status page that silently showed yesterday's "all operational" would be worse than an error,
  so the copy says what it is.

What is never kept: any request carrying a session cookie, anything but a `GET`, any answer that
isn't a 200 or that sets a cookie, the console, the rest of the API, and the demos. Counting
visits isn't affected, because the page's script reports a visit to the API; a page load by itself
never counted.

## Why a Worker, and not Cloud Run's own custom domains or a load balancer?

- A Google load balancer in front of Cloud Run costs about $18 a month.
- Cloud Run's built-in domain mapping needs the domain verified with Google and its own
  certificate, which conflicts with Cloudflare proxying the same name.
- A Worker is free (100,000 requests a day), is defined in the same Terraform, and gives a place
  to add the client IP, the edge secret and caching.

## Why does Lighthouse refuse requests that didn't come through the edge?

Cloud Run addresses (`*.run.app`) are public. Without a check, anyone could skip Cloudflare by
calling the origin directly, and could send a forged `X-Client-IP` to dodge the per-visitor rate
limits (and skew the visitor counts).

So Lighthouse runs with `EDGE_SECRET` (`edgeOnly` in `internal/web/server.go`): a request whose
`X-Edge-Secret` doesn't match gets a 404. The comparison is constant-time, so the secret can't be
guessed byte by byte from response timing. Three paths are exempt: `/healthz` and `/readyz`
(Cloud Run's startup probe calls the container directly) and `/internal/tick` (it has its own
authentication, see the monitoring page).

The secret is a 48-character random value generated by Terraform and given to both sides: the
Worker (as a secret binding) and the service (as an environment variable).

## Why one service per app, instead of sharing one?

Each project is its own Cloud Run service: `lighthouse`, `redacted`, `ghostchat`. A service runs
one container image, so sharing one would mean packing three unrelated programs into a single
image. Keeping them apart means each one deploys on its own (from its own repository), sleeps and
wakes on its own, has its own identity and secrets, and fails on its own: a crash in GhostChat
can't take the portfolio down.

## What are the rate limits, and why does the visitor's address matter?

A rate limit caps how often one client may do something, so a single visitor or script can't
exhaust the system. Each client needs an identity for that; here it is the visitor's IP address,
which is why the Worker passes it on and why it mustn't be forgeable.

Lighthouse has two:

| Limit | Allowance | Kept in | Protects |
|---|---|---|---|
| New sandboxes | 6 an hour per visitor | The database | Each sandbox creates a tenant with monitors and data; unlimited creation would fill the free database |
| Writes to the API (anything that isn't a read) | 5 a second per address, bursts of 20 | The instance's memory | Abuse of the console's API |

- **The sandbox limit is counted in the database** (a small table reached only through one
  function), so it survives the container sleeping and would be shared between instances. The
  visitor is identified by a keyed hash of their address under a secret that changes daily, never
  by the address itself.
- **The write limit is a token bucket in memory** (`internal/web/limiter.go`): each client has a
  bucket that holds a few tokens and refills at a steady rate; every action spends one; an empty
  bucket means "429 Too Many Requests". It covers a window of seconds, so losing it on a restart
  costs nothing.

The demos have their own: Redacted allows 300 requests a minute (10 for sign-in); GhostChat 300
per 15 minutes (10 for sign-in) and 20 messages per 10 seconds.

## What is a Cloud Run service here?

`deploy/cloudrun/services.tf` defines all three from one block. The settings that matter:

| Setting | Value | Why |
|---|---|---|
| `min_instance_count` | 0 | Scale to zero: no container runs, and nothing is billed, when nobody is visiting |
| `max_instance_count` | 1 | The demos keep real-time state in memory (who is connected to which document or room), which two instances wouldn't share; and one instance can't exceed the free tier by scaling |
| `cpu_idle` | true | CPU is only allocated, and billed, while a request is being handled |
| `max_instance_request_concurrency` | 250 | One instance serves many requests at once; an open WebSocket counts as a request for as long as it's open |
| `session_affinity` | true | Keeps a visitor on the same instance; matters only if the instance limit is ever raised |
| `timeout` | 300 s (Lighthouse), 3600 s (demos) | The longest a single request may last; an hour for the demos because a WebSocket is one long request, after which the client reconnects |
| `startup_probe` | the app's health path, every 3 s, up to 20 tries | Cloud Run sends traffic only once the app answers |
| CPU / memory | 1 CPU; 256 MiB (Go), 512 MiB (Node) | The smallest that runs each app comfortably |

The cost of scale to zero is the **cold start**: the first request after an idle spell waits for a
container to start (and, for Lighthouse and Redacted, for the database to wake). Measured on the
front page: about 2.3 s cold, about 0.9 s warm from Nairobi.

## Who owns the image, Terraform or the pipeline?

Both would fight over it, so the line is explicit. Terraform creates each service with a
placeholder image and then ignores the image field (`lifecycle { ignore_changes = [image] }`).
The release workflow changes only the image. Everything else (environment, secrets, limits,
identity) is Terraform's.

## How do the apps get their configuration and secrets?

Two kinds of environment variable, both declared in `services.tf`:

- **Plain settings** (`PUBLIC_URL`, `SCHEDULE=external`, `DEMO_MODE`, …), written into the service.
- **Secrets**, as references: the service is told "set `PGPASSWORD` from the secret
  `lighthouse-db-app`", and Cloud Run fetches the value from Secret Manager when a container
  starts. The value is never in the service's definition.

Secret Manager holds eight secrets, two more than its free allowance (about $0.12 a month): the
owner and app database passwords for Lighthouse and Redacted, the GitHub OAuth client secret,
GhostChat's MongoDB connection string, the mailbox password for incident emails, and the token for
reporting metrics. Three lower-stakes values (the demos' token-signing keys and the edge secret)
are plain environment variables instead: only someone who can read a service's settings can see
them, and anyone with that access can already deploy code that reads the service's secrets.

## Where are secrets kept and changed?

In two places, by where they come from:

- **Generated by Terraform:** the database passwords. Nobody ever types them; they are in
  Terraform's state.
- **Obtained by a person:** the OAuth client secret, the MongoDB address, the mailbox password,
  the metrics token. These are kept in **Infisical**, a secrets manager: one place to change them,
  with a history and access control (`infisical.tf`).

Terraform signs in to Infisical as a machine identity and reads those values as *ephemeral*
resources, which exist only while a plan or apply runs. It passes them to Secret Manager through
*write-only* arguments. So they are in neither Terraform's state nor its plans. Each secret's
version number in Infisical is read as ordinary data, so changing a value there and applying
copies the new one across.

The services read Secret Manager only. If Infisical is unreachable, nothing that is running
notices; only the next `terraform apply` would.

## Who is allowed to do what?

Each service runs as its own service account (`run-lighthouse`, `run-redacted`, `run-ghostchat`),
and each secret is readable only by the account of the service it belongs to
(`deploy/cloudrun/secrets.tf`). GhostChat's container can't read Lighthouse's database password,
even though they share a project.

| Identity | Can |
|---|---|
| `run-<service>` (one per service) | Read its own secrets. Nothing else |
| `lighthouse-tick` | Nothing: it exists only so the scheduler's calls carry a verifiable identity |
| `github-deployer` | Push images, update the three services, run the migration jobs, act as the three runtime accounts |
| Everyone (`allUsers`) | Call the services over HTTP (they are public websites) |

## Where is the data?

- **Lighthouse and Redacted: Neon Postgres**, one Neon project each (`databases.tf`), in
  `aws-us-east-1`, next to Cloud Run's `us-east4`. A Neon database also scales to zero, sleeping
  after five idle minutes.
- **GhostChat: MongoDB Atlas**, a free M0 cluster, created by hand (Atlas API keys need a fixed IP
  allowlist).
- Each app connects as a **least-privilege role** that its own migrations create
  (`lighthouse_api`, `rbac_app_login`); the database owner's password is given only to the
  migration jobs. GhostChat's Atlas user can read and write the `ghostchat` database and nothing
  else.

## How is the data backed up, and how is that known to work?

Neon's free plan keeps six hours of history and Atlas's free tier keeps none, so there are two
jobs (`backups.tf`, `backup/backup.sh`), both Cloud Run jobs started by Cloud Scheduler:

- **`backup`, every night:** `pg_dump` of both PostgreSQL databases and `mongodump` of
  GhostChat's, into a Cloud Storage bucket that keeps every version of an object and deletes
  anything older than 30 days.
- **`restore-test`, every week:** fetches the latest backups, starts a scratch PostgreSQL inside
  its own container, restores both dumps, and checks what came back: there are tables,
  Lighthouse has its monitors, and its newest check is at most 26 hours old. It refuses a backup
  more than a day old, and records how long the restore took.

**The objective:** lose at most 24 hours of data. Restoring both databases takes seconds at this
size.

Either job failing sends an email. They run in the stock `postgres:17-alpine` image, pinned by
digest, with the script passed as an argument, as their own service account, which can read the
three database credentials and use that one bucket.

**Why the restore test matters:** on its first run it failed, correctly. The backup job had
reported success three times while storing seven bytes per database: the image's built-in
download tool cuts a binary upload at its first zero byte, and a PostgreSQL dump has one in its
header. A backup that has never been restored isn't known to work.

## Why is everything sized the way it is?

Every setting follows from a free-tier limit:

| Limit | Choice |
|---|---|
| Cloud Run: 180,000 vCPU-seconds, 360,000 GiB-seconds, 2M requests a month | Scale to zero, CPU only during requests, one instance each |
| Neon: 100 compute-hours a month *per project* | A project per app; the scheduler ticks every 15 minutes so the databases sleep most of the time |
| Secret Manager: 6 secret versions | Eight: two over, about $0.12 a month, for incident emails and metrics |
| Artifact Registry: 0.5 GB | Only amd64 is copied; the two newest versions of each image are kept |
| Google egress: 1 GB a month | Static files and public pages cached at the edge |
| Cloud Scheduler: 3 jobs | Three: the tick, the nightly backup, the weekly restore test |
| Cloud Storage: 5 GB in three US regions | Backups and Terraform's state, in `us-east1` |

## How would redundancy be added?

Redundancy means no single failure takes the system down. It is added in layers, cheapest first:

1. **More than one instance.** Raise the instance limit (and keep a minimum of two). Cloud Run
   already spreads requests across instances and across the region's data centres (zones), so one
   crashed instance or one failed zone isn't an outage. The catch is state: anything kept in one
   instance's memory (who is connected to which document, rate-limit buckets) must move somewhere
   shared, such as the database or Redis. Redacted and GhostChat have code for this already
   (PostgreSQL LISTEN/NOTIFY, a Redis adapter).
2. **Safe releases.** Cloud Run keeps every deployed version (a *revision*) and can split traffic
   between them: send 5% to the new one, watch, then move the rest, or switch back instantly. This
   is blue-green and canary deployment, built in; no extra load balancer is needed.
3. **A redundant database.** Usually the hardest part. A standby copy in another zone that takes
   over automatically, read replicas to spread reads, and backups with point-in-time restore for
   mistakes that replication would faithfully copy. Managed databases sell this as a plan.
4. **More than one region.** Run the services in two regions with something in front that sends
   visitors to a healthy one. The Worker could do that here: try one origin, fall back to the
   other. The data has to follow, which means replicating the database between regions and
   deciding what happens when they disagree.
5. **The edge as a cushion.** Serve a cached copy of a page when the origin doesn't answer, so a
   short outage is invisible to readers.

Each layer costs money and complexity, so it is added when an outage would cost more than the
layer does. Here, layers 2 and 5 are in place (without the percentage split, which needs more
traffic than this site has to mean anything), plus nightly backups; one instance, one region and
free database plans remain.

## What changed on 2026-10-09?

- **A secret per service.** The demos now also refuse anything that didn't come through the edge,
  and each service has its own secret, so one can't use what it receives to pass as the edge to
  another.
- **Pages leave as the origin wrote them.** Cloudflare was inserting its analytics script into
  every HTML page on all three sites. Each site's security policy (`script-src 'self'`) stopped
  it running, so nothing was collected, but the pages weren't what the servers sent. It was found
  by GhostChat's check of served files against their published hashes. The Worker now marks HTML
  `no-transform`, the standard instruction not to alter a response in transit.
- **A scheduled call is written and not yet running.** The Worker has a handler that calls the
  demos' clean-up paths each hour, to give services that scale to zero a clock. Cloudflare won't
  create the schedule until the account has a workers.dev subdomain.

## Known gaps

- **The demos can be reached directly.** Only Lighthouse checks the edge secret; Redacted and
  GhostChat answer on their `*.run.app` addresses, bypassing Cloudflare (and its client-IP header,
  so their rate limits can be dodged there).
- **One instance, no redundancy.** A crash or a deploy means a cold start for the next visitor; a
  burst beyond 250 concurrent requests is queued briefly, then refused. Fine for a portfolio, not
  for a business. Of the redundancy layers above, two are in place: safe releases (a tested
  candidate and automatic rollback, on the pipeline page) and the edge as a cushion.
- **Cold starts are visible** to whoever arrives when the edge's copy is more than a minute old:
  about 2 s.
- **MongoDB's backup gets a weaker test than PostgreSQL's.** There is no MongoDB server in the
  test container, so the archive is decompressed end to end, not restored.
- **A backup job that never starts sends no email.** The alert matches the failure lines the
  script prints; the weekly restore test would catch it, by refusing a stale backup.
- **Database passwords generated by Terraform are in its state.** The state is in a private,
  versioned bucket and should be treated as a secret.
- **The write limit resets** whenever a container stops (it covers seconds, so little is lost).
- **GhostChat's database accepts connections from any address,** because Cloud Run has no fixed
  address on the free tier; the password and TLS are the protection.
- **Visitors far from `us-east4` pay for the distance** on every request the edge can't answer.
- **A saved copy can be a day old.** It says so on the page; a reader who ignores the line could
  still take stale figures for current ones.

## Questions and answers

**What happens when someone visits after the site has been idle for an hour?**
Cloudflare answers the TLS handshake immediately; the Worker forwards the request; Cloud Run starts
a container (the Go binary starts in well under a second), which wakes the Neon database with its
first query; the page comes back in about two seconds. The next requests take under one.
If the page is a public one and the origin fails to start at all, the edge serves its last copy,
marked as saved.

**Does anything in this path set a cookie on a visitor?**
Not for reading the site. It did, by accident: Cloud Run's "session affinity" was switched on,
which makes Google's front end set a 30-day cookie on every visitor. With one instance there is
nothing to stick to, so it is off. It was found by looking at the live site's response headers,
not by reading the configuration.

**Why can't someone just call the `run.app` address?**
For Lighthouse they get a 404: the request lacks the edge secret. For the two demos they can,
today (see Known gaps).

**Why not keep one instance always running, to avoid cold starts?**
An always-on instance is billed around the clock, which leaves the free tier. Scale to zero costs
nothing while idle and about a second on the first visit.

**Why `max_instance_count = 1`?**
The demos hold live collaboration state in memory, which a second instance wouldn't see; and a
single instance bounds the bill. Redacted and GhostChat both have code paths for several instances
(PostgreSQL LISTEN/NOTIFY, a Redis adapter), unused here.

**If Terraform is run again, does it undo a deploy?**
No. It ignores the image field, so the image the pipeline deployed stays.

**How would this change for real traffic?**
Raise the instance limit (the demos' multi-instance paths then matter), keep a minimum of one
instance for the main site, put secrets back in Secret Manager across the board, move the
databases to paid plans with backups, and consider a region near the users.
