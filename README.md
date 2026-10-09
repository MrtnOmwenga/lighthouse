# Lighthouse

My engineering portfolio, published as a newspaper by the monitoring system that watches it.
Written in Go. It presents my projects as stories (the problem, the key design decisions, how each
is tested), opens their demos with a short introduction while it checks they are up, monitors
all of them, opens and resolves incidents on its own, and publishes the results. Anyone can also try
the monitoring in a sandbox, without an account.

![The front page](docs/front.png)

<sub>Screenshots are from a local run with simulated monitors and generated history.</sub>

## What it does

**The portfolio**

- **A front page** that leads with who I am, then the projects, each told twice: *in plain terms*
  for anyone, *under the hood* for engineers. A live ticker and a systems table show each
  project's uptime and response times, measured by Lighthouse itself.
- **A long-form story per project** ([example](docs/story.png)), written for a hiring team: the
  problem, how it was solved, the key decisions (the choice, why, and the trade-off), how it is
  tested, and what it doesn't do yet.
- **A launch page for every demo.** Launching one shows "Developing: starting Redacted" with a five-part technical introduction that advances on its
  own. Lighthouse polls the demo's health address (which would also wake a sleeping one); when it answers, a LIVE bar
  drops in, and the demo opens once the introduction ends (or at once, with the button beside the status, which waits with the demo). A demo
  with a guided tour (Redacted has two: one that plays itself, one that guides you) offers it at
  the end instead.
- **An About page** built from the CV ([screenshot](docs/about.png)).
- **Responsive and dependable:** every page works from phone to desktop, reads fully without
  JavaScript, and still renders if the monitoring data can't be loaded.
- **Content is configuration:** a folder of YAML (`deploy/site`: profile, projects, one story per
  project, media), validated strictly at startup so a typo fails there, with every problem listed.

| Starting | Ready |
|---|---|
| ![A demo waking up](docs/launch-waking.png) | ![The demo is ready](docs/launch-ready.png) |

![On a phone: the front page, a story and the systems data](docs/phone.png)

**The console** (`/console`)

- **A sandbox for anyone:** one click, no account, three simulated sites. Switch one to *Down*,
  watch an incident open by itself, post updates (public or internal), switch it back and watch it
  close. It is a private tenant, isolated by row-level security, and expires after two hours.
- **The owner's side:** real HTTP monitors, incidents, and *Readers*: the analytics report, with a
  maker for `?ref=` links (one per job application).
- A small Vue 3 and TypeScript app, built into the Go binary and held to the same Content Security
  Policy as the rest of the site (no inline scripts or styles).

| The sandbox's monitors | An incident |
|---|---|
| ![Monitors with Up, Slow, Flaky and Down switches](docs/console-monitors.png) | ![An incident's timeline and controls](docs/console-incident.png) |

**Monitoring**

- **Checks sites** over HTTP on a schedule: status code range, expected text and response time, recording
  each TLS certificate's expiry date. A pool of workers runs checks concurrently, and several instances can share
  one database without checking anything twice.
- **Opens and resolves incidents automatically.** A monitor must fail several checks in a row to be
  declared down, and pass several in a row to recover (hysteresis), so one blip doesn't page anyone
  and a flapping site doesn't open an incident every minute.
- **Incident timelines** with public updates and internal notes. Only public updates reach the
  status page.
- **A public systems page** ([screenshot](docs/status.png)) rendered on the server: overall state,
  uptime over 24 hours, 7 and 90 days, 90 days of daily 24-hour response-time sparklines (SVG drawn in Go), median and 95th-percentile latency,
  and past incidents. Also available as JSON at `/api/status`.
- **Visit analytics without cookies** ([how it works](#privacy-friendly-analytics)): which pages
  and projects are read, for how long, and which demos are opened; private `?ref=` tags show when a
  link sent with a job application is opened, and what that visitor went on to read.
- **The owner signs in with GitHub.** Only one GitHub account is admitted.

## Privacy-friendly analytics

- **No cookies, no local storage, no stored IP addresses.** A visitor is an HMAC of their IP address
  and user agent, keyed with a random salt that exists for one UTC day and is then deleted: enough
  to count unique readers and follow one visit across pages, not enough to recognise anyone the
  next day or recover an IP. The API's database role can't read the salts; one narrow function
  hands out today's.
- **Signals are respected before anything is sent:** Global Privacy Control and Do Not Track stop
  the script, and the server checks again. Bots and the signed-in owner aren't counted, nor are browsers the
  owner has marked as his own (a cookie in his browser only). The report separates engaged readers
  (five seconds of reading, or a demo opened) from everyone who merely loaded a page.
- **Engaged time is measured by the server.** The page sends a heartbeat every 15 seconds only while
  it is visible and in use; each heartbeat can add at most 20 seconds, measured from the previous
  one on the server, so a client can't claim time that didn't pass.
- **Public numbers hide small counts.** The systems page shows readers per project over 30 days;
  anything under five is shown as "fewer than 5". The detailed report (pages, projects, `?ref=`
  tags, referrers, devices) is the owner's alone, at `/api/analytics`.
- **Tags don't spread:** a `?ref=` tag is read once, then removed from the address bar.
- Records are deleted after 90 days. The rules are explained to visitors at `/privacy`.

## Security

| Concern | How it's handled |
|---|---|
| One visitor seeing another's data | PostgreSQL row-level security, forced on every table and keyed on a transaction-local tenant. The app connects as a role that can't bypass or disable it; a query without a tenant sees nothing. Composite foreign keys stop rows from pointing across tenants. |
| Using the monitor to reach internal services (SSRF) | Checks refuse private, loopback, link-local and CGNAT addresses. The check runs on the address actually dialled, after DNS, so hostnames that resolve inward and redirects are caught too. Sandboxes can't send real traffic at all. |
| Leaking internals on the status page | Failures are stored as categories (`timeout`, `tls`, …), never raw errors. Incidents on private monitors stay private, even after the monitor is deleted. |
| Session theft and CSRF | Random session tokens in HttpOnly, SameSite cookies; the database stores only their SHA-256. Writes from other origins are refused. OAuth state is bound to the browser. |
| Abuse | Rate limits on sandbox creation and writes; strict JSON decoding with size limits; a strict Content Security Policy. Readiness checks for demos are cached and shared, so however many visitors wait on a launch page, a demo gets one health request at a time. Demos' internal health addresses never appear in public output. |
| Unsafe deployments | Configuration refuses to start in production with the development login enabled, without HTTPS, or without the owner's GitHub ID, and lists every problem at once. |

## Tests

```sh
go test -race ./...
```

- **Integration tests against real PostgreSQL** (testcontainers-go). Each test gets its own
  database, cloned from a migrated template in milliseconds, so tests run in parallel.
- **Isolation tests** that try to read, change and link to another tenant's rows, and try to switch
  row-level security off as the app role.
- **Property tests** (rapid) for the incident state machine over random check sequences, and for
  uptime rounding.
- **Fuzzing** for monitor validation, the SVG sparkline and analytics page paths.
- **Analytics tests:** tagged visits attributed across pages; Global Privacy Control, Do Not Track,
  bots and the owner never counted; engaged time capped by the server; events counted once;
  public counts under five hidden; salts rotating daily and unreadable by the app role.
- **Content tests:** the shipped site loads and validates, every page of it renders without
  template errors or inline styles (which the CSP would block), and no internal address appears in
  public output.
- **Launch-page tests** in a real browser (Playwright, run locally): the introduction advances,
  the page switches to ready when the demo answers, opens it after the last slide, skip works,
  and the page is complete without JavaScript.
- **End-to-end HTTP tests:** GitHub sign-in against a fake GitHub (owner, stranger, forged state),
  sandbox isolation, cross-site requests, status-page leaks, incident pagination.
- The important tests have been checked to fail when the protection they cover is removed
  (the RLS policy, atomic claiming, the origin check, the owner check, private-incident hiding).

- **The console:** type-checked, unit-tested (Vitest), and driven end to end in a real browser
  (Playwright) against the running stack: break a site, watch the incident open and close; no
  sideways scrolling at phone width; sandboxes isolated from each other and from the owner's area.

CI runs gofmt, `go vet`, staticcheck, the Go tests with the race detector, fuzzing, govulncheck,
the console's type check, unit tests, build and npm audit, gitleaks, CodeQL, a Trivy scan of the
image, and the compose stack with the console's browser tests.

## Run it

```sh
cp .env.example .env   # set the two passwords; DEV_LOGIN=true for a local owner login
docker compose up --build
```

Then open <http://localhost:8080>. The pages read `deploy/site`; point `SITE_DIR` at another folder
to use your own. `POST /auth/dev` signs you in as the owner locally;
`POST /api/sandbox` starts a sandbox.

The image is a static binary on a distroless base, running as a non-root user with a read-only
filesystem. `lighthouse migrate` applies migrations as the database owner and creates the app's
least-privileged role; `lighthouse serve` runs the server, scheduler and housekeeping (with
`SCHEDULE=external`, calls to `POST /internal/tick` drive the checks instead, for platforms that
freeze idle instances; each call checks everything due, a monitor due within `TICK_SLACK_SECONDS`
counts as due so a call that arrives a little early doesn't skip it, and a sandbox's simulated
monitors are checked as its console reads data). `CONFIRM_SECONDS` re-checks an HTTP monitor that soon
after a result that starts to change its state, so an outage is confirmed within a minute, and
`WARM_THRESHOLD_MS` records a slower passing answer as a sleeping service waking up and checks
again, so response times describe the service and not its start-up.

## Design

```
cmd/lighthouse       serve | migrate | healthcheck
internal/config      settings from the environment, validated together
internal/store       PostgreSQL: migrations, row-level security, queries
internal/monitor     probes (HTTP, simulated), the incident state machine, the scheduler
internal/oidc        verifies Google-signed identity tokens (the scheduler's tick)
internal/status      status page data and the SVG sparkline
internal/site        the portfolio content (profile, projects, stories) and demo readiness
internal/auth        sessions, GitHub OAuth, sandboxes
internal/web         routes, middleware, HTML templates, the JSON API
console/             the console: Vue 3, TypeScript, Vite; Vitest and Playwright
```

Choices worth explaining:

- **Server-rendered pages.** These pages are read far more often than anything else, and they
  should load fast and work when things are broken. Go's `html/template` escapes by context. The
  only script is the launch page's introduction, and the page works without it.
- **A broadsheet, because the data is real.** The newspaper design (Newsreader and IBM Plex Sans
  Condensed on pink paper, one claret accent) carries live figures: the "markets data" is uptime.
  Fonts are served from the site itself, so the Content Security Policy allows nothing from other
  origins, not even inline styles.
- **The database is the queue.** Due monitors are claimed with one atomic `UPDATE`, so there is no
  separate queue or lock service to run, and instances can be added freely.
- **Probes run outside transactions.** A check can take 30 seconds; holding a database
  connection that long would limit concurrency to the pool size.

## Deploying it

**Live at [martinomwenga.com](https://martinomwenga.com)**, for free: Lighthouse and both demos run
on Google Cloud Run behind a Cloudflare edge ([`deploy/`](deploy/README.md)).

- **Scale to zero.** Each app sleeps when idle and wakes on the first request; the launch page's
  "starting Redacted…" introduction covers that wake-up. An idle Cloud Run instance gets no CPU, so
  Lighthouse can't keep its own clock there: Cloud Scheduler calls `POST /internal/tick` every 15
  minutes with a Google-signed identity token, which Lighthouse verifies with the standard library.
- **Nothing bypasses the edge.** A Cloudflare Worker routes each hostname to its service, passes on
  the visitor's IP and adds a secret header; Lighthouse refuses requests without it, so its public
  origin can't be used to skip Cloudflare or forge an IP.
- **No stored cloud credentials.** Each repository's release workflow builds and signs the image,
  then deploys through Workload Identity Federation (GitHub's OIDC token for a short-lived Google
  one, accepted only from that repository's release branch): copy to Artifact Registry, run the
  migrations as a job, deploy by digest, smoke-test through the edge.
- **Free by design.** Every setting follows from a free-tier limit: a Neon project per app, 15-minute
  ticks so the databases can sleep, seven secrets (one over the free six), two image versions kept, static files
  cached at the edge. All of it is Terraform ([`deploy/cloudrun`](deploy/cloudrun)).

**It also runs on Kubernetes.** [`deploy/terraform`](deploy/terraform) and
[`deploy/k8s`](deploy/k8s) put the same images on one k3s host with no open inbound ports
(Cloudflare Tunnels, SSH behind Cloudflare Access), kept in step by Flux, with every pod locked
down (non-root, read-only, no capabilities, restricted Pod Security, default-deny network
policies). It's proven on a local kind cluster and validated in CI. It isn't the live deployment
because the free Oracle host never became available; [the runbook](deploy/README.md#why-cloud-run-and-not-the-k3s-host)
explains why, and lists what deploying for real found that validation couldn't.

Lighthouse can also email the owner when a monitor opens or resolves an incident (SMTP settings;
not switched on in the live deployment yet).
