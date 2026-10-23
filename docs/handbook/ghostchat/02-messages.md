---
title: "Messages"
project: ghostchat
topics: [end-to-end-encryption, aead, digital-signatures, hash-chain, envelope-encryption, concurrency, forward-secrecy, rooms]
sources:
  - docs/DESIGN.md
  - frontend/src/lib/messaging.js
  - frontend/src/lib/crypto/envelope.js
  - frontend/src/lib/crypto/files.js
  - backend/src/services/chain.js
  - backend/src/routes/messages.js
  - backend/src/routes/rooms.js
  - backend/src/realtime.js
verified: 2026-10-08
---

# Messages

The server stores every message and delivers it, and must not be able to read one, write one,
change one, or rearrange them. This page is about how a message is built so that all four are
true.

## The words used on this page

- **Encrypt:** make unreadable without a key. **Sign:** attach proof of who wrote something, which
  anyone can check and nobody else can produce.
- **Symmetric key:** one key that both locks and unlocks; fast, used for the message itself.
- **Sealing:** locking a small secret with someone's public key so only their private key opens
  it.
- **Envelope:** the whole stored message: the locked text plus everything needed to check it.
- **Hash chain:** each item carries the fingerprint of the one before
  ([Redacted's audit log](../redacted/04-audit-log.md) uses the same idea).
- **Forward secrecy:** a key stolen today can't unlock yesterday's messages.

## How is one message protected?

Two separate jobs, done with two different kinds of key
(`frontend/src/lib/crypto/envelope.js`).

**Keeping it secret.** The browser makes a fresh random key for this one message and encrypts the
text with it. Then it *seals* that key twice: once to the recipient's public key, and once to the
sender's own, so the sender can read their sent messages on another device. The server stores the
locked text and the two sealed keys, and can open none of them.

**Proving who wrote it.** The browser hashes the whole envelope and signs the hash with the
sender's private signing key. Anyone holding the sender's public key can check it; nobody
without the private key can produce it. The server never has a private key, so it can't write a
message as anyone.

Two details close gaps that those leave:

- **The conversation's id is bound into the encryption.** A locked message copied into a different
  conversation fails to decrypt, so the server can't move messages between chats.
- **The server checks the signature before storing,** and every recipient checks it again. The
  server's check is a courtesy; the recipient's is the control.

## What stops the server rearranging the history?

Each conversation is one hash chain. Every envelope carries its number in the conversation and
the hash of the envelope before it, and **the signature covers both.** So the server can't
reorder messages, insert one, or remove one from the middle: the links would no longer match, and
it can't re-sign them. The browser checks each message as it arrives and shows a shield beside
it; a "Verify" panel shows the hashes and the signature.

## What happens when two people send at the same moment?

Both built their message on the same "previous" message, and only one can be next.

The server accepts the first and answers the second with a **conflict**, including the message it
missed. The second sender's browser then, with no action from the person: applies the message
that won, re-links its own message onto it, signs it again, and sends it again after a short
random wait. The message shows as "sending" until it lands. Both people end up with the same
order.

This is the opposite choice from Redacted's editor, where simultaneous edits merge without
anyone waiting ([its page](../redacted/06-live-collaboration.md)). A signed chain needs one agreed
order, so one sender has to go again; shared text doesn't, so nobody does.

## How can a message be deleted from a chain?

Removing it would break the links. So it is replaced by a **tombstone**: the number, the links and
the hash stay, with a deletion statement signed by the author; the locked text, the sealed keys
and any attached file are erased. The chain still verifies, the content is gone, and the
conversation shows "Message deleted" instead of a silent gap.

## How do rooms differ?

A room has one shared key, and everyone in it encrypts with that.

- **Joining** is by an invite link whose secret comes after the `#`. Browsers never send that
  part to a server. The secret unlocks the room's keys, which is how a new member reads the
  earlier history.
- **Leaving** forces a new key. The server refuses new messages for the room until the key has
  been replaced; the next member to send makes one, seals it to each remaining member, and then
  sends. Someone who has left can't read anything new.

Files work the same way as messages: encrypted in the browser with their own key, stored by the
hash of the encrypted bytes, and the key travels inside the encrypted message. Photos are
re-encoded in the browser first, which strips hidden details such as where they were taken.

## Known gaps

- **No forward secrecy.** A message key is sealed to a long-lived public key. If that private key
  ever leaks, every past message sealed to it can be opened. Replacing keys limits the damage to
  a period; Signal's method (the Double Ratchet), and its equivalent for groups (MLS), remove it
  by changing keys with every message. This is the largest gap.
- **The server sees who talks to whom, when, and how much.** Only the content is hidden.
- **The newest messages can be withheld.** A chain shows a change in the middle; it can't show
  that the server simply stopped delivering. A browser notices a gap in what it has seen, and a
  new device has nothing to compare with.
- **A signature proves authorship to anyone, for ever.** A recipient can show a signed message to
  a third party as proof of who wrote it. Signal deliberately avoids that; GhostChat chooses
  verifiability.
- **What someone has already read, they keep.** Deleting erases the server's copy, and leaving a
  room stops new messages, but neither reaches into another person's browser.
- **One message at a time per conversation.** Under heavy simultaneous sending, senders queue
  behind each other and retry.

## Questions and answers

**What is the difference between encrypting and signing?**
Encrypting hides the content from everyone without the key. Signing hides nothing: it proves who
wrote it and that it hasn't changed.

**Why a new key for every message, sealed to each person, instead of encrypting with their
public key directly?**
Public-key encryption is slow and suited to small things; a symmetric key is fast for the message
itself. Sealing only the small key also lets one message go to several readers.

**Why is the message also sealed to the sender?**
So they can read what they sent from another device. Otherwise only the recipient could ever
open it.

**What can a compromised server do to a conversation?**
See who is talking and when, refuse to deliver, or stop delivering new messages. It can't read,
forge, alter or reorder them without the browsers noticing.

**Two people send at once. What happens?**
One is accepted. The other's browser is told what it missed, re-links and re-signs its message,
and sends again by itself.

**How do you delete a message without breaking the chain?**
Replace its content with a signed deletion statement, keeping its number and hashes.

**What is forward secrecy, and does GhostChat have it?**
That a key stolen today can't open yesterday's messages. It doesn't: keys are long-lived. It is
the first thing a production messenger would add.
