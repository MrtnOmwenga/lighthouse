# GhostChat

End-to-end encrypted chat in the browser: the server stores and relays messages it can't read,
forge or quietly change. React, Express, Socket.IO, MongoDB, libsodium. About 2,100 lines on the
server and 3,900 in the browser.

Source: [github.com/MrtnOmwenga/GhostChat](https://github.com/MrtnOmwenga/GhostChat). File paths
on these pages are in that repository; its own `docs/DESIGN.md` is the full security design.

| # | Page | What it covers |
|---|---|---|
| 1 | [Accounts and keys](01-accounts-and-keys.md) | Why the server never receives the password, the key vault, the recovery phrase, signing in on a new device, the session |
| 2 | [Messages](02-messages.md) | How one message is encrypted and signed, the hash chain, two people sending at once, deletion, rooms and files in brief |
| 3 | [Identity as it stands](03-identity.md) | What an identity is, key rotation with pre-committed next keys, the transparency log and its Bitcoin anchor, safety numbers, and what a decentralised identity would still need |

Rooms, attachments, the real-time layer and the tests are covered briefly inside those pages, not
separately. What the review finds that should be built or fixed is in the
[build list](backlog.md).
