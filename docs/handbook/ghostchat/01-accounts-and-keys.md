---
title: "Accounts and keys"
project: ghostchat
topics: [end-to-end-encryption, key-derivation, argon2, key-vault, recovery-phrase, bip39, zero-knowledge, sessions]
sources:
  - docs/DESIGN.md
  - frontend/src/features/auth/accountFlows.js
  - frontend/src/lib/keystore.js
  - frontend/src/lib/keys.js
  - backend/src/routes/auth.js
  - backend/src/auth.js
verified: 2026-10-08
---

# Accounts and keys

In an end-to-end encrypted app the server must not be able to read messages. That starts at
sign-up: if the server ever holds your password, or keys made from it, it can read everything.
This page is about how an account is created and used without that happening.

## The words used on this page

- **End-to-end encryption:** only the people in a conversation can read it; the server in the
  middle carries text it can't decrypt.
- **Private key / public key:** a matched pair. The public one is shared; the private one never
  leaves its owner. What one locks, only the other opens.
- **Key derivation:** turning a password or a seed into a key, by a fixed calculation.
- **Vault:** a user's private keys, encrypted, kept on the server.
- **Recovery phrase:** 24 ordinary words that encode a long random number.
- **Salt:** a random value mixed in so that two users with the same password get different
  results.

## Why can't the browser just send the password?

Because the encryption keys depend on it. A server that receives the password can derive
everything the user can.

So the browser splits the password before anything is sent
(`splitPassword`, used in `accountFlows.js`):

```
master    = Argon2id(password, salt)     64 bytes, computed in the browser
authKey   = first half     -> sent to the server, which uses it to sign you in
vaultKey  = second half    -> never leaves the browser
```

The two halves can't be worked out from each other. The server gets a value that proves you know
the password and is useless for decrypting anything.

The server stores a bcrypt hash of `authKey`, so a leaked database doesn't even hold something
that can be replayed to sign in.

## Where do the private keys live?

In the **vault**: the private keys, encrypted in the browser with `vaultKey`, and stored on the
server as a blob it can't open.

Signing in on a new device is then: enter the username and password → the browser derives both
halves → `authKey` signs in → the browser downloads the vault and opens it locally with
`vaultKey`. History and identity follow the user to any device, and the server never held a
usable key.

On a device, the unlocked keys are kept in memory, with a copy in the browser's storage encrypted
under a key the browser will use but won't reveal (a "non-extractable" key), so a page reload
doesn't ask for the password again and the raw keys aren't on disk in the clear
(`keystore.js`).

## What is the recovery phrase for?

A 24-word phrase is generated at sign-up, and the user must re-enter three of its words before
the account is created.

**Every key the account will ever use is calculated from it:** version 1, version 2, and so on.
That gives three things:

- **A forgotten password isn't the end.** The phrase regenerates the keys; signing a one-time
  challenge from the server with them proves ownership; a new password and vault are set.
- **Keys can be replaced** (rotated) in a way contacts can verify, covered in
  [Part 3](03-identity.md).
- **Nothing depends on the server remembering anything secret.**

If the password and the phrase are both lost, nobody can recover the history, the server
included. That is the design working, not failing.

## Why is a strong password enforced?

The vault is on the server, so whoever steals the database can try passwords against it at their
leisure. Two defences:

- **Argon2id with 64 MB of memory and three passes,** which makes every guess expensive.
- **A minimum strength** (a zxcvbn score of 3, "safely unguessable") at sign-up and on a password
  change.

The strength check can only run in the browser, because the server never sees the password. A
modified client could skip it, which would weaken only that user's own vault.

## What keeps sign-in from revealing who has an account?

Signing in starts by asking the server for the user's salt. For a username that doesn't exist,
the server returns a fake salt calculated from the username, the same every time, so the answer
can't be used to test which usernames exist. A wrong `authKey` and an unknown user take the same
time, by comparing against a dummy hash.

## What is the session?

After sign-in the server sets a cookie holding a signed token (a JWT), marked `HttpOnly` (page
scripts can't read it), `Secure`, and `SameSite=Strict` (other sites can't cause it to be sent,
which is why no separate anti-forgery token is needed).

## Known gaps

- **The server delivers the code that does the encryption.** A malicious or compromised server
  could send JavaScript that leaks keys. This is the standing weakness of end-to-end encryption on
  the web. A strict content-security policy limits what the page can load; closing it properly
  needs a signed, packaged client, or a browser extension that checks the code's hash.
- **Signing out doesn't end the session on the server.** The session is a signed token; signing
  out deletes the cookie, and a copy of the token works until it expires.
- **The phrase is everything.** Anyone who obtains it can regenerate every key the account will
  ever have. Replacing keys doesn't help, because the replacements come from the same phrase;
  only a reset to a new phrase does, and contacts are warned when that happens.
- **A weak password that slipped past the check exposes the vault** to anyone holding the
  database, at the cost of guessing.
- **One set of keys per account.** Every device shares them through the vault, so one device
  can't be revoked without replacing the keys for all.
- **No forward secrecy.** If an encryption key leaks, past messages encrypted to it can be read.

## Questions and answers

**What does the server know about a user?**
A username, a bcrypt hash of half an Argon2id output, a salt, an encrypted blob it can't open,
and public keys.

**Why split the password in two?**
One half proves you know the password; the other encrypts your keys. The server needs the first
and must never have the second, and neither reveals the other.

**How does a new device get your keys without the server having them?**
It downloads the encrypted vault and opens it with a key derived from the password, in the
browser.

**What happens if you forget your password?**
The recovery phrase regenerates your keys. You prove ownership by signing a challenge, then set a
new password, which makes a new vault.

**And if you lose the phrase too?**
The history is unrecoverable, by anyone. You can reset to new keys, and your contacts are warned
that nothing links the new key to the old one.

**Why Argon2id in the browser and bcrypt on the server?**
Argon2id turns a guessable password into a strong key, and must be expensive. What the server
receives is already 256 random-looking bits; bcrypt there only ensures a leaked database holds
nothing that can be replayed.

**What is the biggest weakness of end-to-end encryption in a browser?**
The server supplies the code. The user has to trust that today's JavaScript is the honest one.
