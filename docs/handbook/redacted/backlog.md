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
