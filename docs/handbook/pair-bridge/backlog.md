# Build list from the review of Pairbridge

The project's own `ROADMAP.md` is the fuller plan; these are the items the review would put
first. Sizes: S (under an hour), M (a few hours), L (a day or more).

| # | What | Why | Size |
|---|---|---|---|
| P1 | **Encrypt the traffic,** with the PC's own certificate and its fingerprint carried in the pairing QR code. | Plain HTTP: anyone on the same WiFi who captures a request has the token and the shared folders. | M |
| P2 | **Tests for the tablet app:** the cache's key and replacement, the documents provider's handling of errors, pairing. | The PC side has 28 tests; the app has none. | M |
| P3 | **See and revoke a pairing from the PC,** and replace the token on request. | The token never changes unless pairing is redone. | S |
| P4 | **Notice a file changed on the PC without a new listing** (compare on open, or have the PC say what changed). | The tablet serves its cached copy until the folder is listed again. | M |
| P5 | **Trim the cache** by size and age. | It only grows. | S |
| P6 | **Decide the fate of the tablet's own server:** try it on another device, or remove it and its unmaintained dependency. | It is blocked on the one tablet it was tested on, and needs a broad permission. | S |
| P7 | **Publish the PC side as an installable package.** | It is installed from the repository. | S |
