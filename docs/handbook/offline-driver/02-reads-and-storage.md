---
title: "Reading while offline, and local storage"
project: offline-driver
topics: [offline-first, caching, sqlite, expo-sqlite, concurrency, property-based-testing, react-native]
sources:
  - src/core/query.ts
  - src/sqlite/index.ts
  - src/react-native/index.ts
  - test/query.test.ts
  - test/sqlite.test.ts
  - test/queue.test.ts
verified: 2026-10-09
---

# Reading while offline, and local storage

The other half of working offline: showing something when there is no connection, and keeping
that something on the device safely.

## The words used on this page

- **Cache:** a local copy of data that came from the server.
- **Read-through:** ask the network, save what comes back, and fall back to the saved copy when
  the network can't be reached.
- **SQLite:** a small database that lives in a file on the device.
- **Synchronous / asynchronous:** synchronous code runs to the end before anything else does;
  asynchronous code can pause and let other code run in between.
- **Property test:** a test of a rule over many random inputs, not a few chosen examples.

## How does a read work without a connection?

`offlineQuery` in `query.ts` wraps a normal fetch:

1. **Known to be offline:** don't try the network at all; return the saved copy.
2. **Otherwise** fetch, save the result, return it.
3. **If the fetch fails for lack of a connection:** return the saved copy, quietly.
4. **If it fails for any other reason:** raise the error as usual.

Step 1 exists because of timeouts. A request from a disconnected phone fails fast; a request to a
server that is merely unreachable fails only after the full timeout, with a spinner on screen the
whole time, before showing the very same saved copy.

Step 4 matters as much: only "no connection" is treated as normal. A real error isn't hidden
behind stale data.

## How are pages of results combined?

`mergeBy`: items just fetched replace saved ones with the same key; saved items that weren't in
this page are **kept** (it was one page, not the whole list); and the result is sorted newest
first. The caller says which field means "newest", because an order's and an invoice's are
different, and guessing gets one wrong.

A property test checks it over random inputs: every key appears once, the fetched copy wins, and
merging the same page twice changes nothing.

## Why is the SQLite layer synchronous?

`src/sqlite/index.ts` uses only the database's synchronous calls, on purpose. An asynchronous
version was written first and removed.

Every pause in asynchronous code hands control back to the app, and another part of the app can
then make its own database call in the gap. On the web, both go to one shared worker, where they
interleaved and corrupted each other's results. Synchronous code can't be interrupted, so the
problem can't occur. The cost is that a large write blocks the screen until it finishes; the
answer is to split it into several smaller ones.

Three more things the layer does, each from something that went wrong in use:

- **A database that fails to open is closed.** Left half-open, it keeps its lock on the file and
  the retry races it.
- **Signing out deletes the databases, including ones this session never opened.** On a shared
  device the next person mustn't inherit the last one's data.
- **On the web, the database worker is started at launch,** so the first real query doesn't pay
  for starting it inside a wait that times out on a slow machine.

## How is it tested?

61 tests, with no device needed: the database calls are passed in, so tests supply a desktop
SQLite behind the same interface, and the queue runs on an in-memory store. One property test
fires many queue changes at once and checks none is lost, which is what the queue's internal lock
is for (the storage has no transactions, so two quick taps would otherwise overwrite each other).

## Known gaps

- **Things deleted on the server never disappear from the device.** Merging keeps every saved
  item that a fetched page didn't mention, and has no way to learn that one is gone.
- **The reader isn't told how old the copy is.** Offline data is returned exactly like fresh
  data, with no "as of".
- **Synchronous writes block the screen** for as long as they take.
- **The cache grows without limit.** Nothing expires or trims it.
- **It is built for one database library** (expo-sqlite's interface), though that is passed in
  rather than imported.

## Questions and answers

**Why not try the network when the device says it is offline?**
The request can only fail, and when a server is unreachable rather than the device disconnected,
it fails only after the full timeout, with a spinner showing the whole time.

**Which errors serve the saved copy, and which don't?**
Only a missing connection. Anything else is a real error, and showing old data in its place would
hide it.

**Why was the asynchronous database layer removed?**
Its pauses let other database calls run in between, and on the web they corrupted each other's
results. Synchronous calls can't be interleaved.

**What is the cost of synchronous database calls?**
They block the screen until they finish, so large writes must be split up.

**Why does the queue need a lock if JavaScript runs one thing at a time?**
Reading the queue and writing it back are two separate waits. Two quick taps can both read the
same list, and the second write then drops the first's addition.
