---
title: "Sections, clearances and redaction"
project: redacted
topics: [redaction, classification, clearance, data-minimisation, crdt, yjs, projections, information-leakage]
sources:
  - src/policy/policy.ts
  - src/briefings/briefings.service.ts
  - src/briefings/access.ts
  - src/realtime/projection.ts
  - src/realtime/projection.spec.ts
  - docs/COLLABORATION.md
  - test/briefings.e2e-spec.ts
  - test/marked-words.e2e-spec.ts
verified: 2026-10-08
---

# Sections, clearances and redaction

The feature the project is named for: several people read the same document, and each sees only
what they are cleared for. Sections, and words inside a sentence, black out for the others.

The claim that matters is stronger than "hidden": **text a reader isn't cleared for is never sent
to their browser.** The black bars are not a disguise over text that arrived anyway.

## The words used on this page

- **Clearance:** how much a member is trusted to see: unclassified, confidential, secret, or top
  secret.
- **Classification:** how sensitive a piece of text is, on the same four levels.
- **Redaction:** replacing text a reader may not see with a blank or a bar.
- **Section:** one titled part of a document, with its own classification.
- **Mark:** a classification applied to selected words inside a section, the way bold is applied.
- **Projection:** a copy of a section made by the server for readers at one clearance level, with
  the words above that level replaced by bars.
- **Yjs:** the library that lets several people edit the same text at once
  ([Part 6](06-live-collaboration.md)).

## What decides whether a member sees a piece of text?

Two independent questions, both of which must be yes (`src/policy/policy.ts`):

1. **May they open the document at all?** From their role and its reach
   ([Part 1](01-permissions.md)), or from a share.
2. **Is their clearance at least the text's classification?**

A department admin with no clearance can open every document in their department and reads only
the unclassified parts. A top-secret-cleared member of another department sees nothing until the
document is shared with them. Neither axis implies the other.

## Why is a section a separate document?

A briefing is an ordered list of sections, and **each section is its own collaborative document
on the server.** A member who isn't cleared for a section is never connected to it, so its text
never leaves the server.

The obvious alternative is one document for the whole briefing, with the server leaving out what
a reader may not see. It doesn't work with collaborative editing. In Yjs every character has an
identity, and every edit is described relative to its neighbours ("insert after character X"). A
reader whose copy is missing some characters can't apply an edit that refers to them. Either the
copies drift apart, or the hidden characters' positions have to be sent anyway, which leaks their
structure and often their content.

So hiding can't be done by filtering a shared document. It has to be built into the document's
**structure**: the smallest thing that can safely be hidden is a whole document.

What a reader without clearance receives for a section (`briefings.service.ts`): its position, its
classification level, and a length rounded up to a multiple of 40 characters. No heading, no text.

## How are words inside a sentence hidden?

"NIGHTJAR is the ████████" needs something finer than a section. Three designs were considered
(`docs/COLLABORATION.md`):

| Design | How | Why not, or why |
|---|---|---|
| Each classified span is its own tiny document | The sentence holds a placeholder pointing to it | Safe, and cleared members could edit spans in place; but a nested editor per span, and cursors, selection, undo and copy-paste across span boundaries, are heavy and fragile |
| The server makes a redacted copy per clearance level | Readers below the top get their level's copy | Safe and simple; readers of a copy can't edit that section |
| Everyone gets everything, encrypted per level | Cleared browsers decrypt | The server no longer protects anything: it becomes a key-management problem, reveals exact lengths, and revoking access means re-encrypting |

What was built is called **mark to classify, project to read**:

- **Classifying words is like formatting.** A cleared editor selects words and applies a mark with
  a level, the way they would make text bold. The mark is stored on the text itself.
- **The full text goes only to members cleared for every mark in the section.**
- **Everyone else gets a projection** (`src/realtime/projection.ts`): a separate document the
  server writes itself, with the same paragraphs, where each run of words above the reader's level
  is replaced by a bar. It is rebuilt a tenth of a second after the full text changes. Readers
  can't write to it.

A projection is built by copying what the reader *may* see. Nothing hidden is copied and then
removed, so there is no step at which a mistake could leave it in. A test checks that no
projection ever contains a single character of a run above its level.

## What stops someone hiding what they can't see?

Rules in the policy file (`canClassify`, `canMark`, `canSetClearance`):

- **Classifying needs clearance for both the old and the new level.** Nobody can hide text they
  couldn't then read, and nobody can declassify what they can't read.
- **Marking words needs clearance for the mark's level,** checked by the server on every edit
  before it is applied ([Part 6](06-live-collaboration.md)), not only offered by the editor.
- **Only organization admins set clearances:** never their own, and never above their own.

## What does a redaction still reveal?

Something. A redaction that revealed nothing would also hide that anything was redacted, and the
page would lose its shape. What leaks, deliberately:

| Revealed | Why it is allowed |
|---|---|
| That a hidden section exists, and where | So the document keeps its shape and a reader knows something is withheld |
| Its classification level | The same as a paper document's "TOP SECRET" stamp on a blacked-out page |
| Its length, rounded up to 40 characters | Roughly how much is hidden, never exactly |
| Where in a sentence words are hidden, and their length rounded up to 6 characters | As above; adjacent hidden runs merge into one bar, so the number of hidden words isn't shown |

## Can a member find out why they can see something?

Yes: `explain` answers "why can I see this?" with every reason (the role and its reach, each share
and when it expires), their clearance, and which sections it hides or partly hides. An admin can
ask on someone else's behalf. Access rules that can't be explained to the people they apply to
tend not to be trusted, or checked.

## Known gaps

- **A member who can't see every word of a section can't edit that section.** They read a copy,
  and a copy can't take edits back. They can still edit sections fully within their clearance.
  This is the deliberate trade for never sending hidden text.
- **What was already sent can't be unsent.** Classify a paragraph that someone has open, and they
  are disconnected before anything more reaches them, but they have seen what was there.
- **Clearance is one ladder.** Real classification also has compartments ("secret, and only for
  people on this operation"). There is no need-to-know beyond the document's own access rules.
- **Lengths and positions leak,** as the table says, by design.
- **Each section keeps up to three extra copies,** one per clearance level in use, each rebuilt on
  every change.
- **A projection lags the full text by a tenth of a second.** It is late, never wrong: it only
  ever holds what its readers may see.

- **A document's title has no classification.** It is the label people find the document by,
  like the cover of a file, and anyone who can open the document reads it. Until 2026-10-09 a
  document also had a `body` field outside its sections, readable the same way; that field is
  gone, so all of a document's text is now in sections, where it is classified.

## Questions and answers

**Why not send everything and hide it in the browser?**
Anything sent can be read: in the browser's developer tools, by a script, in a proxy. Hiding in
the browser is presentation, not protection.

**Why can't the server filter one shared document per reader?**
In a collaborative document each edit refers to neighbouring characters. A reader missing some
characters can't apply edits that refer to them, so the copies diverge, or the missing characters'
positions must be sent after all.

**Why can't a reader of a projection edit it?**
Their copy is a different document from the full text. Carrying edits from one to the other is
two-way synchronisation between different documents, the problem collaborative data types exist to
avoid, and it would be the most fragile part of the system.

**What is the difference between a clearance and a role?**
A role says which documents you may open and change. A clearance says how sensitive the text
inside may be. They are checked separately, and both must allow it.

**How do you know hidden text never reaches the browser?**
A browser test records everything the lowest-cleared reader's page receives, every HTTP response
and every WebSocket frame, and checks the hidden text isn't in any of it. A unit test checks no
projection contains a character from a run above its level.

**What does a redaction bar give away?**
That something is hidden, where, its level, and roughly how long it is. Never the exact length,
and never how many separate words.

**Who may classify a section top secret?**
Someone with edit access who is themselves cleared for top secret. Nobody can hide what they
couldn't then read.
