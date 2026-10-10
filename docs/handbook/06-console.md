---
title: "The console"
project: lighthouse
topics: [vue, typescript, single-page-app, vite, go-embed, polling, json-api, problem-details, pagination, csp]
sources:
  - console/src/api.ts
  - console/src/router.ts
  - console/src/session.ts
  - console/src/poll.ts
  - console/src/views/Monitors.vue
  - console/src/views/Incidents.vue
  - console/vite.config.ts
  - console/e2e/sandbox.spec.ts
  - console/e2e/owner.spec.ts
  - console/src/components/MonitorForm.vue
  - console/src/views/Security.vue
  - internal/web/console.go
  - internal/web/server.go
  - internal/web/api.go
  - Dockerfile
verified: 2026-10-07
---

# The console

The console is the part of Lighthouse you operate: add monitors, break a simulated site, watch an
incident open, post updates, preview the status page, and (owner only) read the readership report.
The owner and a sandbox visitor use the same console on different data.

## The words used on this page

- **Single-page app (SPA):** a web app where the server sends one page and a bundle of JavaScript,
  and the JavaScript draws every screen after that, fetching data as needed.
- **Server-rendered page:** the opposite: the server builds the finished HTML for each address.
- **JSON API:** addresses that return data (as JSON text), not pages, for a program to read.
- **Bundle:** the app's source files combined and shrunk into a few files a browser downloads.
- **Polling:** asking the server "anything new?" on a timer.
- **Client-side routing:** the app changes the address bar and the screen itself, without asking
  the server for a new page.
- **Embedding:** packing files inside the compiled program, so there is nothing separate to ship.

## Why is the console a separate app, when the rest of the site isn't?

The public site (front page, projects, status) is server-rendered: pages that are mostly read, must
load fast, and must work without JavaScript. The console is the opposite: screens that change every
few seconds and respond to clicks, used by few people for minutes at a time. Each part uses the
approach that fits it.

The console is about 1,600 lines of Vue 3 and TypeScript with two runtime dependencies (Vue and
its router).

## How does the console reach the browser?

It is built once and packed into the Go program:

1. `npm run build` type-checks the code and has Vite produce the bundle into
   `internal/web/console/` (`vite.config.ts`).
2. `//go:embed all:console` in `internal/web/console.go` packs that folder into the binary when Go
   compiles.
3. The Dockerfile does both in order: a Node stage builds the console, the Go stage copies the
   output in and compiles. The final image is still one file.

So there is no second server, no separate deployment and no version mismatch: a given binary
always carries the console built from the same commit as its API. The build output isn't committed;
a placeholder page is, so `go build` works without Node.

## How are its files served?

`console()` in `console.go` has three rules:

| Request | Answer | Cached |
|---|---|---|
| A real file under `assets/` | The file | For a year, marked immutable |
| A missing file under `assets/` | 404 | |
| Anything else under `/console/` | `index.html` | Never (`no-cache`) |

- **Why a year?** Vite puts a hash of each file's contents in its name. If the code changes, the
  name changes, so an old name can be cached forever.
- **Why never cache `index.html`?** It is the file that names the current bundle. Fetching it
  fresh is what makes a new release take effect.
- **Why answer `index.html` for unknown addresses?** `/console/incidents/42` isn't a file. The
  app's router understands it, so the server hands over the app and lets it draw the right screen.
  Without this, reloading on any screen but the first would be a 404.
- **Why 404 for a missing asset?** Otherwise a stale page asking for an old script would receive
  HTML and fail with a confusing error.

The site's Content-Security-Policy forbids inline scripts, so the build is configured to produce
none (`modulePreload: { polyfill: false }`).

## How does it talk to the server?

Everything goes through one function, `call` in `api.ts`:

- Requests go to the same address the console came from, so the browser attaches the session
  cookie by itself. The console never sees or stores the token (it can't: the cookie is
  `HttpOnly`).
- On an error the server answers in one standard shape (RFC 9457 "problem details": a status, a
  title, a detail). `call` turns that into an `ApiError`, which says what to tell the person:
  what to fix and where, to wait, to sign in again, or what to quote in a report.
- The data shapes (`Monitor`, `Check`, `Incident`…) are TypeScript types, so a typo in a field
  name fails the build.

On the server, every handler returns an error and one wrapper (`errs` in `server.go`) translates
it: not found, forbidden, "that slug is in use" (from the database's unique rule), "a value is
outside its range" (from a database check). Anything unexpected is logged in full and the user
gets a plain "Something went wrong", so internal details never reach the browser. Request bodies
are capped at 64 KB and unknown fields are rejected (`readJSON`).

Three things make errors usable:

- **Validation reports every problem, each against its field** (`errors: [{field, message}]`), so
  a form marks all the inputs that are wrong at once, not the first.
- **Every response carries a request id** (`X-Request-Id`), which is also on that request's log
  lines. An unexpected failure repeats the id in its body, and the console shows it: "If you
  report it, quote 73701787986171d3". That is the link from what a person saw to what the server
  recorded.
- **The console reacts by status:** a 401 means the session is gone (back to the welcome page,
  which says why), a 429 says to wait, and no answer at all says to check the connection.

## How does the console know who is signed in?

It asks. `loadSession` calls `/api/session` once and keeps the answer (role, login, expiry) in a
small shared object (`session.ts`). The router checks it before every screen (`router.ts`):

- signed out → the welcome page;
- a sandbox asking for the readers page → sent to monitors;
- signed in and on the welcome page → sent to monitors.

**These checks are for convenience, not security.** Anyone can edit JavaScript in their own
browser. The real rule is on the server: `/api/analytics` answers 403 to a sandbox whatever the
console shows, and every query is confined to the session's tenant by the database. A browser test
asserts exactly this (`sandboxes are isolated and never see the owner area`).

## What can be done with a monitor?

- **Add or edit one** through the same form (`MonitorForm.vue`): address, interval, when it counts
  as down, timeout, accepted status and text, public, paused.
- **Test settings before saving** ("Test these settings"): the server probes with them and
  records nothing. The test passes the same validation and the same address guard as a scheduled
  check, so a sandbox can only try simulated monitors.
- **Check now:** a monitor is checked the moment it is saved, and on request, so a mistyped
  address is seen at once and not at the next round.

## How does the screen stay up to date?

By polling. `usePoll` (`poll.ts`) runs a view's `refresh` immediately, then every five seconds
(ten for the status preview), and:

- **skips while the tab is hidden**, so a forgotten tab costs nothing;
- **waits for one request to finish before scheduling the next**, so slow answers can't pile up;
- **stops when you leave the screen;**
- **eases off when nothing changes:** after six identical answers it polls a third as often, and
  returns to full speed the moment something changes or the person does something.

The monitors screen gets everything it shows in one request (`GET /api/overview`).

**Why not push updates from the server (WebSockets or server-sent events)?** Those need a
connection held open per viewer. On Cloud Run an open connection keeps the container awake and
billed, and with more than one instance, updates would have to be relayed between them. Checks
happen every ten seconds at most, so a five-second poll is as fresh as the data gets, with nothing
extra to run. The GhostChat project is where real-time push is the point.

One change is shown before the server confirms it: switching a simulated site's behaviour
(`setMode` in `Monitors.vue`) updates the button at once and puts it back if the request fails.
This is called an optimistic update.

## How are long lists handled?

Incidents and checks are fetched a page at a time using a **cursor**: "the items after this one",
identified by the last item's time and id. The alternative, "skip the first 200", gets slower the
deeper you go and shows duplicates or gaps when new rows arrive in between. A cursor uses the
index and stays correct while the list grows. The incidents screen keeps its first page fresh by
polling and loads older pages on demand.

## How is it made usable for everyone?

- Real HTML controls: the behaviour switch is a group of radio buttons with a legend, tables have
  header cells, errors are announced (`role="alert"`), there is a skip link.
- Health is never colour alone: "● Up", "● Down", "● Checking".
- A browser test checks every screen at phone width for sideways scrolling.

## How is it tested?

- **Unit tests** (Vitest): the formatting helpers and two components.
- **Browser tests** (Playwright, `e2e/sandbox.spec.ts`) against a running Lighthouse: start a
  sandbox, break a site, wait for the incident to open by itself, post an update, fix the site,
  watch it close; the phone-width check; and the isolation check with two visitors.
- CI also type-checks, builds, and audits dependencies.

## Known gaps

- **The data shapes are written twice,** once as Go structs and once as TypeScript types, by hand.
  Nothing fails if they drift apart.
- **Polling still costs a database query each time,** and an open console keeps the database
  awake. Uptime figures are recomputed from raw check rows on each poll.
- **Deleting uses the browser's own confirm box,** and there is no undo.
- **Screens have no unit tests of their own.** The form, the error mapping, the polling back-off
  and two components do; the screens are covered by the browser tests, which run against a real
  server and take half a minute.
- **A release under an open tab** is handled by reloading once when a screen's file is gone. Work
  typed into a form at that moment would be lost.

## Questions and answers

**Why Vue for the console and plain templates for the rest?**
The public pages are read-mostly and must be fast and work without JavaScript, so the server
renders them. The console is interactive and constantly changing, which is what a single-page app
is good at.

**Why embed the console in the Go binary rather than host it separately?**
One thing to build, sign, deploy and roll back, and the console can never be a different version
from the API it calls. It also keeps everything on one address, so the session cookie and the
same-origin check simply work, with no cross-origin configuration.

**Where does the console keep the session token?**
Nowhere. It is in an `HttpOnly` cookie the browser sends automatically; JavaScript can't read it,
so an injected script can't steal it.

**The router hides the readers page from sandboxes. Is that the protection?**
No, it is tidiness. The server refuses the request and the database confines every query to the
caller's tenant. Anything enforced only in the browser isn't enforced.

**Why polling rather than WebSockets?**
The data changes every ten seconds at most, held-open connections would keep a scale-to-zero
container awake, and polling needs nothing shared between instances. Polling stops when the tab is
hidden.

**What happens on a new release to someone with the console open?**
Their loaded app keeps working against the API. On their next full load they get a fresh
`index.html` (never cached), which names the new hashed files.

**Would rooms or publish/subscribe make server push cheap?**
No. A room is the server's way of grouping connections; each browser still holds its own, and on
Cloud Run an open connection is billed as a request that never ends. Polling is billed for the
milliseconds each short request takes. Push would also need a shared channel once there is more
than one instance.

**Is one generic error message enough?**
It is only used for failures nobody anticipated, so internals never reach the browser; expected
failures (a bad value, a taken slug, not found, not allowed, too many requests) each have their
own status and message. What is missing is a request id to trace the generic one, and per-field
validation errors.

**Why cursor pagination?**
It stays fast at any depth and doesn't repeat or skip rows when new ones are added while someone
is paging.

**What do the browser tests cover?**
The sandbox from start to finish (break a site, watch the incident open and close), the phone
layout, isolation between two visitors, the owner adding, testing, editing and pausing an HTTP
monitor, an incident opened and resolved by hand, the readers report, the Security screen with a
sign-out everywhere else, a session that ends, and a sandbox reset. Ten tests, against a real
server and database.

**What is an optimistic update, and where is it used?**
Showing a change before the server confirms it, and undoing it on failure. It is used for the
behaviour switch, so the control responds instantly.
