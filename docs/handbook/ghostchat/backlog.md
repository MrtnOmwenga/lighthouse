# Build list from the review of GhostChat

**State on 2026-10-09:** 8 of 12 built (G6 the first half), one decided against (G8), and forward secrecy (G7) and two later-version ideas deferred, with reasons at the end.

Things the review finds that should be built or fixed. Nothing is built during the review.
Sizes: S (under an hour), M (a few hours), L (a day or more).

Carried over from the Lighthouse build:

| # | What | Why | Size | State |
|---|---|---|---|---|
| G1 | **Bring the release pipeline level with Lighthouse's:** release only after CI, verify the signature, test a candidate revision before it takes traffic, roll back on failure, pin actions to commit hashes. | It still releases on every push to the release branch. | M | **Built** (2026-10-09) |
| G2 | **Required status checks on the release branch.** | The branch requires a pull request, with no checks. | S | **Built**: five required checks |
| G3 | **Refuse requests that didn't come through the edge,** as Lighthouse does. | The service answers on its `*.run.app` address, bypassing Cloudflare and its client-address header, so its rate limits can be dodged there. | S | **Built**; each service has its own secret |
| G4 | **Run on two instances, with a test proving a message sent through one reaches a reader on the other.** | The Redis adapter for sharing between instances exists; it has never run on more than one. | L | **Built**: five tests with two instances sharing MongoDB and Redis. The existing code needed no change |

## From Part 1: accounts and keys

| # | What | Why | Size | State |
|---|---|---|---|---|
| G5 | **Make signing out end the session on the server** (a session id that can be revoked, or a short token with a revocable refresh). | The session is a signed token in a cookie; signing out only deletes the cookie, and a copied token works until it expires. | M | **Built**: a session is a row; signing out deletes it and closes its sockets |
| G6 | **Code transparency for the web client** (Martin's question, 2026-10-08: how to stop the server sending code that leaks keys). Build the bundle reproducibly in CI; sign its hash keylessly (the GitHub identity, as the images already are); publish the hash in GhostChat's own transparency log; and give users a way to check that what their browser received is what was published: a small browser extension, or serving the client from a content-addressed address so the code can't change without the address changing. | The server delivers the code that does the encryption, so a compromised server can send code that leaks keys. Nothing a page loads from that server can check the server. The check has to come from outside it. | L | **Built**, first half: a reproducible build whose digest is signed into Sigstore's public log and checked on the transparency page. The browser extension is deferred |

## From Part 2: messages

| # | What | Why | Size | State |
|---|---|---|---|---|
| G7 | **Forward secrecy:** the Double Ratchet for direct messages, MLS for rooms. | Message keys are sealed to long-lived keys, so a leaked private key opens every past message sealed to it. The largest gap, and the design document's own "next". | L | **Deferred** (2026-10-09) |
| G8 | **Let a new device detect withheld messages:** each participant signs "I have seen this conversation up to number N", and devices compare. | A chain shows a change in the middle, not that the server stopped delivering the newest messages. | M | **Decided against** (2026-10-09) |

## From Part 3: identity

| # | What | Why | Size | State |
|---|---|---|---|---|
| G9 | **Anchor the log from an outside clock,** not the server's own timer (a scheduled call, as Lighthouse's tick). | The daily timestamp runs on a timer that doesn't fire while the Cloud Run instance is idle. | S | **Built**: `POST /internal/anchor`. The edge's hourly call is written and waits on one account setting (a workers.dev subdomain) |
| G10 | **Carry "verified" marks across devices** (keep them in the vault). | Which contacts were verified is stored in one browser. | S | **Built**: an encrypted blob on the server, keyed from the vault |

## Ideas for a later version (Martin, 2026-10-08)

| # | What | Why | Size | State |
|---|---|---|---|---|
| G11 | **Deniable authorship with a zero-knowledge proof:** show the recipient that a legitimate participant wrote a message, without a signature they can show a third party. | A signature proves authorship to anyone, for ever. | L | Later version |
| G12 | **Hide the sender from the server:** a proof of "I am a member of this room" without saying which member. | The server sees who sends every message; encryption can't hide that. | L | Later version |

## Deferred or decided against on 2026-10-09, and why

| # | Decision | Why |
|---|---|---|
| G7 (forward secrecy) | Deferred | The Double Ratchet for direct messages and MLS for rooms are weeks of careful cryptography, and both assume keys per device, where GhostChat shares one set of keys through the vault. It belongs with the decentralised identity project, where the device model is being rethought anyway. It remains the largest gap in the design. |
| G8 (detecting withheld messages) | Decided against | Signed "seen up to N" markers catch a rollback (a database restored from an old backup) or a server that withholds messages and not the markers. A server that withholds consistently, markers included, is not caught, and that is the attacker the feature would claim to cover. Closing it properly needs devices to compare notes over a channel the server doesn't control. |
| G6, second half (a browser extension that checks the bundle automatically) | Deferred | A project of its own: a separately distributed, separately trusted piece of software. The first half makes the check possible by hand. |
| G11, G12 (zero-knowledge ideas) | Later version | As recorded above. |
