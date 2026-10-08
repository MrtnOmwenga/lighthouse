---
title: "Security, and what is unfinished"
project: pair-bridge
topics: [path-traversal, symlinks, bearer-tokens, tls, certificate-pinning, android-keystore, project-status]
sources:
  - README.md
  - ROADMAP.md
  - laptop/pairbridge/server.py
  - laptop/pairbridge/config.py
  - laptop/tests/test_server.py
  - laptop/tests/test_cli.py
  - android/app/src/main/kotlin/dev/pairbridge/app/CredentialStore.kt
  - android/app/src/main/kotlin/dev/pairbridge/app/PairingActivity.kt
verified: 2026-10-09
---

# Security, and what is unfinished

A server on your PC that hands out your files has to be very clear about who may ask and for
what. This page is what Pairbridge gets right, and the large thing it doesn't do yet.

## The words used on this page

- **Path traversal:** asking for `../../something` to climb out of the folder you were given.
- **Symlink:** a file that is really a pointer to another file or folder.
- **Token:** a long random secret that proves a request comes from the paired tablet.
- **TLS:** the encryption that turns `http` into `https`.
- **Certificate pinning:** accepting one specific certificate and no other, instead of trusting
  any that a public authority has signed.

## What can the tablet reach?

**Only the folders that were chosen to be shared.** Every path in every request is worked out in
full and refused if it lands outside its shared folder (`resolve_path` in `server.py`).

Working it out *in full* is the point. Checking the text of a path for `..` isn't enough: a
symlink inside a shared folder can point anywhere. So the path is resolved the way the operating
system would, following links, and only then compared with the shared folder. A link that leads
out is refused.

The home folder as a whole can't be shared at all.

## How does the PC know a request is from the paired tablet?

A **pairing token**: 256 random bits, made on the PC and carried to the tablet in the QR code.

- It is compared in constant time, so the time a comparison takes reveals nothing about it.
- On the PC it is in a configuration file only its user can read.
- On the tablet it is encrypted with a key held by Android's keystore, which apps can use and
  can't extract.
- It is shown only when the pairing command is run.

## What isn't protected?

**Everything in transit.** The traffic is plain HTTP. Anyone on the same WiFi who captures one
request has the token, and with it can read and change the shared folders.

The README says so in its own security section, and says to use it only on networks you trust.

The planned fix avoids the usual difficulty of encrypting a connection to a device that has no
public name:

1. The PC makes its own certificate the first time it runs.
2. **The QR code carries that certificate's fingerprint** alongside the token.
3. The tablet accepts only a certificate with that fingerprint.

No public authority is needed, and there is no "trust this certificate?" prompt to click
through. The QR code is already a private channel between the two devices, so it can carry the
one fact the tablet needs.

## How far is it tested?

- **The PC side: 28 tests,** covering the token, climbing out of a shared folder by path and by
  symlink, writes that complete or leave the original untouched, upload size limits, names that
  never collide, thumbnails, the permissions on the configuration file, and the command line.
- **The tablet app: none.** It is built on every change and has been used by hand.

## What is the state of the project?

Usable for its main purpose, and not finished. Its roadmap, written before each piece of work so
the design is settled first, lists:

| Next | Later |
|---|---|
| Encrypting the traffic, as above | Editing a file in place |
| A clipboard hand-off between the devices | Showing the tablet's notifications on the PC |
| Publishing the PC side as an installable package | Deciding whether to fix or remove the tablet's own server |

It was built for one person's two devices, and tested on one tablet.

## Known gaps

- **Plain HTTP,** as above. This is the one that matters.
- **No tests on the tablet app.**
- **The token never changes** unless the pairing is removed and redone. There is no way to see
  or revoke a pairing from the PC.
- **One tablet model.** Others may behave differently, in both directions: the block on inbound
  connections may not exist elsewhere.
- **The experimental path depends on a library that is no longer maintained,** and needs a broad
  permission on the tablet.
- **Not installable from an app store or the usual package index.**

## Questions and answers

**How do you stop a request reading files outside the shared folder?**
Resolve the requested path completely, following symlinks, and refuse it unless the result is
inside the shared folder. Checking the text for `..` would miss a symlink that points out.

**What is wrong with plain HTTP on a home network?**
Anyone else on that network can read the traffic, including the token, and then do anything the
tablet can.

**How do you encrypt a connection to a device with no public name?**
Let it make its own certificate and give the other device that certificate's fingerprint through
a channel you already trust. Here that channel is the pairing QR code.

**Why store the token in Android's keystore?**
The app can ask the keystore to use the key and can't read it out, so a copy of the app's files
doesn't yield the token.

**Is this project finished?**
No. It does what it was built for on the devices it was built for. The traffic isn't encrypted
yet, the tablet app has no tests, and its roadmap says what is next.
