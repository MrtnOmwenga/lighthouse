---
title: "Live collaboration"
project: redacted
topics: [crdt, yjs, websockets, real-time, concurrency, race-conditions, listen-notify, authorization, fail-closed]
sources:
  - src/realtime/realtime.service.ts
  - src/realtime/access-changes.ts
  - src/realtime/projection.ts
  - src/briefings/access.ts
  - docs/COLLABORATION.md
  - test/realtime.e2e-spec.ts
  - test/marked-words.e2e-spec.ts
  - test/realtime-time.e2e-spec.ts
verified: 2026-10-09
---

# Live collaboration

Several people type in the same section at once, and everyone's screen stays the same. This page
is about how that works, and about the harder half: keeping permissions true on a connection that
stays open while permissions change.

## The words used on this page

- **WebSocket:** a connection between a browser and a server that stays open, so either side can
  send at any time.
- **CRDT (conflict-free replicated data type):** a way of storing shared data so that copies
  edited separately can always be merged, with every copy ending up the same.
- **Yjs:** the CRDT library used here. **Hocuspocus** is the server that speaks its protocol.
- **Room:** one shared thing that connections join, here one section's text.
- **Race condition:** a bug that depends on which of two things happens first.
- **Fail closed:** when in doubt, refuse.
- **`LISTEN`/`NOTIFY`:** PostgreSQL's built-in way for one connection to broadcast a message to
  others.

## How do two people type in the same place without overwriting each other?

There are no locks and no "last save wins". Every browser, and the server, holds a copy of the
section. Each applies its own edits at once and sends them to the others. Yjs guarantees that
copies which have seen the same edits hold the same text, **whatever order the edits arrived in.**

It can promise that because of how an edit is described:

- **Every character has a permanent identity:** which client typed it, and that client's count.
  It also records which characters were on its left and right when it was typed. A position is
  never "offset 42", which goes stale the moment someone else types.
- **Deleting marks a character as deleted** and keeps its place (a *tombstone*), so a later edit
  that refers to it still knows where it belongs.
- **Edits can be applied in any order, or twice,** with the same result.

| Situation | Result |
|---|---|
| Two people type at the same spot at once | Both survive, in the same order on every screen (decided by a fixed rule) |
| One types inside a word another deletes | The typed characters survive, anchored to their neighbours |
| Someone's connection drops | Their browser keeps the edits and sends what the server lacks when it returns |
| Undo | Undoes *your* changes, never a collaborator's |

The server merges edits like any other copy, relays them, and saves the section about a second
after typing pauses (at most five seconds apart), recording in the audit log who edited.

**What a CRDT doesn't promise is intent.** Two people who retype the same word at once both
succeed: "blue" changed to "red" and to "green" can become "redgreen". Every real-time editor
accepts that; people see it and fix it.

## Who may connect to what?

The browser sends its access token in the first message on the socket, not in a cookie, so no
other website can ride on it. The server verifies it, loads the member inside their organization's
transaction, and works out their access with **the same policy code the REST API uses**
(`loadDocumentAccess`), so a browser tab and a socket can never disagree.

| Their access to the section | The connection |
|---|---|
| None | Refused. Nothing is synced |
| Read | **Read-only on the server:** edits it sends are dropped. The locked editor in the page is a courtesy |
| Edit | Normal |

There are four kinds of room: a section's full text; a projection of it at one clearance level
([Part 5](05-redaction.md)), always read-only; a briefing room that carries no text and tells open
pages when to re-fetch; and a personal room per member, so someone with no access yet learns when
they are given some.

## What happens when permissions change while people are connected?

An HTTP request asks permission every time. A socket asked once, when it opened, and may stay open
for hours. So every change that can affect access (a role, a clearance, a share, a
classification, a deleted document, a disabled member) is **announced**:

1. The code making the change calls `AccessChanges.announce`, which sends a PostgreSQL `NOTIFY`
   inside the same transaction. The message is delivered only if the change commits.
2. Every server instance is listening, and re-checks each of its open connections for that
   organization.
3. Lost access: the browser is told and disconnected. Demoted: the connection becomes read-only
   and the editor locks. Promoted: it becomes writable.

## What was the race, and how was it closed?

The re-check runs after the change commits. So for a few milliseconds, between the commit and the
re-check, a demoted editor's connection was still writable. A test showed it: a client that sent
an edit the instant the demotion request returned had that edit accepted, every time.

The fix fails closed, then recovers:

1. **Lock before commit.** Before the change commits, every live section connection in that
   organization is made read-only on this server. From then on no edit is accepted from anyone
   until the re-check has decided.
2. **Re-check and restore.** After the commit, each connection gets its new access; those still
   allowed to edit become writable again.
3. **Recover what the lock refused.** A browser doesn't resend a refused edit by itself, so an
   innocent editor would silently lose what they typed during the lock. When a connection is
   restored the server starts a sync with it, and the browser sends everything the server is
   missing.
4. **If the change rolls back,** no notification comes, so a fallback re-check runs after three
   seconds and restores everyone.

Each part has a test that fails without it: an edit sent right after a demotion never lands, and
an edit refused during the lock is recovered.

## What if access ends and nobody changed anything?

Three things end access with no permission change to announce:

| What happens | How the open connection finds out |
|---|---|
| A temporary share runs out | A sweep every 15 seconds asks, per organization with connections open, whether a share's expiry has passed since the last sweep. If so, that organization is re-checked. |
| The token that opened the connection expires | The same sweep closes it. The client reconnects with a fresh token. |
| The member signs out, or their session is revoked as stolen | Access tokens carry the session they came from. Ending a session is announced, and every server closes that session's connections. The member's other devices stay connected. |

An open WebSocket keeps a serverless instance awake, so the sweep's timer runs whenever there is
a connection for it to check. That is why a timer is acceptable here and wasn't for clean-up.

## Does a change lock everyone?

It used to: any permission change made every section in the organization read-only until the
re-check finished. Now a change says how far it reaches:

- **one member** (a role, a clearance, a disabled account),
- **one document** (a share, a section's classification, a deletion),
- or the whole organization.

Only the connections inside that reach are locked and re-checked. The channels that only tell a
page to re-fetch are always told, because a member with no access yet may just have been given
some.

## What about classifying words while someone is reading them?

The check happens **before an edit is applied** (the `guard` hook). The server decodes the
incoming edit, applies it to a scratch copy, and looks at the highest mark the section would then
contain:

- **Above the sender's own clearance:** refused, and the sender's connection is closed. Nobody
  hides words they couldn't then read.
- **Above another connected member's clearance:** that member is told and disconnected *first*,
  and only then is the edit applied. Neither it, nor anything typed into the newly classified
  words, reaches them. Their page re-fetches the briefing and moves to the projection it now
  needs.

Here the order of two steps is the whole protection: disconnect, then apply.

## What happens with more than one server?

Each server keeps its own copy of an open section in memory. Permission changes always reached
every server, because they are announced through the database. **Edits did not**, and nobody knew
until a test started two servers and put one person on each: an edit accepted by one never
reached the reader on the other, and each server's save overwrote the other's.

Now an accepted edit is sent to the other servers through the same database channel (PostgreSQL
`NOTIFY`) and applied to their copies. No extra service is needed.

| Situation | What happens |
|---|---|
| An edit on one server | Sent as an update; the others apply it. Updates of this kind merge in any order, so nothing needs sequencing. |
| A server opens a section others are editing | The database lags unsaved typing, so the newcomer says what it has and whoever has more answers with the difference. |
| An update too big for a notification (8000 bytes) | The sender saves first and tells the others to read it. |
| Two servers save | Each merges what is stored into its copy before saving, so a save can only add. |
| Words classified on one server, a reader below them on another | The receiving server disconnects that reader before applying the update. |

Seven tests cover it. With the sending line removed, five of them fail, which is the evidence
that the tests are testing the sharing and not something else.

The live site still runs one server. This makes a second one safe; it doesn't add one.

## Known gaps

- **With several server instances, the lock isn't complete.** Only the instance that handled the
  change locks before commit; the others lock when the notification arrives, milliseconds later.
  In production the service has only ever run as one instance. Closing it fully means sending each
  organization's connections to one instance, or checking access on every incoming edit.
- **A demoted editor's last keystrokes stay on their own screen.** The server refused them, so
  nobody else sees them and they aren't saved, but their page shows them until it reloads.
- **Moving text between sections is copy and delete,** because sections are separate documents. A
  simultaneous edit to the moved text in its old place isn't carried along.

- **Long-lived sections keep their editing history.** Deleted text is discarded, but the record
  that something was deleted there stays. Rebuilding a section from scratch would break any client
  still holding the old history, so it isn't done.

## Questions and answers

**What is a CRDT, in a sentence?**
A way of storing shared data so that copies edited separately can always be merged and end up
identical, whatever order the edits arrive in.

**Why doesn't Yjs use positions like "character 42"?**
A number goes stale as soon as someone else types before it. An identity for each character,
with its neighbours, still means the same place after any other edit.

**Why is a reader's connection read-only on the server, when the editor in the page is already
locked?**
The page is the member's to modify. Only the server's refusal is a control; the locked editor is
there so honest users aren't confused.

**Permissions are checked when a socket opens. Why isn't that enough?**
The socket can stay open for hours while roles, clearances and shares change. Each change is
announced and every open connection is re-checked.

**Why is the announcement sent inside the transaction?**
So it is delivered only if the change commits. Sent before, a rolled-back change would still
disconnect people; sent after, a crash in between would leave connections unchecked.

**Describe the race you found.**
The re-check ran after the commit, leaving a few milliseconds in which a demoted editor could
still write. The fix locks every connection in the organization before the commit, restores the
ones still allowed afterwards, and then asks each restored browser for anything it typed during
the lock.

**Why is the token sent in a message and not a cookie?**
A cookie is attached automatically, including to a socket opened by another website's page. A
token the page must send itself can't be used that way.

**What happens to an edit that classifies words above another reader's clearance?**
That reader is disconnected before the edit is applied, so nothing from then on reaches them.

**A share expires while its guest has the document open. What happens?**
Within 15 seconds a sweep notices that a share's expiry has passed, re-checks that organization's
connections, and the guest's is closed. The member who shared it is untouched.
