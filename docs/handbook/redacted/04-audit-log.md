---
title: "The audit log"
project: redacted
topics: [audit-log, hash-chain, tamper-evidence, append-only, advisory-locks, canonical-json, compliance]
sources:
  - src/audit/chain.ts
  - src/audit/chain.spec.ts
  - src/audit/audit.service.ts
  - src/audit/audit.controller.ts
  - src/common/crypto.ts
  - src/database/migrations.ts
  - test/audit.e2e-spec.ts
verified: 2026-10-08
---

# The audit log

Every change in an organization is written down: who did it, to what, when. This page is about
how that record is kept, and how far it can be trusted.

## The words used on this page

- **Audit log:** a record of who did what and when, kept so it can be checked later.
- **Append-only:** entries can be added, never changed or removed.
- **Hash:** a one-way fingerprint of a value; change the value at all and the fingerprint changes.
- **Hash chain:** a list where each entry's fingerprint also covers the previous entry's
  fingerprint, so entries can't be altered or reordered without it showing.
- **Tamper-evident:** changes can be *detected*. (Not the same as tamper-proof, which would mean
  they can't be made.)
- **Advisory lock:** a lock in PostgreSQL that isn't tied to a row or table; code agrees on a
  number and takes turns.

## What is recorded?

One event per change (`src/audit/audit.service.ts`): its number in the organization's sequence,
the time, who (a member, an API key, or nobody), the action (`document.update`), the kind of thing
and its id, and a small detail object.

Recorded: creating, changing and deleting departments, members, projects, documents and sections;
shares given and removed; API keys created and revoked; sign-ins, sign-outs, failed sign-ins,
lockouts, and a refresh token used twice.

## What makes it trustworthy?

Three things, each covering what the one before can't.

**1. It commits with the change it describes.** An event is written inside the request's
transaction ([Part 2](02-tenants-and-database.md)). The change and its record both happen or
neither does: there is never a document without its event, or an event for something that didn't
happen.

**2. The application can't alter it.** Its database role may insert and read audit events. It was
never granted update or delete. A bug, or an attacker who has taken over the application, can add
to the log and can't rewrite it.

**3. It is a hash chain.** That covers changes made *around* the application, by someone with
direct access to the database.

## How does the chain work?

Each event stores two hashes (`src/audit/chain.ts`):

```
hash = SHA-256( previous event's hash + this event's content )
```

The first event links to a starting value derived from the organization's id, so one
organization's chain can't be passed off as another's.

Change any stored event and its hash no longer matches its content. Recompute that hash to hide
the change, and the *next* event's "previous hash" no longer matches. Each event vouches for
everything before it.

`GET /audit-events/verify` walks the chain from the start and reports the first break, with the
reason: a missing number in the sequence, an event that doesn't link to the one before, or
content that doesn't match its hash.

**Why "canonical" JSON?** The content is hashed as text, and the same object can be written with
its keys in different orders. `canonicalJson` always writes keys sorted, so the same event always
produces the same hash, whatever order the database returns its fields in.

## How does it stay in order when requests arrive together?

Two requests in one organization, at the same moment, would both read "the last event is number
41" and both try to write number 42.

Before appending, `record` takes an advisory lock keyed on the organization
(`pg_advisory_xact_lock`). The second request waits until the first commits, then reads 42 as the
last and writes 43. The lock is released automatically when the transaction ends, and it is per
organization, so one tenant's writes never wait on another's.

## Who can read it?

Organization admins and auditors (`audit:read` in [Part 1](01-permissions.md)). The auditor role
exists for this: it can read everything in the organization and change nothing.

## How is it tested?

- **`chain.spec.ts`:** an untouched chain verifies; any single edit, deletion or swap is caught at
  the first affected event; a gap is caught even when every hash was recomputed; key order doesn't
  change a hash. The file is also mutation-tested ([Part 1](01-permissions.md)).
- **`audit.e2e-spec.ts`:** changes produce a chain that verifies; tampering through the database
  is pinpointed; a failed request leaves no event; concurrent changes keep one linear chain;
  auditors can read it and editors can't.

## Known gaps

- **Someone who can rewrite the whole chain can't be caught.** The chain detects a change to part
  of it. A database owner who alters an event and then recomputes every hash after it, or who
  simply deletes the newest events, leaves a chain that verifies. Nothing outside the database
  remembers what the latest hash was. The fix is an *anchor*: publish the latest hash somewhere
  the database owner can't change (a signed checkpoint, a write-once bucket, an email to the
  auditors) and compare against it.
- **Refused actions aren't recorded.** A member who tries to open what they may not gets a 403 and
  leaves no trace, because the refusal rolls back the transaction. Failed sign-ins are recorded
  (they are returned as values, see [Part 3](03-signing-in.md)); failed authorization isn't. For
  a security log, the attempts are often the interesting part.
- **Reading isn't recorded.** The log says who changed a classified section, never who read it.
- **Every audited write in an organization waits its turn.** The lock makes writes within one
  organization strictly one at a time.
- **Verifying reads the whole chain,** every time, into memory. There are no checkpoints.
- **The log can only be listed newest-first,** up to 500 events: no search by member, action or
  date, and no export.
- **The time is the application server's clock,** not the database's.
- **It is kept forever.** No retention period, and the detail object could hold personal data.

## Questions and answers

**What is the difference between tamper-evident and tamper-proof?**
Tamper-proof means a change can't be made. Tamper-evident means a change can be detected. A hash
chain is the second: it doesn't stop anyone with database access, it makes what they did show.

**The application can't update or delete audit rows. Why is the chain needed as well?**
The grants stop the application. The chain covers everything that isn't the application: a
database administrator, a restored backup, a script run by hand.

**What exactly does the chain not protect against?**
Someone who rewrites it consistently from some point to the end, or cuts off the end. Detecting
that needs a copy of the latest hash kept somewhere they can't reach.

**Why an advisory lock rather than locking the table?**
A table lock would make every organization wait for every other. The advisory lock is keyed on
the organization, so only that organization's writes queue, and it disappears with the
transaction.

**Why is the audit event written in the same transaction as the change?**
So the two can't disagree. Written separately, a crash between them leaves a change with no
record, or a record of something that was rolled back.

**What does that cost?**
A failed or refused request leaves no record, because its event is rolled back with it. Recording
attempts needs a second, separate write.

**Why hash "canonical" JSON?**
So the same event always hashes the same. Without a fixed key order, a harmless reordering of
fields would look like tampering.
