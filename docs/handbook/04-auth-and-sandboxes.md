---
title: "Auth and sandboxes"
project: lighthouse
topics: [authentication, sessions, cookies, oauth, github, csrf, sandbox, rate-limiting, security-headers]
sources:
  - internal/auth/auth.go
  - internal/store/tenants.go
  - internal/web/server.go
  - internal/web/validate.go
  - internal/web/limiter.go
  - internal/config/config.go
  - internal/web/security.go
  - internal/store/migrations/00004_security.sql
  - deploy/cloudrun/alerts.tf
  - internal/web/web_test.go
verified: 2026-10-07
---

# Auth and sandboxes

The database keeps tenants apart perfectly, but it trusts whatever tenant id it is given
(see [data model and tenant isolation](03-data-and-tenants.md)). This page is about where that id
comes from: how Lighthouse decides who is making a request.

## The words used on this page

- **Authentication:** working out who someone is.
- **Authorization:** deciding what they may do.
- **Session:** the server's memory that a browser has signed in, lasting until it expires.
- **Cookie:** a small value the browser stores for a site and sends back with every request to it.
- **OAuth:** a way to let another service (here GitHub) confirm who someone is, without Lighthouse
  ever seeing their password.
- **CSRF (cross-site request forgery):** another website making your browser send a request to
  this one, using your cookie, without you meaning to.
- **Hash:** a one-way fingerprint of a value: easy to compute, impractical to reverse.

## Who can use Lighthouse, and as what?

Exactly two kinds of identity (`internal/auth/auth.go`):

| | Owner | Sandbox |
|---|---|---|
| Who | One GitHub account, named in the configuration by its numeric id | Anyone, with no account |
| How they get in | Sign in with GitHub | Click "start a sandbox" |
| Their tenant | The single owner tenant: the real monitors and incidents | A new tenant created for them, with three pretend monitors |
| Lasts | 12 hours | 2 hours, then the tenant and everything in it is deleted |
| May | Everything | Use the full console on their own data, with limits (below) |

There are no accounts, passwords or sign-up in Lighthouse itself.

## What is a session, concretely?

1. Lighthouse generates 32 random bytes from the operating system's secure random source: the
   **token**.
2. It stores the token's **SHA-256 hash** in the `sessions` table, with the tenant, the role and
   an expiry time.
3. It sends the token itself to the browser as a cookie named `__Host-lh_session`.

On every later request, the middleware reads the cookie, hashes it, and looks the hash up
(`lighthouse_session`, one of the four cross-tenant functions). If a session exists and hasn't
expired, the request carries that identity: tenant, role, login. Otherwise the request is
anonymous. From then on, every database transaction for the request is scoped to that tenant.

**Why store only the hash?** If the sessions table ever leaked (a backup, a bug), the attacker
would have hashes, not tokens, and a hash can't be sent as a cookie. Because the token is long and
random, the hash can't be guessed backwards either. (Passwords need slower, salted hashes because
people choose guessable ones; random tokens don't.)

**Why a server-side session rather than a signed token (JWT)?** Think of two ways to run a
cloakroom. With a **session**, you get a numbered ticket and the attendant keeps a list: to cancel
a ticket, they cross it off. With a **signed token**, you get a stamped pass that says "valid
until midnight" and nobody keeps a list: anyone can check the stamp without asking, which is fast
and works across many doors, but a pass can't be cancelled before midnight unless a list of
cancelled passes is kept, which is a list again.

Lighthouse is one service that already has a database, and it needs to cancel: signing out must
really end a session, and a sandbox's session must vanish when its tenant is deleted. So it keeps
the list. Signed tokens earn their keep when many separate services need to check identity without
all asking one database.

## Can a session be renewed?

No. A session has a fixed lifetime from the moment it starts (12 hours for the owner, 2 for a
sandbox) and nothing extends it; there are no refresh tokens. The owner signs in again, which is
one click through GitHub. For sandboxes the fixed lifetime is deliberate: it is what guarantees
their data is deleted.

(Refresh tokens belong to the signed-token design: a short-lived pass plus a longer-lived token
that can be exchanged for a new pass, and revoked. The RBAC-API project uses rotating refresh
tokens. The session equivalent is a *sliding* expiry, extended on each use.)

## How is the cookie protected?

Three flags and a name, set where the cookie is created:

| | Effect | Stops |
|---|---|---|
| `HttpOnly` | JavaScript on the page can't read the cookie | A script injected into the page stealing the session |
| `Secure` | Sent only over HTTPS | Someone on the network reading it |
| `SameSite=Lax` | Not sent on requests another site triggers (except a plain top-level link) | Most cross-site request forgery |
| The `__Host-` prefix on its name | The browser accepts the cookie only from this exact host, over HTTPS, for the whole site | A page on a sibling address planting a session |

The last one matters because the demos live on sibling addresses of the same parent domain
(`redacted.martinomwenga.com`). Any page there may set a cookie for the whole domain, including
one called `lh_session`. If a demo were compromised, it could plant an attacker's sandbox session
in the owner's browser. A browser refuses a `__Host-` cookie that wasn't set by the exact host,
and Lighthouse reads only the prefixed name, so a planted cookie is ignored
(`TestSessionCookieIsHostBound`). Over plain HTTP, in local development, the bare name is used,
because browsers refuse the prefix there.

## What stops another website acting as me?

Your browser attaches the cookie to any request to Lighthouse, including one triggered by a
different site you happen to have open. Two things refuse such requests:

1. `SameSite=Lax` on the cookie (above).
2. The **same-origin check** (`sameOrigin` in `internal/web/server.go`): any request that changes
   something (not a read) must come from Lighthouse's own address, judged by the `Origin` header
   the browser adds and can't be made to lie about. Anything else gets "forbidden".

The second matters here in particular: the two demos live on sibling addresses
(`redacted.martinomwenga.com`, `ghostchat.martinomwenga.com`), which browsers count as the *same
site* for cookie purposes. The origin check compares the full address, so a request from a demo's
page is refused like one from a stranger's.

## How does the owner sign in?

With GitHub, using OAuth (`GitHubStart` and `GitHubCallback`):

1. Lighthouse sends the browser to GitHub with a random **state** value, and also saves that value
   in a short-lived cookie.
2. The person signs in at GitHub (password, two-factor: all GitHub's concern) and GitHub sends the
   browser back with a one-time **code** and the same state.
3. Lighthouse checks the state matches the cookie. This proves the sign-in that is finishing was
   started by this browser, so nobody can trick the owner's browser into completing a sign-in the
   attacker started.
4. Lighthouse exchanges the code for a GitHub token (server to server, using the client secret),
   asks GitHub "who is this?", and compares the account's **numeric id** with the configured
   owner id.
5. A match starts an owner session. Anyone else is told only the owner can sign in, and the
   attempt is recorded.

Details worth knowing:

- **The numeric id, not the username.** A username can be changed, and a released one can be
  registered by someone else. The id never changes.
- **No permissions are requested** from GitHub (no scopes), and GitHub's token is thrown away once
  the id is known. Lighthouse can't do anything to the owner's GitHub account.
- **Production refuses to start** without the client id, client secret and owner id, or with the
  development login enabled (`internal/config/config.go`); the development login is checked again
  in the handler.

## What is recorded about sign-ins?

Every owner sign-in, every refusal (a GitHub account that isn't the owner's), every sign-out and
every time sessions are ended goes into `auth_events`, in the owner's tenant. The application can
add to that table and read it, nothing else. A refusal records the GitHub username and account
number; no network address is kept, and the privacy page says so.

Each is also written to the log as one structured line (`msg: security`, with an `event`), along
with things that aren't sign-ins: a rejected scheduler call, a request that bypassed the edge, a
rate limit being hit, a sandbox being created. An alert in Google Cloud matches three of them
(a refused sign-in, a failed sign-in, a rejected scheduler call) and emails the owner
(`deploy/cloudrun/alerts.tf`). The rest are logged without alerting: scanners produce them all
day.

## Can the owner see and end his sessions?

Yes, on the console's Security screen. It lists his live sessions (what kind of device, when it
signed in, when it ends) and the record of sign-ins, and can end one session or every session
except the one in hand. Tokens and their hashes never leave the server: sessions have an id for
this purpose.

## What happens when a visitor starts a sandbox?

`Sandbox` in `auth.go`, in one transaction (`CreateSandbox` in `internal/store/tenants.go`):

1. Generate a new tenant id.
2. Set it as the transaction's tenant, then insert the tenant row. The row-level-security rule
   allows it because the new tenant's id equals the current tenant: a tenant can only create
   itself.
3. Add three pretend monitors: Storefront (up), Checkout API (flaky), Search (slow), each checked
   every 10 seconds and opening an incident after 2 failures.
4. Start a session for that tenant, role `sandbox`, lasting two hours.

The visitor now has the same console the owner uses, on their own data.

A browser that already has a sandbox gets that one back when it asks to start another, so nothing
is abandoned. "Start over" in the console puts a sandbox back to its three sample monitors without
changing how long it has left.

## What keeps sandboxes from being abused?

They are open to anyone on the internet, so each avenue is bounded:

| Limit | Where |
|---|---|
| Only simulated monitors: a sandbox can't make Lighthouse send a request anywhere | `normalize` in `validate.go` |
| At most 10 monitors, checked no more often than every 10 seconds | `validate.go`, `createMonitor` |
| 6 new sandboxes an hour per visitor, counted in the database under a keyed hash of the address | `security.go`, `lighthouse_rate_hit` |
| Writes limited to 5 a second per address | `limiter.go` |
| Everything deleted two hours after creation (the session expires, then the clean-up removes the tenant and all its rows) | `lighthouse_prune` |
| Never sends email, never appears on the public status page (that shows the owner's tenant only) | `internal/alert`, `internal/status` |
| Can't see or touch the owner's data or another sandbox's | Row-level security |

## What else protects the pages?

Every response carries security headers (`securityHeaders` in `server.go`). The main one is a strict
**Content-Security-Policy**: the browser may load scripts, styles, fonts and images only from
Lighthouse itself, may not run inline scripts, and may not embed the site in a frame. If an
attacker managed to inject markup into a page, the browser would refuse to run it. The others stop
content-type guessing, limit what is sent in the referrer, and (over HTTPS) tell browsers to
always use HTTPS.

## Known gaps

- **Two roles, checked in the handlers.** Owner or sandbox, with `id.Owner()` tests where they
  differ. Enough here; more roles would need a real permission model (which is what the RBAC-API
  project is).
- **A sandbox visitor is recognised by the session cookie only.** Someone who clears cookies, or
  uses another browser, starts again. The six-an-hour limit is per network address, which several
  people can share and one person can change.
- **The sandbox limit's identity changes at midnight UTC,** with the daily secret it is hashed
  under, so the count starts again then.
- **Every request with a cookie costs a database lookup,** which also wakes a sleeping database.
- **Sessions can't be extended.** The owner signs in again after 12 hours.
- **Security alerts are email, at most one an hour,** and depend on Google Cloud's alerting. There
  is no digest of the quieter events.
- **Sandbox clean-up runs with the round,** at most hourly, so an expired sandbox's rows linger up
  to an hour after its session ends.

## Questions and answers

**What is the difference between authentication and authorization here?**
Authentication is the session lookup: which tenant and role does this cookie belong to?
Authorization is two layers: the handlers check the role where owner and sandbox differ, and the
database confines every query to the session's tenant.

**If someone steals the sessions table, can they sign in?**
No. It holds SHA-256 hashes of the tokens, and the cookie must contain the token itself.

**Why GitHub, rather than a password for the owner?**
There is exactly one user. Delegating to GitHub means no password storage, no reset flow, and
GitHub's two-factor authentication for free. Lighthouse only needs GitHub to answer "which account
is this?".

**What is the `state` value for?**
It ties the end of a sign-in to the browser that started it. Without it, an attacker could start a
sign-in with their own account and get the victim's browser to finish it.

**Someone signs in with a different GitHub account. What happens?**
GitHub signs them in fine; Lighthouse sees an id that isn't the owner's, starts no session, and
shows a page saying only the owner can sign in.

**Could a sandbox visitor use Lighthouse to attack another site?**
No. Sandbox monitors are simulated: their results are invented, and no request leaves Lighthouse.

**Why does a sandbox get the real console rather than a canned demo?**
It is the real product on isolated data, so what a visitor tries is what actually runs, and the
isolation itself is part of what's being demonstrated.
