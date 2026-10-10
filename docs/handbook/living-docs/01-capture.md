---
title: "The problem and the capture pipeline"
project: living-docs
topics: [documentation, ai-coding-tools, claude-code, hooks, decision-records, automation, llm-pipelines]
sources:
  - README.md
  - policy/documentation-policy.md
  - bin/living-docs.cjs
  - bin/lib/hooks.cjs
  - bin/lib/docs-sync.cjs
  - bin/lib/transcript.cjs
  - bin/lib/git.cjs
verified: 2026-10-09
---

# The problem and the capture pipeline

## The words used on this page

- **Hook:** a command an AI coding tool runs by itself at a set moment, such as when a session
  ends.
- **Transcript:** the full record of a session: what was asked, what was tried, what was decided.
- **Compaction:** when a long session is summarised to free up room; the tool writes a summary.
- **Capture:** one run of this tool: reading a session and filing what was decided.
- **Module:** one documentation file, covering one business area or one standing topic.
- **Worker:** a separate process that does the slow part after the session has moved on.

## What problem is this solving?

Code says *what* the system does. Commit messages say *what changed*. The **why** is in neither:
the constraint that ruled out the obvious approach, the option that was tried and abandoned. It
lives in a conversation, and is gone when the conversation ends.

AI coding sessions make this both worse and better. More decisions are made, faster. But every
one of them is already written down, in a transcript nobody reads again.

So the idea is simple: when a session ends, read its transcript and file the decisions.

The hard part is **trust**. Documentation that is written automatically and is wrong is worse
than none, because every later session reads it as fact and builds on it. Most of this project is
therefore not the capturing, it is the guardrails ([next page](02-guardrails.md)).

## What do the docs look like?

One git repository of documentation per group of projects: an index, and one file per **module**.
A module is a business area (auth, billing) or a standing topic (architecture, deployment, a
glossary, troubleshooting). Every module has the same four sections:

| Section | Holds | How it changes |
|---|---|---|
| **Context** | What the module is *for*: the use case, the limits, the constraints | Rarely, and only with a person's review |
| **Relationships** | What it depends on, and how | |
| **Implementation** | How it works *now* | Rewritten in place, so it stays true |
| **Decision History** | Dated entries: what was decided, why, what was rejected | Only ever added to |

The split matters. "How it works now" must be current, so it is rewritten. "Why we chose this"
must be permanent, so it is only added to. One document can't be both.

The rules are written once, in a policy file that every session loads, so the AI tool reads the
same instructions a person would.

## How does a capture happen?

1. **A hook fires** when a session ends, or when it is compacted. It hands the job to a detached
   worker and returns at once, so the developer never waits.
2. **The worker uses its own private copy of the docs,** never the developer's checkout. Two
   sessions ending together, or an edit the developer hasn't committed, can't collide with it.
3. **It pulls the latest docs,** and first sends anything left over from an earlier run.
4. **A small model reads the conversation and edits the modules.** For a compacted session it
   reads the summary; for an ended one, the transcript. A session only costs a model call if it
   held a real discussion (at least three turns from the user and about 1,500 characters), and it
   skips whatever an earlier capture already filed.
5. **The edits are checked, split by what they change, committed and pushed**
   ([next page](02-guardrails.md)). The commit is attributed "Claude (via the developer)" and
   records which repository, branch and commit the session was on.
6. **If the push is refused** because someone else pushed first, it pulls and tries again. On a
   real conflict it throws its attempt away and **reads the conversation again against the latest
   docs**, instead of merging two pieces of prose line by line. If it still can't land, it
   becomes a pull request, so nothing captured is lost.
7. **Offline:** the commit stays local and goes out at the start of the next capture.

## What about developers not using that tool?

The reading side works for any tool that can be pointed at files: an always-on rule for Cursor
says where the docs are. For the writing side, a workflow in the code repository can file a
merged pull request's decisions instead, and skips branches a developer's hooks already captured.

## Known gaps

- **The model can still be wrong.** The guardrails check the *form* of what is written and where
  it goes. Nothing checks that a recorded decision is what was actually decided.
- **A capture can't be undone** with a command. A bad one is corrected by another entry, or by
  hand.
- **It is built around one AI tool's hooks.** Others get reading, and a weaker path for writing.
- **A transcript can contain anything a developer typed or pasted.** The secret check is by
  pattern, and patterns miss things.
- **Nothing helps find the right module as the docs grow.** The index is handed over whole.
- **A long Decision History is never archived.**

## Questions and answers

**Why not ask developers to write this down?**
They don't, reliably, and the moment a decision is made is the moment nobody wants to stop and
document it. The transcript already contains it.

**Why separate "Implementation" from "Decision History"?**
One must stay current and is rewritten; the other must stay permanent and is only added to. Mixed
together, either the history gets edited or the description goes stale.

**Why does the worker use its own copy of the docs?**
So it can't collide with the developer's uncommitted edits, or with another session's capture
running at the same moment.

**What happens when two captures conflict?**
The second discards its attempt and reads the conversation again against the updated docs. Prose
merged line by line reads as nonsense; re-deriving it doesn't.

**Why is "wrong automatic documentation" worse than none?**
Later sessions read the docs as fact. One wrong entry becomes the premise for the next ten
decisions.
