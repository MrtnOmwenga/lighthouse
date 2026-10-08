# The Lighthouse handbook

How Lighthouse and the projects it hosts work, and why they were built this way. Each page stands
on its own, cites the source files it describes, states what's weak or unfinished, and ends with
questions and answers.

It is written for three readers: someone evaluating the engineering, me before an interview, and
an assistant that answers questions about the portfolio by looking things up here.

| # | Page | What it covers |
|---|---|---|
| 1a | [Shipping the apps: the pipeline](01a-pipeline.md) | From a merged pull request to a running container: released only after CI, signed and verified, keyless deploy, a tested candidate, automatic rollback, migrations |
| 1b | [Shipping the apps: Cloud Run and the edge](01b-cloud-run-and-edge.md) | How a request reaches a container; the Worker and its copies of the pages, the services, identities, where secrets are kept, backups and the restore test, and the free-tier choices |
| 2 | [Watching the apps: probes, the scheduler and incidents](02-monitoring.md) | What a probe checks, the SSRF guard, the database as the queue, the incident state machine, the external clock and what it took to make it work, quick confirmation, and who watches Lighthouse |
| 3 | [Data model and tenant isolation](03-data-and-tenants.md) | The tables, row-level security, the application's limited role, cross-tenant functions, constraints, and how isolation is tested |
| 4 | [Auth and sandboxes](04-auth-and-sandboxes.md) | Sessions and host-bound cookies, the owner's GitHub sign-in, the record of sign-ins and its alerts, session control, cross-site request protection, how a sandbox is created and bounded |
| 5 | [Privacy-friendly analytics](05-analytics.md) | Counting visitors without cookies or stored addresses: daily-salted pseudonyms, server-measured reading time, engaged readers, campaign tags and the email when one is opened, what's public |
| 6 | [The console](06-console.md) | The Vue app embedded in the Go binary: how it is built and served, the JSON API and its errors, polling, pagination, and what is enforced where |
| 7 | [The public site](07-public-site.md) | Server-rendered pages, content as validated YAML, live figures joined from the monitors, how the status page is worked out, and the launch page that wakes a sleeping demo |
| 8 | Testing and CI | *to be written* |

**Other projects:** [Redacted (the RBAC-API repository)](redacted/README.md), reviewed in eight
parts; [GhostChat](ghostchat/README.md), reviewed in three.

The pages describe the code on `main` as of the `verified` date in each. The review they came from
produced a build list; most of it has since been built, and each page's "known gaps" is what
remains.

What the review found that still needs building is in the [build list](backlog.md); the costed options for backups, redundancy and self-managed databases are in the [resilience plan](resilience-plan.md).

## Conventions

- **Front matter** on every page: `project`, `topics`, `sources` (the files it describes) and
  `verified` (the date its claims were last checked against the code and the live deployment).
- **Headings are questions** where that's natural, so a page can be found by what someone asks.
- **Known gaps** are stated plainly, with what would fix them.
- **Questions and answers** close each page.
- Nothing here names a client or contains anything that isn't in the public repositories.
