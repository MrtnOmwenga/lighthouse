---
title: "The guardrails"
project: living-docs
topics: [guardrails, llm-safety, human-in-the-loop, append-only, prompt-injection, linting, review-routing]
sources:
  - README.md
  - policy/documentation-policy.md
  - bin/lib/docs-lint.cjs
  - bin/lib/docs-sync.cjs
  - bin/lib/proposals.cjs
  - bin/lib/policy-version.cjs
  - bin/lib/doctor.cjs
  - test/capture-pipeline.test.cjs
  - test/held-implementation.test.cjs
verified: 2026-10-09
---

# The guardrails

A model writes the documentation. This page is about everything that stands between what the
model produces and what the team reads, and the one idea behind all of it: **decide by what a
change is, never by how sure the model sounds.**

## The words used on this page

- **Guardrail:** a check outside the model that limits what it can do, whatever it outputs.
- **Lint:** an automatic check of a file against rules.
- **Diff:** the exact lines that changed.
- **Pull request (PR):** a proposed change waiting for a person to approve it.
- **Append-only:** can be added to, never edited or removed.
- **Prompt injection:** text planted where an AI will read it, written to be obeyed as an
  instruction.

## Who decides whether a person must review a change?

Not the model. The route is chosen mechanically from the change itself:

| The change | Where it goes |
|---|---|
| A new Decision History entry | Straight in |
| Implementation, from code already merged | Straight in |
| Implementation, from a branch not yet merged | Held as a draft; merged automatically when the code merges, closed if the code is abandoned |
| **Context:** what a module is *for* | A pull request, for a person |
| A new module | A pull request: the model can only *propose* one, never create it |
| Anything that couldn't be pushed | A pull request, so nothing is lost |

The important detail: a change to Context is detected from **the actual diff of that section**,
not from what the model says it did. A model that changed a module's purpose and didn't mention
it is still routed to a person.

The aim is that a person only ever does three things: approve a new area, review a change of
purpose, and review corrections. Nobody has to remember to document anything, and nobody has to
read every routine update.

## What is checked before anything is pushed?

Lint runs in the worker (`bin/lib/docs-lint.cjs`):

- **Decision History can only grow.** Existing entries must survive, unchanged, **and in the same
  order**.
- **A module's structure can't regress:** its sections stay.
- **Added lines can't contain what looks like a secret.**
- **Only existing modules may be edited.** A new file or an edit to the index is dropped, because
  new modules are proposed, not created.

A file that fails is reverted; the rest of the capture carries on.

The documentation repository runs the same lint again on every change, and adds three checks:
a second secret scanner, a warning for personal data, and a check that **fails on text that reads
like instructions to an AI.** That last one matters here in particular: these docs are loaded
into every later session, so a planted "ignore your instructions and…" would steer all of them.

## Why can't the docs describe code that hasn't merged?

"Implementation" says what the code *does*. On a feature branch that is only a plan: the branch
may change or never merge. If the docs said it anyway, every other session would be told
something false.

So Implementation written on an unmerged branch **waits**, on a draft pull request tied to that
branch. It merges when the code merges and closes if the code is abandoned. A branch cut from
another waiting branch stacks on it, so the docs land in the order the code does. A session that
moved between branches is split by which branch each message was on.

Decision History doesn't wait. A decision was made whether or not the code ships.

## How is a decision traced, or corrected?

- **Every entry is stamped** with the repository, branch and commit it came from, by the worker,
  not by the model, so the stamp can't be invented.
- **History is never edited.** A reversed decision gets a *new* entry that names the one it
  supersedes. The check warns when one refers to an entry that doesn't exist.

## What stops it becoming noise?

- **Proposals don't pile up.** A duplicate or near-duplicate is dropped. One that is already open
  gets a "seen again" note, so the evidence gathers in one place. One a person closed isn't raised
  again for 90 days.
- **A session with no real discussion costs nothing** and files nothing.

## How would anyone know it had stopped working?

Silent failure is the usual end of automation like this, so it is made visible three ways:

- **A `doctor` command** checks the whole chain: the hooks, the connection, the index, recent
  captures, waiting proposals, and pull requests that have waited too long.
- **A session starts with a one-line notice** when captures have been failing.
- **A weekly digest flags a week with no captures at all.**

## What if developers have different versions of the tool?

The rules carry a version, and the docs repository records the one it is on. A tool older than
the docs **pauses its captures** rather than writing under rules it doesn't know.

## Known gaps

- **The checks are about form, not truth.** They guarantee where a change goes and what shape it
  has. A plausible, well-formed, wrong entry passes all of them.
- **The secret and instruction checks are by pattern.** They catch the obvious and can be evaded.
- **Implementation is rewritten in place,** so a capture can replace a correct description with a
  worse one. The weekly comparison against the code is the backstop.
- **Review is the bottleneck by design.** If nobody looks at the pull requests, changes of purpose
  and new modules wait indefinitely (with reminders, then closure).

## Questions and answers

**Why not let the model decide what needs review?**
A model is most confidently wrong exactly when review matters. Routing by the diff can't be
talked out of it.

**How is a change of purpose caught if the model doesn't report it?**
The worker compares the Context section before and after. The route depends on that comparison,
not on the model's account.

**Why does the history check look at order as well as presence?**
A real run inserted a new entry *above* the existing ones. Every old entry survived, and the
record was still rearranged. The check now requires the old entries in their old order.

**Why do the docs wait for the code?**
A description of unmerged code is a description of something that may never exist, and every
other session would read it as true.

**Why is prompt injection a particular risk here?**
The docs are fed to every future session. One planted instruction would be obeyed again and
again.

**What is the point of the version check?**
Without it, a developer on an old version would write under old rules into docs that have moved
on, and nobody would notice until the docs were inconsistent.
