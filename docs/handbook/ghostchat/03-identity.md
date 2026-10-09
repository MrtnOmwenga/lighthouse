---
title: "Identity as it stands"
project: ghostchat
topics: [identity, did-key, key-rotation, pre-rotation, key-transparency, merkle-tree, certificate-transparency, opentimestamps, safety-numbers]
sources:
  - docs/DESIGN.md
  - frontend/src/lib/crypto/keyHistory.js
  - frontend/src/lib/keys.js
  - frontend/src/lib/log.js
  - frontend/src/lib/crypto/merkle.js
  - backend/src/services/log.js
  - backend/src/services/merkle.js
  - backend/src/services/anchoring.js
  - backend/src/services/ots.js
  - test-vectors/
  - frontend/src/lib/crypto/pins.js
  - backend/src/edge.js
verified: 2026-10-09
---

# Identity as it stands

Nobody in GhostChat gives a real name. The question this page answers is narrower than "who are
you?": **is this the same person I was talking to yesterday, and is the key I'm encrypting to
really theirs?**

End-to-end encryption depends on that. If the server could hand you its own key in place of your
friend's, it could read everything while both of you saw a padlock.

## The words used on this page

- **Identity key:** the public key that *is* a user, as far as the system is concerned.
- **DID (decentralised identifier):** a standard way to write an identity that no company issues.
  `did:key` is the simplest kind: the identifier is the public key itself.
- **Rotation:** replacing a key with a new one.
- **Merkle tree:** a tree of hashes over a list, whose single top hash (the *root*) changes if
  anything in the list changes.
- **Transparency log:** a public, append-only list that anyone can check hasn't been rewritten.
- **Safety number:** a number two people compare, outside the app, to confirm each other's keys.

## What is an identity here?

A user's first signing key, written as a `did:key`. The username is only a display name tied to
it. Nothing about the person is in it, and nobody issued it: whoever holds the private key is that
identity.

## What happens when someone changes their key?

Every change is an entry in that user's **key history**, a signed chain like the message chain in
[Part 2](02-messages.md). Each entry holds the new keys and is **signed by the previous key**, so
a contact can follow the links: the person I verified signed this new key, which signed the next.

There are two kinds of change:

- **Rotation:** version *n+1*, signed by version *n*. Contacts see "keys rotated: signed by their
  previous key ✓".
- **Reset** (everything lost): a new entry the old key did *not* sign. Contacts see a warning,
  because nothing but the server's word connects the new key to the old one.

## What stops a thief who steals the current key from rotating to their own?

**Pre-rotation.** Every entry also contains a commitment: the hash of the *next* signing key. That
next key is calculated from the recovery phrase and is never stored in the vault
([Part 1](01-accounts-and-keys.md)).

So a rotation is only valid if the new key matches the hash promised in advance. Someone who
steals today's key can sign with it, and still can't rotate to a key of their own, because their
key doesn't match the commitment. Rotating asks for the recovery phrase.

## What stops the server lying about someone's key?

A key history protects the links between one user's keys. It doesn't stop the server showing
Alice one history for Bob and showing Carol a different one.

So every entry also goes into one server-wide **transparency log**: a Merkle tree, the design
used to keep website certificates honest.

- The server signs each **tree head** (how many entries, and the root hash).
- When a browser fetches someone's keys, it also checks an **inclusion proof**: a short list of
  hashes showing that this entry really is in the tree with that root.
- It checks a **consistency proof** against the last tree head it saw: the new tree contains the
  old one unchanged, with entries only added.
- **Browsers attach their latest tree head to messages,** so two people who chat also compare
  what they were each shown. A server giving different people different logs is caught the moment
  they talk.

## Why is Bitcoin involved?

One remaining trick: the server could rewrite the whole log and re-sign it.

Once a day the log's root hash is **timestamped on the Bitcoin blockchain** through
OpenTimestamps, a free service that needs no coins. That gives proof, held by a system nobody
controls, that this exact root existed on that day. A rewritten log can't match an anchor made
before the rewrite.

This is the same problem, and the same answer, as the missing piece in Redacted's audit log
([its page](../redacted/04-audit-log.md)): a chain can be rewritten by whoever holds all of it,
unless a copy of its head is kept somewhere they can't reach. GhostChat has that anchor; Redacted
doesn't yet.

## And if all of that is somehow wrong?

**Safety numbers.** For any contact there is a 60-digit number and a QR code calculated from both
people's identity keys. Compare them in person or on a call, and no server is involved in the
check at all. Marking a contact verified pins their key in the browser; any later change that the
pinned key didn't authorise raises a warning.

## What is not here?

What exists is identity *inside GhostChat*: stable, verifiable, and anonymous. What doesn't exist
is the larger idea the `did:key` format was chosen to leave room for:

- **Using the identity anywhere else.** There is no way to sign in to another site with it.
- **Proving anything about yourself.** There are no credentials (a verified age, membership, a
  qualification) and no way to show one selectively.
- **A profile to share,** or any way to find someone except by their username.
- **An identity that isn't tied to one server.** The username, the key history and the log all
  live on GhostChat's server. The format is decentralised; the storage isn't.

## Do verified marks follow you to another device?

Yes, since 2026-10-09. The marks are kept on the server as **one encrypted blob per account**.

- **The key** comes from the oldest encryption key in the vault. Every device that has unlocked
  the vault can derive it, it survives key rotations (the vault keeps every key), and it needs no
  password prompt.
- **The server can't read the marks or invent one:** the encryption is authenticated, with the
  account's identity bound in so a blob can't be moved to another account.
- **Two devices can't drop each other's change:** a write names the version it was based on. The
  second gets a refusal, re-reads, re-applies its one change and tries again.

What the server can still do is serve an *older* copy. That can remove a recent mark or bring
back a removed one. It can never create one.

## What drives the daily anchoring now?

A timer inside a server that has scaled to zero doesn't fire. The server now has a path that runs
one anchoring round (`POST /internal/anchor`), which only the edge in front of it may call: the
edge never forwards a visitor's request to `/internal/`, and the server refuses anything that
doesn't carry the edge's secret. The edge's hourly scheduled call is written and waits on one
account setting; until then the in-process timer still runs whenever the server is awake.

## Does it work on more than one server?

Yes, and that is now tested: two servers sharing only the database and Redis, with people on
different ones. A message crosses, a session started on one opens a socket on the other, signing
out through one closes the socket held by the other, presence is one count across both, and
joining a room through one subscribes a socket on the other. The existing code passed unchanged.

## Known gaps

- **Trust on first use.** A browser accepts the log's signing key the first time it sees it, and a
  contact's key the first time it sees one. Everything after that is checked against the first.
  Only comparing safety numbers checks the first.
- **The phrase is the whole identity.** Lose it and the next rotation is impossible; leak it and
  the identity can be taken over, pre-rotation notwithstanding.
- **Nothing deletes from the log.** Deleting an account erases messages and private keys; the
  username and public keys stay, because the log is append-only by design.
- **The log is recomputed for every request.** Fine for a small deployment.
- **Safety numbers are a simplified construction** (one hash, where Signal's is iterated).

## Questions and answers

**What attack does all this prevent?**
The server (or whoever controls it) substituting its own key for someone's, and reading the
conversation while both people see a padlock.

**What is a `did:key`?**
An identifier that is simply a public key in a standard format. Nobody issues it and nothing has
to be looked up.

**What does pre-rotation add over signing each new key with the old one?**
Signing with the old key means whoever steals the old key can rotate. Committing in advance to
the hash of the next key, which lives only in the recovery phrase, means a thief with today's key
still can't.

**What is the difference between a rotation and a reset?**
A rotation is signed by the previous key, so contacts can follow it. A reset isn't, so contacts
are warned and should compare safety numbers again.

**What does the transparency log add to the key history?**
The history links one person's keys together. The log makes sure everyone is shown the same
history: a server can't give two people different keys for the same user without it showing.

**Why timestamp the log on Bitcoin?**
So that rewriting the entire log and re-signing it is detectable. An anchor made earlier, held by
a system the server doesn't control, won't match.

**What are safety numbers for, given all the rest?**
They are the one check that involves no server at all, and the only one that covers the very
first time you see someone's key.

**Is this a decentralised identity?**
The identifier is. The system isn't: the names, histories and log are all on one server, and the
identity can't be used anywhere else.
