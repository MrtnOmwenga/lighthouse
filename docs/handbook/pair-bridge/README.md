# Pairbridge

Share files between a Linux PC and an Android tablet over the local network, with no cloud
service and no cable. A Python server on the PC and a Kotlin app on the tablet, about 2,000
lines. **Unfinished:** it works for its main use, and its own roadmap lists what is missing,
including encryption of the traffic.

Source: [github.com/MrtnOmwenga/pair-bridge](https://github.com/MrtnOmwenga/pair-bridge).

| # | Page | What it covers |
|---|---|---|
| 1 | [How it works](01-how-it-works.md) | The PC as a storage location in every Android file picker, the rule that the tablet only connects outward and the investigation behind it, making a tap feel instant |
| 2 | [Security, and what is unfinished](02-security-and-status.md) | Shared folders only, the pairing token, why it isn't safe on an untrusted network yet, what is tested and what isn't |

What the review found that should be built or fixed is in the [build list](backlog.md).
