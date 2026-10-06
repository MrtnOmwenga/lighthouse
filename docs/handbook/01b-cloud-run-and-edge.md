---
title: "Shipping the apps: Cloud Run and the edge"
project: lighthouse (hosts Lighthouse, Redacted and GhostChat)
topics: [cloud-run, cloudflare-workers, terraform, scale-to-zero, secrets, least-privilege, neon, free-tier]
sources:
  - deploy/cloudrun/services.tf
  - deploy/cloudrun/edge.tf
  - deploy/cloudrun/edge/worker.js
  - deploy/cloudrun/identity.tf
  - deploy/cloudrun/secrets.tf
  - deploy/cloudrun/databases.tf
  - deploy/cloudrun/registry.tf
  - internal/web/server.go
verified: 2026-10-05
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

About thirty lines (`deploy/cloudrun/edge/worker.js`):

1. **Routes by hostname.** `ORIGINS` is a JSON map from hostname to the service's `*.run.app`
   address, filled in by Terraform from the services it created. Unknown hostname: 404.
2. **Rewrites the address, keeps everything else:** same path, query, method, headers and body, so
   WebSocket upgrades pass through untouched.
3. **Sets three headers,** replacing anything the visitor sent under those names:
   - `X-Client-IP`: the visitor's address, which Cloudflare knows (`CF-Connecting-IP`) and the
     origin otherwise wouldn't (it would only see Cloudflare);
   - `X-Forwarded-Host`: the hostname the visitor used;
   - `X-Edge-Secret`: proof the request came through this Worker.
4. **Caches static files** (scripts, styles, images, fonts) at the edge for an hour; everything
   else goes to the origin every time. `redirect: "manual"` hands redirects back to the browser
   instead of following them at the edge.

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

Lighthouse has two (`internal/web/limiter.go`), both *token buckets*: each client has a bucket
that holds a few tokens and refills at a steady rate; every action spends one; an empty bucket
means "429 Too Many Requests".

| Limit | Allowance | Protects |
|---|---|---|
| New sandboxes | 6 an hour per address, at most 3 in a burst | Each sandbox creates a tenant with monitors and data; unlimited creation would fill the free database |
| Writes to the API (anything that isn't a read) | 5 a second per address, bursts of 20 | Abuse of the console's API |

The buckets live in the instance's memory and idle ones are forgotten, so they reset when the
container stops. The demos have their own: Redacted allows 300 requests a minute (10 for sign-in);
GhostChat 300 per 15 minutes (10 for sign-in) and 20 messages per 10 seconds.

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

Secret Manager holds exactly six secrets, its free allowance: the owner and app database passwords
for Lighthouse and Redacted, the GitHub OAuth client secret, and GhostChat's MongoDB connection
string. Three lower-stakes values (the demos' token-signing keys and the edge secret) are plain
environment variables instead: only someone who can read a service's settings can see them, and
anyone with that access can already deploy code that reads the service's secrets.

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

## Why is everything sized the way it is?

Every setting follows from a free-tier limit:

| Limit | Choice |
|---|---|
| Cloud Run: 180,000 vCPU-seconds, 360,000 GiB-seconds, 2M requests a month | Scale to zero, CPU only during requests, one instance each |
| Neon: 100 compute-hours a month *per project* | A project per app; the scheduler ticks every 15 minutes so the databases sleep most of the time |
| Secret Manager: 6 secret versions | Exactly six |
| Artifact Registry: 0.5 GB | Only amd64 is copied; the two newest versions of each image are kept |
| Google egress: 1 GB a month | Static files cached at the edge |
| Cloud Scheduler: 3 jobs | One |

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
layer does. Here, none is in place: one instance, one region, free database plans.

## Known gaps

- **The demos can be reached directly.** Only Lighthouse checks the edge secret; Redacted and
  GhostChat answer on their `*.run.app` addresses, bypassing Cloudflare (and its client-IP header,
  so their rate limits can be dodged there).
- **One edge secret, sent to all three origins.** The Worker adds Lighthouse's secret to requests
  for the demos too. They ignore it, but a secret should only go where it's needed.
- **One instance, no redundancy.** A crash or a deploy means a cold start for the next visitor; a
  burst beyond 250 concurrent requests is queued briefly, then refused. Fine for a portfolio, not for a business.
- **Cold starts are visible:** about 2 s for the first visitor after a quiet spell.
- **The edge cache is per Cloudflare location and lasts an hour,** so on a low-traffic site many
  requests for static files still reach the origin.
- **In-memory state resets** whenever a container stops: rate-limit counters and small caches
  start empty.
- **Terraform's state file contains every secret value** (generated passwords included). It is in
  a private bucket; it should be treated as a secret itself.
- **GhostChat's database accepts connections from any address,** because Cloud Run has no fixed
  address on the free tier; the password and TLS are the protection.
- **Visitors far from `us-east4` pay for the distance** on every uncached request.

## Questions and answers

**What happens when someone visits after the site has been idle for an hour?**
Cloudflare answers the TLS handshake immediately; the Worker forwards the request; Cloud Run starts
a container (the Go binary starts in well under a second), which wakes the Neon database with its
first query; the page comes back in about two seconds. The next requests take under one.

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
