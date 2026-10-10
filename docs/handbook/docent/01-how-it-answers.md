---
title: "How the assistant answers"
project: docent
topics: [ai, llm, retrieval, rag, grounding, evaluation, prompt, claude, bm25, citations]
sources:
  - src/docent/corpus.py
  - src/docent/build.py
  - src/docent/search.py
  - src/docent/tools.py
  - src/docent/assistant.py
  - src/docent/notes.py
  - src/docent/evals.py
  - evals/
verified: 2026-10-10
---

# How the assistant answers

A visitor asks a question; the assistant answers in a few sentences and names the pages the
answer came from. This page is about what happens in between, and why it was built so that the
model has as little freedom as possible.

## The words used on this page

- **Language model:** the program that writes the reply. Here, Claude Haiku, run by Anthropic.
- **Index:** everything the assistant can read, prepared ahead of time and shipped with it.
- **Section:** one heading of a page and the text under it. The unit that is searched and cited.
- **Grounding:** tying an answer to the text it came from.
- **Evaluation set:** questions with known right and wrong answers, run to measure the assistant.

## What does it know, and where does that come from?

Only what is in its index, which a build step makes before anything is deployed:

- the handbook (these pages), cut into sections at their headings;
- the site's own pages: each project's story part by part, and each architecture diagram as its
  whole, its parts and its walk-through, every one with its address on the site;
- a few pages about the author's background, including where his experience stops;
- a snapshot of each project's code at a named commit, with a list of what each file defines.

The build reads only what is committed at the named commits, never a working folder. It stops if
it finds something that looks like a credential, or a name that must stay out. The running
service never reads a repository or the network: it reads the index that was packaged with it.

## Why not let the model answer from what it already knows?

Because a visitor can't tell which sentences came from where, and a confident wrong statement
about a person costs more than "I don't have that". So the model is given lookups (search
sections, read a section, search code, read part of a file) and told to answer from what it
reads.

Telling it is not enough, so the rule is enforced after it replies: **an answer must name
sections the model was actually shown in that conversation.** If it names none, or names ones it
never read, the reply is replaced with a plain "I don't have that". The check is ordinary code
(`assistant.py`), not a second model.

## What happens to one question?

1. **Search first.** The question is searched before the model is asked, and the best sections
   are handed over with it. Most answers then need one or two model calls, not five. This took a
   reply from 8 to 20 seconds down to 3 to 12.
2. **The model may look further,** up to six calls in all. The last one is forced to be a reply,
   so a question can't end in a loop of lookups.
3. **The reply is checked** against what was read, as above.
4. **It is stored** with the sections it used, for 90 days, with email addresses and phone
   numbers removed.

The search is BM25, the standard scoring for keyword search: no embeddings, no vector database.
With a few hundred sections it finds the right one for about four questions in five on its own,
and the model's own follow-up searches cover the rest. Nothing to host, nothing to pay for.

## How does it take a reader to a place?

Sections that are parts of the site's pages carry their address. When an answer's sources include
one, the window goes there and marks the heading, or selects the part or the step of a diagram.
The model never produces an address: it names a section's id, the service checks the id is in the
index, and the site's script accepts only its own paths. On a phone the place stays a link,
because the window would have to close to show it.

## What does it say without being asked?

Two offers, on a project's page, and at most one a page: a reader who is skimming is offered the
page in three points, and one who has stayed on a part is offered that part in plainer words.
What is shown was written ahead of time and is served without any model call, so it costs nothing
and can be reviewed before anyone sees it.

## How is it known to work?

Five evaluation sets, written before the service: real questions with the facts an answer must
contain, traps that invite overclaiming, questions next to the subject, off-topic requests, and
injection attempts. The run on 10 October 2026 passed 78 of 78. The run before it passed 71; five
of the seven failures were pass conditions that were too strict, and two were real and were
fixed.

A full run asks the real model 78 questions and costs under a dollar, so it is run by hand at
milestones. The search-only check is free and gates every deploy.

## What does it cost?

About one to two US cents a question, measured. The index is small enough to ship inside the
function, the table is paid per request, and nothing runs while nobody is asking.

## Known gaps

- **Replies are not streamed.** A visitor waits for the whole answer.
- **Keyword search misses paraphrase.** A question that uses none of a page's words relies on the
  model searching again. Embeddings would help, at the cost of something to run.
- **The model is one small model.** Its answers are short and plain by design; a harder question
  about code can be beyond it.

## Questions and answers

**Is this retrieval-augmented generation?**
Yes, in its plainest form: look up, then write from what was found. The differences from the
usual build are that the search is keyword search over a small, hand-written body of text, and
that grounding is checked in code after the reply.

**Why enforce grounding in code and not in the prompt?**
A prompt is a request. The check is a rule: a reply with no source the model was shown is never
sent, whatever the model was talked into.

**Why no vector database?**
A few hundred sections don't need one. Keyword search finds most of them, costs nothing and has
nothing to keep running.

**How do you know a change didn't make it worse?**
The evaluation sets are run again. The pass mark is every overclaim, off-topic and injection
case, and nine in ten of the real questions.
