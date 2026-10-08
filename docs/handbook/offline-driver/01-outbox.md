---
title: "Writing while offline: the outbox"
project: offline-driver
topics: [offline-first, outbox-pattern, retries, dead-letter-queue, error-classification, idempotency, react-native]
sources:
  - src/core/types.ts
  - src/core/queue.ts
  - src/core/driver.ts
  - src/core/errors.ts
  - src/core/status.ts
  - src/react/index.ts
  - test/driver.test.ts
  - test/queue.test.ts
verified: 2026-10-09
---

# Writing while offline: the outbox

Someone places an order in a warehouse with no signal. The app must accept it, keep it safe, send
it when the connection returns, and be honest about which of those has happened.

## The words used on this page

- **Outbox:** a list of things waiting to be sent, kept on the device.
- **Action:** one queued change: "create this order", "update that quantity".
- **Handler:** the app's code that sends one kind of action to the server.
- **Drain:** one pass through the outbox, sending what can be sent.
- **Dead-letter:** to set an action aside as failed for good, instead of retrying it for ever.
- **Idempotent:** safe to do twice; the second time changes nothing.
- **Optimistic update:** showing a change as done before the server has confirmed it.

## What are the pieces?

- **The queue** (`queue.ts`) only stores: it keeps the list on the device, counts attempts, and
  says when it changed. It knows nothing about what any action means.
- **The driver** (`driver.ts`) is the engine: it takes each waiting action to its handler and
  settles it by how the attempt went. It also knows nothing about what an action means.
- **Handlers** are the app's: one per kind of action, registered with the driver.

Because the first two are ignorant of the third, an app adds a new kind of action by writing a
handler and touching nothing else.

## How can an attempt end?

Four ways, and the difference between them is the core of the library (`types.ts`):

| Outcome | Means | What happens |
|---|---|---|
| `success` | Done | The action leaves the queue |
| `network` | The request never reached a server | **Not counted as an attempt.** The drain stops, and carries on at the next reconnect |
| `failure` | The server refused, but might not next time (a 5xx) | Counted. After three, the action is dead-lettered |
| `terminal` | The server refused, and always will (a validation error) | Dead-lettered **at once** |

Two rules follow, and both are easy to get wrong:

- **A dropped connection must not count as an attempt.** Otherwise an order dies because the
  train went through three tunnels.
- **A rejection that can't change must not be retried.** Otherwise the user waits through every
  retry for an answer that was never in doubt, while the screen says "syncing".

## How does it tell a dropped connection from a rejection?

`isNetworkError` in `errors.ts`, which recognises the ways fetch, React Native and Axios each
report "no answer": by error name, by code (`ECONNRESET`, `ETIMEDOUT`…), and by message. It also
looks one level inside the error, because client libraries often wrap the original.

One case is called out in the code: a connection **reset part-way through** (`ECONNRESET`), which
a firewall can cause. It looks like a server's answer and isn't one. Missed, it gets an action
dead-lettered that should have been quietly retried.

For rejections, most 4xx statuses are final. Four aren't: 401 and 403 (a refreshed sign-in may fix
them), 408 (a timeout) and 429 (slow down).

## When is the outbox sent?

On three occasions (`start` in `driver.ts`):

- **When the connection returns.**
- **When something new is queued,** after half a second of quiet. The pause lets ten quick taps on
  "+1" be merged into one queued update before anything is sent.
- **When the app returns to the foreground.**

A drain sends actions **oldest first, one at a time**, because order matters: an edit must not
reach the server before the create it edits. Only one drain runs at a time.

One detail prevents a nasty loop. The drain itself writes to the queue (it removes what
succeeded, counts what failed). If those writes triggered another drain, a failing action would
use up all its retries within a second or two. So only *new work* triggers a drain; the queue
says what kind of change happened.

## What happens to an action that fails for good?

It stays in the queue, marked stuck, and the app is told (`onStuck`), so that whatever it showed
optimistically ("order queued") can be corrected. The user can retry stuck actions or discard
them. Once per session they get one more chance automatically, in case an update fixed what was
wrong.

## What is the user told?

A small store (`status.ts`) turns the queue and the connection into one of five states for a
status bar: hidden, offline, syncing, error, success.

Three decisions keep it from lying or flickering:

- **"Syncing" shows only after a real offline period.** Writes go through the outbox when online
  too, and a save that takes 200 ms shouldn't flash a banner.
- **It waits for the drain to say it is finished,** not for the queue to be empty, which happens a
  moment earlier and would flicker.
- **Every screen reads the same store,** so a screen opened mid-sync shows "syncing" at once.

## Known gaps

- **A request can be sent twice.** If the server did the work and the *answer* was lost, the
  attempt looks like a dropped connection and is retried. Whether that creates a second order is
  left to the app: its handler has to send something the server can use to spot the repeat (the
  action's id is there for that). The library doesn't do it, or say so loudly.
- **Actions don't know about each other.** If "create order" fails for good, the queued "edit
  that order" is still sent, and fails too.
- **The whole queue is rewritten on every change,** as one piece of text under one key. Fine for
  tens of actions; it isn't built for thousands.
- **Retries aren't timed.** An action that failed is tried again at the next reconnect, new
  action or return to the app, not after a growing delay.
- **Telling a dropped connection from a rejection is partly by message text,** which libraries can
  change.
- **The queue isn't tied to a user.** The app must clear it on sign-out, or one person's waiting
  changes are sent under the next person's sign-in.
- **No handling of conflicts.** If the record changed on the server while the device was offline,
  the queued change is simply sent.
- **Not published to the package registry,** so it is installed from the repository.

## Questions and answers

**What is the outbox pattern?**
Record what you intend to send, durably, before trying to send it; then send from that record
until each item is settled. Nothing depends on the connection being up at the moment of the tap.

**Why must a network failure not count as an attempt?**
The server never saw the request, so nothing is known about whether it would succeed. Counting it
would let a flaky connection dead-letter a perfectly good action.

**Why dead-letter a validation error at once?**
The same payload will be refused the same way. Retrying only delays telling the user.

**Why one action at a time, in order?**
Later actions can depend on earlier ones. Sending them together, or out of order, lets an edit
arrive before the thing it edits exists.

**What is the risk in retrying after a lost response?**
The server may already have done it. Unless the request carries an id the server can recognise,
the retry does it again.

**Why does only new work trigger a drain?**
The drain's own bookkeeping changes the queue. If that started another drain, a failing action
would burn its retries in a second.
