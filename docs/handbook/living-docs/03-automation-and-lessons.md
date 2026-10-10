---
title: "Automation, and what running it taught"
project: living-docs
topics: [github-actions, drift-detection, llm-testing, testing-with-stubs, lessons-learned, honest-status]
sources:
  - README.md
  - integrations/docs-repo/ci/lint.cjs
  - integrations/docs-repo/ci/lifecycle.cjs
  - integrations/docs-repo/ci/drift.cjs
  - integrations/docs-repo/ci/digest.cjs
  - integrations/docs-repo/ci/capture-merge.cjs
  - test/docs-ci.test.cjs
  - test/capture-pipeline.test.cjs
verified: 2026-10-09
---

# Automation, and what running it taught

## The words used on this page

- **Workflow:** an automated job that runs in the repository's hosting service.
- **Drift:** the docs and the code no longer saying the same thing.
- **Stub:** a stand-in for a real program in a test, which returns a prepared answer.
- **Mutation check:** breaking the code on purpose to see whether a test notices.

## What runs in the documentation repository itself?

Four workflows, installed by one command, and later updated through a pull request (they run with
the organisation's secrets, so a change to them is reviewed like any other).

| Workflow | When | Does |
|---|---|---|
| Checks | Every change | The lint again, a secret scan, the check for instructions to an AI, personal-data warnings |
| Lifecycle | Hourly | Merges a waiting Implementation change when its code merges; closes it when the code is abandoned; reminds after 14 idle days and closes after 30. On Mondays, a health digest |
| Notify | A pull request opens | A message to the team's chat. Waiting drafts never ping anyone |
| Drift audit | Weekly | A read-only model compares each Implementation section with the code and opens one pull request of corrections, each citing a file and line |

A missing secret makes its step skip rather than fail, so a repository without a chat webhook
still works.

The drift audit is the backstop for everything else. Captures can be wrong and code changes
without a session; once a week the docs are compared with what the code really does.

## What did running it for real find?

The tests use stand-ins for the AI tool and the hosting service. They passed, and four faults
still appeared the first time the real tool was used:

1. **The AI tool refuses to write anywhere under its own settings folder,** even when allowed to.
   The model read the docs, reasoned correctly, asked "May I proceed?", and wrote nothing. The
   working copies moved elsewhere.
2. **A small model will guess instead of looking.** Told to "check the existing files first", it
   wrote that the project had no docs, with a docs file in its working folder. The prompt now
   states the list of modules and the whole index outright.
3. **A real model put the new history entry first.** Hence the order check on
   [the previous page](02-guardrails.md).
4. **A model started from inside a session inherits that session's environment** and silently
   can't write. Those settings are now stripped.

Two more came from reading the workflows against how the hosting service actually behaves: a pull
request opened by an automated job doesn't trigger other workflows, so the drift audit's own
corrections would have skipped the lint; and one path couldn't fetch the docs with the kind of
credential it had.

The lesson is the same one the Lighthouse build produced (backups that reported success while
storing seven bytes): **stand-ins test your understanding of a system, not the system.**

## How is it tested, and how far?

127 tests: unit tests, and end-to-end runs of the whole pipeline against real local git
repositories, with the AI tool and the hosting command stubbed. Each behaviour was mutation
checked: broken on purpose, to confirm the intended test fails. That found gaps in the tests
themselves.

Real runs: a capture after compaction and a capture at the end of a session both produced
correct, attributed, stamped entries; a proposal became a branch; and a planted "delete every
file" line in a transcript was ignored. The project's own README calls that what it is: one
sample, evidence and not proof.

**Not verified:** anything against the real hosting service (opening and merging pull requests,
the workflows actually running); a second developer's machine from start to finish; the
first-time seeding and the drift audit with a real model on real code.

## Known gaps

- **The automation in the hosting service has never run for real** in this standalone version.
  It is tested with stand-ins only.
- **The drift audit opens a new pull request each week** even when last week's is still open.
- **Seeding reads the code, not its history,** so it can describe what exists and not why.
- **Every capture and every audit costs a model call,** and nothing reports the running cost.
- **It depends on one hosting service** and its command-line tool.

## Questions and answers

**What is drift, and how is it caught?**
The docs and the code no longer agreeing. Once a week a read-only model compares each
Implementation section with the code and proposes corrections, each with a file and line.

**Why are the workflows updated by pull request and not pushed?**
They run with the organisation's secrets. A change to them is a change to something powerful, so a
person reviews it.

**The tests passed. Why did real runs find faults?**
The tests replaced the AI tool with a stand-in that behaved as expected. The real tool refused a
write, guessed instead of looking, and reordered entries. A stand-in only encodes what you
already believe.

**What would you verify next?**
The whole thing against the real hosting service with two developers, since everything about pull
requests and workflows is so far tested only with stand-ins.

**How do you describe evidence from one successful run?**
As one sample. The README says so in those words, and lists what hasn't been verified.
