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

## Questions for a second version (Martin, 2026-10-09)

Raised while reviewing the design with a critical eye, and from a first week of real use on a
team's projects.

| # | What | Why | Size |
|---|---|---|---|
| L8 | **Give a decision a status, like an architecture decision record:** *proposed* while its branch is unmerged, *accepted* when the code merges, *abandoned* when the pull request is closed, *superseded* when a later entry replaces it. Sessions read only accepted decisions as fact. | Decisions publish at once, before code review. A poorly made decision on a branch is recorded exactly like a good one, and stays if review overturns it. | M |
| L9 | **Capture the code review, at merge:** what reviewers objected to and what changed as a result, filed as entries (and as the reason a proposed decision was accepted, changed or dropped). | Review is where a misguided approach is corrected, and today none of that reaches the docs unless it happens inside a captured session. | M |
| L10 | **Capture at more moments than the end of a session:** when the AI tool pushes a branch or opens a pull request (a hook on those commands, not shell aliases), and from a git push hook for work done without it. | A long session can open and merge several pull requests before a capture runs, so the docs trail the code and the branch a decision belonged to has to be reconstructed afterwards. | M |
| L11 | **Don't let review of the docs become the bottleneck:** merge routine, checkable changes automatically after a waiting period when an independent check agrees, and keep a person for changes of purpose and new modules. | Pull requests in the docs repository wait on people who are busy with code review. | M |
| L12 | **An independent checking pass, used for triage and never as the approver** (see L7): a second model, with the code and the transcript, verifies claims that can be checked ("the code does what this Implementation says", "the conversation supports this entry") and labels the pull request. It does not judge whether a decision was good. | A second model approving the first's output shares its blind spots. Used to verify checkable facts, it removes most of the reading from the human reviewer without replacing them. | L |
| L13 | **Make the docs cheap and accurate for a model to read:** a map from code paths to modules, so a session loads the modules for the files it is touching; a short card per module in the index; recent decisions separate from an archive; and a search tool the model can call, instead of loading everything. Deterministic lookups first; embeddings only if those stop being enough. | Nine modules reached about 12,000 tokens in a week, most of it Decision History. Loading everything stops being possible long before the docs are large. | L |
| L14 | **Keep entries to decisions.** Reject, or route elsewhere, entries that only say what changed ("added X to endpoint Y") with no reason and no alternative considered. | In real use many entries were a change log, which the commit history already is. | S |
| L15 | **Keep point-in-time facts out of the index and Context** ("merged in pull request N"), by lint. | They were true the day they were written and are noise afterwards. | S |
| L16 | **Guard against module sprawl:** propose folding a narrow feature into the business area it belongs to. | Modules drifted towards one per feature. | M |
| L17 | *(Optional)* **Publish the docs as a browsable site** from a workflow (search, navigation, a decision timeline), with a clear choice about who can see it. | Easier for people to read than a repository. Internal docs must not become public by accident: private hosting of such a site depends on the hosting plan. | S |
| L18 | **Prove it on a new project with two developers from the first day** (the planned AWS project), against the real hosting service. | Covers L1, and tests the second version where it matters: with real branches, reviews and delays. | M |
