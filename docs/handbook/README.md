# The Lighthouse handbook

How Lighthouse and the projects it hosts work, and why they were built this way. Each page stands
on its own, cites the source files it describes, states what's weak or unfinished, and ends with
questions and answers.

It is written for three readers: someone evaluating the engineering, me before an interview, and
an assistant that answers questions about the portfolio by looking things up here.

| # | Page | What it covers |
|---|---|---|
| 1a | [Shipping the apps: the pipeline](01a-pipeline.md) | From a push to a running container: build, sign, keyless deploy, migrations |
| 1b | [Shipping the apps: Cloud Run and the edge](01b-cloud-run-and-edge.md) | How a request reaches a container; the Worker, the services, identities, secrets, data, and the free-tier choices |
| 2 | [Watching the apps: probes, the scheduler and incidents](02-monitoring.md) | What a probe checks, the SSRF guard, the database as the queue, the incident state machine, the external clock and its signed tokens |
| 3 | [Data model and tenant isolation](03-data-and-tenants.md) | The tables, row-level security, the application's limited role, cross-tenant functions, constraints, and how isolation is tested |
| 4 | [Auth and sandboxes](04-auth-and-sandboxes.md) | Sessions and cookies, the owner's GitHub sign-in, cross-site request protection, how a sandbox is created and bounded |
| 5 | [Privacy-friendly analytics](05-analytics.md) | Counting visitors without cookies or stored addresses: daily-salted pseudonyms, server-measured reading time, campaign tags, what's public |
| 6 | [The console](06-console.md) | The Vue app embedded in the Go binary: how it is built and served, the JSON API and its errors, polling, pagination, and what is enforced where |
| 7 | The public site | *to be written* |
| 8 | Testing and CI | *to be written* |

What the review found that still needs building is in the [build list](backlog.md); the costed options for backups, redundancy and self-managed databases are in the [resilience plan](resilience-plan.md).

## Conventions

- **Front matter** on every page: `project`, `topics`, `sources` (the files it describes) and
  `verified` (the date its claims were last checked against the code and the live deployment).
- **Headings are questions** where that's natural, so a page can be found by what someone asks.
- **Known gaps** are stated plainly, with what would fix them.
- **Questions and answers** close each page.
- Nothing here names a client or contains anything that isn't in the public repositories.
