# Build list from the review of Redacted

Things the part-by-part review finds that should be built or fixed. Nothing is built during the
review. Sizes: S (under an hour), M (a few hours), L (a day or more).

Carried over from the Lighthouse build:

| # | What | Why | Size |
|---|---|---|---|
| R1 | **Bring the release pipeline level with Lighthouse's:** release only after CI, verify the signature, test a candidate revision before it takes traffic, roll back on failure, pin actions to commit hashes. | It still releases on every push to the release branch, with none of those. | M |
| R2 | **Required status checks on the release branch.** | The branch requires a pull request, with no checks; some jobs only run conditionally, so the list needs choosing. | S |
| R3 | **Refuse requests that didn't come through the edge,** as Lighthouse does. | The service answers on its `*.run.app` address, bypassing Cloudflare and its client-address header, so its rate limits can be dodged there. | S |
| R4 | **Run on two instances, with a test proving an edit through one reaches a reader on the other.** | The code for sharing state between instances exists (PostgreSQL LISTEN/NOTIFY); it has never run on more than one. | L |

## From Part 1: the permission model

| # | What | Why | Size |
|---|---|---|---|
| R5 | **Pagination on list endpoints.** | Lists return the newest 100 rows and no way to ask for more. | S |
| R6 | **Make it impossible to write an endpoint that forgets to ask** (Martin's idea, 2026-10-08): each route declares its action (a decorator), and a test fails for any route that declares none. The check itself stays in the service, because it needs the resource loaded first. | `authorize` is a call each service method must remember. The generated matrix proves the endpoints it knows about; nothing catches a new route that was never added to it. | M |

## From Part 2: tenants and the database

| # | What | Why | Size |
|---|---|---|---|
| R7 | **A `down` for every migration, and a test that runs them all down and up again** (as Lighthouse has). | Migrations have only an `up`; a rollback would be written by hand, under pressure. | M |
| R8 | **Record an API key's last use at most once a minute.** | Every request from a key updates `last_used_at`, so read-only traffic still writes a row each time. | S |

## From Part 3: signing in

| # | What | Why | Size |
|---|---|---|---|
| R9 | **Stop sign-up revealing which emails are registered** (answer the same way and send the rest by email, or at least rate-limit and say less). | It answers 409 "That email is already registered", the enumeration sign-in avoids. Needs email to do properly. | M |
| R10 | **Remove used and expired refresh tokens** (a function the housekeeping calls, as demo clean-up does). | The table grows by one row per refresh and nothing deletes from it. | S |
| R11 | **Tolerate two tabs refreshing at once:** accept a just-used token for a few seconds and return the pair it already produced. | The second tab's refresh looks like theft, and the member is signed out. | M |

## From Part 4: the audit log

| # | What | Why | Size |
|---|---|---|---|
| R12 | **Anchor the chain outside the database:** publish each organization's latest sequence number and hash somewhere the database owner can't change (a signed checkpoint in a write-once bucket, or sent to the auditors), and have `verify` compare against it. | The chain detects a partial change. Rewriting it consistently to the end, or cutting off the newest events, verifies cleanly. | M |
| R13 | **Record refused actions** (a 403) in a separate transaction, so they survive the rollback. | A member probing for what they may not open leaves no trace. | M |
| R14 | **Record reads of classified sections.** | The log says who changed a classified section, never who read it, which is what an investigation would ask. | M |
| R15 | **Search, paging and export for the log** (by member, action, date). | It can only be listed newest-first, up to 500 events. | S |

## From Part 5: sections, clearances and redaction

| # | What | Why | Size |
|---|---|---|---|
| R16 | **Decide what a document's title and `body` are:** classify them like a section, or drop `body` now that content lives in sections. | Sections are redacted; the title and the original body field are shown to anyone who can open the document. | M |
| R17 | *(Optional)* **Compartments:** a need-to-know label on a section, held by named members, beside the clearance ladder. | Real classification isn't one ladder; "secret" doesn't mean every secret-cleared person. | L |
