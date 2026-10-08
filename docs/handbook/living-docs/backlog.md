# Build list from the review of living-docs

Nothing is built during the review. Sizes: S (under an hour), M (a few hours), L (a day or more).

| # | What | Why | Size |
|---|---|---|---|
| L1 | **Run the whole pipeline against the real hosting service,** with two developers: captures, pull requests opening and merging, the four workflows. | Everything involving pull requests and workflows is tested with stand-ins only; real runs of the capture found four faults the stand-ins couldn't. | M |
| L2 | **An "undo this capture" command** (revert its commit, and note that it was withdrawn). | A bad capture is corrected by hand today. | S |
| L3 | **Have the drift audit update last week's open pull request** instead of opening a new one. | A new one is opened every week regardless. | S |
| L4 | **Report what it costs:** model calls and tokens per capture and per audit, in the weekly digest. | Every capture and audit costs a model call and nothing totals them. | S |
| L5 | **Help a session find the right module as the docs grow** (a map from code paths to modules). | The index is handed over whole. | M |
| L6 | **Archive old Decision History** into a linked file once a module's passes a size. | It only grows. | S |
| L7 | *(Research)* **Check a captured decision against the transcript it came from:** a second, independent pass that flags entries the conversation doesn't support. | The guardrails check form and routing, not truth; a plausible wrong entry passes all of them. | L |
