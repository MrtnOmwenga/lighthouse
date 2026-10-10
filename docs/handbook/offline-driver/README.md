# offline-driver

A small TypeScript library that lets a React Native app keep working without a connection: writes
are queued and sent later, reads fall back to a local copy. About 1,100 lines, 61 tests. It was
generalised from the offline layer of a production app.

Source: [github.com/MrtnOmwenga/offline-driver](https://github.com/MrtnOmwenga/offline-driver).

| # | Page | What it covers |
|---|---|---|
| 1 | [Writing while offline: the outbox](01-outbox.md) | The queue, the four outcomes of an attempt, telling a dropped connection from a rejection, when the queue is sent, what the user is told |
| 2 | [Reading while offline, and local storage](02-reads-and-storage.md) | Reading through a cache, merging pages, why the SQLite layer is synchronous, how it is tested |

What the review found that should be built or fixed is in the [build list](backlog.md).
