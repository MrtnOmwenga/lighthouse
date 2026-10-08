# Build list from the review of GhostChat

Things the review finds that should be built or fixed. Nothing is built during the review.
Sizes: S (under an hour), M (a few hours), L (a day or more).

Carried over from the Lighthouse build:

| # | What | Why | Size |
|---|---|---|---|
| G1 | **Bring the release pipeline level with Lighthouse's:** release only after CI, verify the signature, test a candidate revision before it takes traffic, roll back on failure, pin actions to commit hashes. | It still releases on every push to the release branch. | M |
| G2 | **Required status checks on the release branch.** | The branch requires a pull request, with no checks. | S |
| G3 | **Refuse requests that didn't come through the edge,** as Lighthouse does. | The service answers on its `*.run.app` address, bypassing Cloudflare and its client-address header, so its rate limits can be dodged there. | S |
| G4 | **Run on two instances, with a test proving a message sent through one reaches a reader on the other.** | The Redis adapter for sharing between instances exists; it has never run on more than one. | L |

## From Part 1: accounts and keys

| # | What | Why | Size |
|---|---|---|---|
| G5 | **Make signing out end the session on the server** (a session id that can be revoked, or a short token with a revocable refresh). | The session is a signed token in a cookie; signing out only deletes the cookie, and a copied token works until it expires. | M |
