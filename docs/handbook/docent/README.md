# The assistant (docent)

The AI assistant on the site: it answers questions about the projects and their author from a
fixed body of material, says where each answer comes from, and can take a reader to the part of
the site it is talking about. Python, AWS Lambda and DynamoDB, Claude Haiku through Anthropic's
API. About 2,900 lines, a quarter of it infrastructure code.

Source: the `docent` repository, which is private for now (its background pages name an employer
and clients). File paths on these pages are in that repository. The window visitors see is in
Lighthouse (`internal/web/static/guide.js`), and so is the edge that carries questions to it.

| # | Page | What it covers |
|---|---|---|
| 1 | [How it answers](01-how-it-answers.md) | The index built ahead of time, search before the model, the rule that an answer must rest on a source, what it costs, how it is evaluated, and how it takes a reader to a place on the site |
| 2 | [Keeping it safe to run](02-keeping-it-safe.md) | What an attacker could want, why prompt injection gets them little, the caps, the off switch, the firewall and what it doesn't do, secrets and rotation, and what the AWS account's plan ruled out |

## What is left

| Item | State |
|---|---|
| Review of real transcripts after a fortnight of visitors | Waits on visitors |
| GuardDuty, Security Hub, deploys from CI by federation | Blocked by the AWS account's free plan; to be reconsidered |
| A signed build | Not done: the package is built on a signed-in machine, because the index is made from local checkouts |
| Streaming replies | Not done: a reply arrives whole, in 3 to 12 seconds |
| The assistant in the open | It is behind a preview switch until its pre-written notes have been reviewed |
