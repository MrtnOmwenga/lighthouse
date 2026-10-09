---
title: "Testing and the pipeline"
project: redacted
topics: [testing, generated-tests, property-based-testing, mutation-testing, load-testing, playwright, ci]
sources:
  - test/authorization-matrix.e2e-spec.ts
  - test/browser/redacted.spec.ts
  - test/tokens.e2e-spec.ts
  - test/support/database.ts
  - stryker.config.json
  - load/smoke.js
  - .github/workflows/ci.yml
  - .github/workflows/flake-hunt.yml
verified: 2026-10-09
---

# Testing and the pipeline

605 tests: 79 unit, 518 end-to-end (441 of them generated) and 8 in a browser. This page lists
only what is unusual about them.

## What is worth knowing

- **Most of the tests are generated from the policy.** The authorization matrix sends one real
  request for every principal (five roles, three kinds of API key) × every action × own
  department, another department, another organization: 441 cases. The expected answer comes from
  `can()` ([Part 1](01-permissions.md)), and a last test fails if an action has no scenario.
- **The central claim has its own test.** A browser test records everything the Intern's page
  receives, every HTTP response and every WebSocket frame, and checks that the hidden text isn't
  in any of it ([Part 5](05-redaction.md)).
- **Property tests** (fast-check) check rules over thousands of random cases instead of chosen
  examples: nothing crosses organizations, viewers and auditors never change anything, a key never
  exceeds its scopes, no projection contains a hidden character.
- **Mutation testing** (Stryker) on the five security-critical files: the policy, tokens, the
  audit chain, canonical JSON and the projection code. The tool changes the code in small ways and
  the tests must notice. CI fails below 95%; the score is 100%, with five changes marked as
  harmless, each with its reason.
- **Tests that prove a fix.** Several realtime tests were written to fail first: an edit sent the
  instant a demotion returns was accepted, every time, before the lock was added
  ([Part 6](06-live-collaboration.md)).
- **Attack cases are tests.** Tokens with `alg: none`, a wrong secret, an edited payload, the
  wrong audience; a key sent as a token; fields a client may not set (mass assignment);
  self-promotion. All refused, all asserted.
- **Fast and isolated.** One PostgreSQL container per run; migrations run once into a template
  database and each test worker clones it in milliseconds. Every test makes its own organization,
  so nothing is shared and nothing needs cleaning up.
- **A latency budget in CI.** A k6 load test (20 users for 30 seconds) fails the build if reads
  exceed 100 ms or writes 400 ms at the 95th percentile, or more than 1% of requests fail.
- **A weekly flake hunt** runs the whole suite ten times with no retries, so a test that fails
  only sometimes is found on a schedule and not as a mystery red build.
- **The README's permission table is checked in CI** against the policy it is generated from.

## What was added on 2026-10-09?

- **The pipeline matches Lighthouse's:** it starts when CI has passed, verifies the image's
  signature, migrates, starts the new revision without traffic and checks it, moves traffic,
  checks through the edge, and moves traffic back on failure. Seven checks are required before a
  merge.
- **Behaviour over time is tested** with real connections: a share running out, a token expiring,
  a sign-out on one device leaving the other connected.
- **Every migration is run down and up again** and the schemas compared.
- **Two servers are tested together,** sharing only the database: edits, saves, classification,
  demotion and sign-out all cross ([Part 6](06-live-collaboration.md)). The first run of that
  test is what showed edits weren't shared.
- **Every route must declare its action,** and a handler that skips its check is shown to fail.

## Known gaps

- **The load test is small and from one organization:** it guards against a regression, and says
  little about real capacity.

## Questions and answers

**Why generate the authorization tests instead of writing them?**
Written by hand, they cover the cases someone thought of. Generated from the policy, they cover
every cell of the table, and a new action can't be added without being covered.

**What is a property test?**
A test of a rule ("nothing ever crosses organizations") over many random inputs, instead of a
handful of examples someone chose.

**Mutation score 100%: what does that mean, and what doesn't it?**
Every small change the tool made to those five files was caught by a test. It says the tests
would notice a wrong line there. It says nothing about code outside those files, or about rules
nobody wrote down.

**Why a flake hunt?**
An unreliable test teaches people to re-run and ignore failures. Finding flaky tests on a schedule
keeps a red build meaningful.
