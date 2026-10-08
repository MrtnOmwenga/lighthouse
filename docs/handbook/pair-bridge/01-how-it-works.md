---
title: "How it works"
project: pair-bridge
topics: [android, storage-access-framework, documents-provider, fastapi, mdns, local-network, debugging, caching, streaming]
sources:
  - README.md
  - ROADMAP.md
  - docs/hyperos-wifi-investigation.md
  - laptop/pairbridge/server.py
  - laptop/pairbridge/cli.py
  - laptop/pairbridge/mount.py
  - android/app/src/main/kotlin/dev/pairbridge/app/LaptopDocumentsProvider.kt
  - android/app/src/main/kotlin/dev/pairbridge/app/DocumentCache.kt
  - android/app/src/main/kotlin/dev/pairbridge/app/LaptopDiscovery.kt
  - android/app/src/main/kotlin/dev/pairbridge/app/ShareActivity.kt
verified: 2026-10-09
---

# How it works

## The words used on this page

- **File picker:** the screen an Android app shows when you attach or open a file.
- **Storage location:** an entry in that picker, such as "Google Drive" or "Downloads".
- **Documents provider:** the piece of an Android app that adds a storage location to every
  picker.
- **Share sheet:** the list of targets an app shows when you tap "Share".
- **mDNS:** a way for devices on one network to find each other by name, without a server.
- **Inbound / outbound connection:** one that a device receives, or one that it starts.

## What does it do?

- **The PC's folders appear on the tablet** as a storage location in every app's file picker,
  the way Google Drive does. Browse, open, attach, save, rename, delete.
- **"Send to laptop"** in any app's share sheet drops files into a folder on the PC.
- **Pairing is one QR code.** If the PC's address changes, the tablet finds it again by name.

## What are the two halves?

| On the PC | On the tablet |
|---|---|
| A small web server (FastAPI) that lists, sends and receives files from the folders chosen to be shared | A documents provider that turns picker actions into requests to that server |
| A command (`pairbridge`) that installs it as a background service, shows the pairing code and manages the shared folders | A share-sheet target that uploads to the PC |
| An announcement of itself on the network (mDNS) | A search for that announcement when the saved address stops working |

The important design choice on the tablet is to plug into Android's own system for storage
(`LaptopDocumentsProvider.kt`) instead of building a file browser. That is why it works inside
Slack, WhatsApp and every other app with no effort from them: they already ask Android for a
file, and Android now offers the PC.

## Why does the tablet only ever connect outward?

The first design had a small server on the tablet too, so the PC could browse the tablet's
files. On the test tablet it simply didn't work: connections to it hung, with no error anywhere.

The investigation is written down (`docs/hyperos-wifi-investigation.md`) so it isn't repeated:

- Over the USB cable the tablet's server answered at once, so the server was fine.
- Six explanations were tested and ruled out, one by one: app permissions, battery limits, the
  kind of address it listened on, Android's local-network permission, restrictions on apps
  installed by hand, a firewall in between.
- The decisive test: a listener started from the debugging shell on the same tablet **was**
  reachable over WiFi. So the network was fine, and the block is how that manufacturer's version
  of Android treats an ordinary app that listens.

It was never solved. The design was changed to go around it: **every feature that ships uses
only connections the tablet starts.** Sending a file to the PC is an upload from the tablet, not
the PC fetching it.

That is a reasonable way to end an investigation that doesn't reach a cause: establish exactly
where the boundary is, write it down, and design so it doesn't matter.

## Why does opening a file feel instant?

A tablet's WiFi radio goes to sleep after a few seconds without traffic, and waking it costs one
to three seconds whatever the size of the file. That was measured by matching the timestamps in
the server's log and the app's log.

So when a folder is listed, files under 10 MB are downloaded in the background into a cache
(`DocumentCache.kt`), keyed by path, size and modification time. A tap is then served from the
tablet's own storage.

Three rules keep that from causing trouble:

- **Background downloads run one at a time,** and thumbnails three at a time, so the file a
  person actually taps isn't competing with them.
- **Thumbnails are made on the PC,** so scrolling a folder of photos transfers kilobytes, not the
  photos.
- **Nothing is held in memory.** Files flow straight through between the app and the network, so
  a large one never sits in memory or in a temporary copy, and a failure part-way is reported
  rather than delivering half a file as if it were whole.

On the PC, a file being written goes to a temporary name and is renamed into place only when it
is complete, so a dropped connection never leaves a damaged file.

## Known gaps

- **A file changed on the PC is noticed only when its folder is listed again.** Until then the
  tablet serves its cached copy.
- **Apps that edit a file in place can't save back.** Saving a new or replaced file works.
- **Browsing the tablet from the PC is experimental and blocked** on the tablet it was built for.
  The code is still there.
- **The PC side assumes one family of Linux** for installing itself as a service.
- **The cache isn't trimmed.**

## Questions and answers

**Why a documents provider and not a file-browser app?**
Every Android app already asks the system for files. Becoming one of the system's storage
locations makes the PC available inside all of them, with nothing for those apps to do.

**How did you find out why the tablet's server couldn't be reached?**
By ruling causes out one at a time and writing each down, until one test separated the network
from the device: a listener run from the debugging shell was reachable, an ordinary app's wasn't.
The cause inside that version of Android was never found.

**What did you do when you couldn't fix it?**
Changed the design so it didn't matter: the tablet starts every connection. And kept the
investigation in the repository.

**Why preload files?**
Waking the WiFi radio costs seconds on every first request. Fetching small files while the
folder is on screen means a tap never waits for that.

**How do you avoid a half-written file on the PC?**
Write to a temporary name beside it and rename when complete. A rename either happens or doesn't.
