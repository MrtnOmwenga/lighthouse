# living-docs

A tool that files the decisions made in AI coding sessions into a shared documentation
repository, with guardrails so that what it writes can be trusted. A command-line tool in Node,
about 3,900 lines, 127 tests. It is the standalone version of a pipeline designed and built for a
team working across several project groups.

Source: [github.com/MrtnOmwenga/living-docs](https://github.com/MrtnOmwenga/living-docs).

| # | Page | What it covers |
|---|---|---|
| 1 | [The problem and the capture pipeline](01-capture.md) | Where the "why" goes missing, what the docs look like, how a session's decisions get filed |
| 2 | [The guardrails](02-guardrails.md) | Routing by what a change is, lint before push, docs that can't run ahead of the code, history that can't be rewritten |
| 3 | [Automation, and what running it taught](03-automation-and-lessons.md) | The docs repository's own workflows, the faults only real runs found, what is and isn't verified |

What the review found that should be built or fixed is in the [build list](backlog.md).
