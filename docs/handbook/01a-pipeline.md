---
title: "Shipping the apps: the pipeline"
project: lighthouse (RBAC-API and GhostChat run the earlier, simpler version of this pipeline)
topics: [ci-cd, github-actions, docker, cosign, workload-identity-federation, cloud-run, migrations]
sources:
  - .github/workflows/release.yml
  - .github/workflows/ci.yml
  - Dockerfile
  - deploy/cloudrun/identity.tf
  - deploy/cloudrun/registry.tf
  - internal/store/migrations/README.md
verified: 2026-10-07
---

# Shipping the apps: the pipeline

Lighthouse hosts three apps: itself, Redacted (the RBAC-API repository) and GhostChat. Each lives
in its own repository and ships itself: a push to the release branch builds an image, signs it,
and deploys it to Google Cloud Run. No cloud credential is stored in GitHub at any point.

```
push to the release branch
  └─▶ job "image"                               job "deploy"
      build for amd64 + arm64                    prove to Google who we are (no stored key)
      push to GHCR                          ─▶   copy the amd64 image to Artifact Registry
      sign with cosign (keyless)                 run the database migrations (a Cloud Run job)
                                                 point the service at the new image, by digest
                                                 check the public URL answers
```

Tests and scanners run separately, on every pull request and on the release branch (`ci.yml`,
see the testing page).

## How is the image built?

The `Dockerfile` has three stages:

1. **Console:** Node builds the Vue console into static files.
2. **Binary:** Go compiles one static binary with those files embedded.
3. **Final image:** a distroless base with only that binary and the site's content. No shell, no
   package manager, and it runs as a non-root user, so code execution inside the container gives
   an attacker nothing to work with.

Both build stages are declared `FROM --platform=$BUILDPLATFORM`, and the binary is compiled with
`GOOS=$TARGETOS GOARCH=$TARGETARCH`. The build therefore always runs natively on the CI machine
and Go cross-compiles the arm64 binary, instead of building under emulation (roughly ten times
slower).

## What does the "image" job do?

- **Tags** the image `<unix time>-<short commit>`, for example `1790669030-a71356c`. Tags sort by
  age (which Flux needs on the Kubernetes path) and say exactly which commit is inside.
- **Builds and pushes** both architectures to GHCR, reusing layers between runs
  (`cache-from/to: type=gha`), and attaches build provenance and an SBOM (a list of what's inside).
- **Signs** the image by digest with cosign, keyless: there is no signing key to store or leak.
  GitHub issues the job a short-lived identity token ("the release workflow of this repository"),
  and the signature is bound to that identity in a public transparency log. That's why the
  workflow has `id-token: write`.

## How does a job get an identity token from GitHub?

GitHub runs an identity service (`https://token.actions.githubusercontent.com`). It holds private
signing keys and publishes the matching public keys at `/.well-known/jwks`.

1. **GitHub knows who the job is because GitHub started it.** A push triggered the workflow, so
   GitHub knows first-hand the repository, branch, commit, workflow and actor. The job's code
   doesn't claim any of it.
2. **The job is given a way to ask.** With `id-token: write`, GitHub puts an internal address and a
   one-time access code in the job's environment, valid for that run only.
3. **A step asks**, naming who the token is for (the *audience*): here, the Google identity pool.
4. **GitHub writes the facts down and signs them** with its private key. The result is a JWT:
   `iss` (GitHub's identity service), `aud`, `repository`, `ref`, `sha`, `workflow`, `actor`,
   issue and expiry times, and more. The job chooses only the audience, and the token lasts
   minutes.
5. **The receiver verifies it without asking GitHub about the job:** it fetches GitHub's public
   keys, checks the signature, the expiry and the audience, then applies its own rule to the facts.

It can't be faked: changing a fact breaks the signature, GitHub fills in the facts itself (a fork
gets a token naming the fork), and a stolen token expires in minutes and works for one audience.

Lighthouse plays the receiver's part itself for Cloud Scheduler's tokens (`internal/oidc`, see the
monitoring page).

## What is the difference between the identity token and the signature?

They answer different questions, at different times.

- **The identity token** answers "who is asking, right now?" GitHub issues it to a running job; it
  lasts minutes; it is shown once to get something (a Google credential, or a signing
  certificate) and then it is gone. It's like showing ID at a door.
- **The signature** answers "where did this image come from?", for as long as the image exists.
  It is attached to the image's digest and says "the release workflow of this repository produced
  exactly this content". Anyone can check it later, without GitHub being involved. It's like a
  seal on a parcel.

Keyless signing connects the two: cosign shows the identity token to Sigstore's certificate
authority, which issues a certificate valid for about ten minutes naming the workflow; cosign signs
the digest with it and the signature is recorded in a public log. No long-lived key ever exists.

Signing matters when something *verifies* it: a deploy step or a cluster policy that refuses any
image not signed by this workflow, which would stop an image swapped in the registry. Here the
signature is produced but not yet verified (see Known gaps).

## What is a digest?

A digest is the SHA-256 hash of an image's contents, written `sha256:8e0e58c8…`. The same content
always gives the same digest, and any change, however small, gives a different one, so a digest
*is* the content's identity. A tag (`:main`, `:1790669030-a71356c`) is only a name pointing at a
digest, and can be pointed somewhere else later.

It is the same relationship as a git branch name and a commit hash. Registries check the digest
when an image is pulled, so content that doesn't match is rejected.

A multi-architecture image has one digest for the index (the list of variants) and one per
variant. That's why the deploy job asks the registry for the digest after copying the amd64
variant, instead of reusing the build's digest.

## What does "distroless, non-root" mean?

Most images start from a small Linux distribution: a shell, a package manager, tools like `curl`.
An attacker who finds a bug in the app usually uses exactly those to look around and download more
tools. A distroless image contains none of them: only the program, TLS root certificates and time
zone data. There is nothing to run except the app itself, far less for vulnerability scanners to
flag, and the image is about 7 MB.

Non-root means the process runs as an unprivileged user (id 65532). It can't modify system files,
and a flaw that let it escape the container wouldn't hand over root on the host.

The cost: there is no shell to open inside a running container. Debugging is done from logs.

## Why are there no cloud keys in GitHub?

The usual way to let CI deploy is a service-account key file stored as a secret. It never expires,
and whoever copies it controls the project.

This pipeline uses **Workload Identity Federation** instead:

1. GitHub gives the job a signed OIDC token stating facts about it: the repository and the branch.
2. The job presents that token to Google (`google-github-actions/auth`).
3. Google verifies GitHub's signature and checks the facts against a condition.
4. If it passes, Google issues a credential that lasts about an hour, for one service account
   (`github-deployer`).

The condition is in `deploy/cloudrun/identity.tf` and admits exactly three combinations:

```
(repository == 'MrtnOmwenga/lighthouse' && ref == 'refs/heads/main')
|| (repository == 'MrtnOmwenga/RBAC-API'  && ref == 'refs/heads/master')
|| (repository == 'MrtnOmwenga/GhostChat' && ref == 'refs/heads/master')
```

A fork, a pull-request branch or any other repository gets no token. The four `GCP_*` values the
workflow reads are Actions *variables* (a provider name, a service-account email, a region, a
registry path), not secrets: none of them is a credential.

The deployer account can push images to one registry, update the Cloud Run services, run the
migration jobs, and act as the three services' runtime accounts to do so. It can't read secrets'
values directly or touch anything else in the project.

## When does a release run?

Not on every push. The release workflow is started by the CI workflow **finishing successfully on
a commit pushed to `main`** (`on: workflow_run`), and it builds exactly that commit. A commit CI
hasn't passed is never built or deployed. A release can also be started by hand; then the first
step asks GitHub whether CI passed on that commit, and stops if it didn't.

`main` itself is protected by a branch rule: changes arrive through a pull request whose six CI
checks have passed, and force pushes and deletion are blocked. The rule and the workflow
dependency do different jobs. The rule can be changed by an administrator; the dependency is part
of the workflow and can't be skipped by a setting.

## What does the "deploy" job do?

In order, each step stopping the release if it fails:

1. **Verifies the signature.** `cosign verify` checks that the image was signed by this
   repository's release workflow on `main`, before anything is copied. A signature nobody checks
   protects nothing; this is the check.
2. **Copies the image** from GHCR to Artifact Registry with `crane copy --platform linux/amd64`.
   Cloud Run can't pull from GHCR, and it only runs amd64, so only that architecture is copied
   (which also keeps storage inside the registry's free 0.5 GB).
3. **Pins the digest.** Everything after the copy refers to `image@sha256:…`, never a tag. A tag
   can be moved to different content; a digest can't.
4. **Migrates.** A separate Cloud Run job runs the app's migration command on the new image, as
   the database owner, and the workflow waits for it. If it fails, its output is printed in the
   workflow log and the previous version keeps serving.
5. **Starts the new revision with no traffic,** and gives it a private address (a "candidate"
   tag).
6. **Tests the candidate** at that address (`/readyz`). If it doesn't answer, the release stops
   and no visitor ever reached the new revision.
7. **Moves traffic** to the new revision.
8. **Tests again through the edge** (the public address, through Cloudflare).
9. **Rolls back** if that test fails: traffic goes back to the previous revision, automatically.

The job is skipped when the `GCP_*` variables aren't set (a fork still builds cleanly), and deploys
never overlap (`concurrency`).

**Why not a canary** (sending a few percent of visitors to the new revision first)? A canary needs
enough traffic to tell a bad revision from a good one. This site gets tens of visits a week; 5% of
that is no signal. Testing the revision before it takes traffic, and rolling back on failure,
gives the safety without pretending to measure something.

## Why are migrations a separate job, not something the app does at startup?

- **Privilege:** migrations need the database owner; the running app connects as a
  least-privilege role that can't alter tables. Only the migration job is given the owner password.
- **Ordering:** one migration run per deploy, finished before any new instance starts, with a
  clear failure that stops the deploy.
- **Startup time:** the services scale to zero and start on demand; a start shouldn't wait on
  schema checks.

## How do the three repositories differ?

| | Lighthouse | Redacted (RBAC-API) | GhostChat |
|---|---|---|---|
| Release branch | `main` | `master` | `master` |
| Migration job | `lighthouse-migrate` | `redacted-migrate` | none (MongoDB) |
| Smoke test | `/readyz` | `/health/ready` | `/health` |

## Why is an arm64 image built if Cloud Run runs amd64?

For the Kubernetes path (`deploy/k8s`), which targets Arm hosts, and so the published image runs
on Arm laptops. Cross-compiling makes it nearly free.

## How do I see what happened on Google's side?

- **The workflow itself:** `gcloud run jobs execute --wait` and `gcloud run services update` fail
  the job if the migration or the new revision fails, so a red deploy job is the first signal.
- **Migration runs:** `gcloud run jobs executions list --job lighthouse-migrate --region us-east4`
  lists each run with its result; their output is in Cloud Logging
  (`resource.type="cloud_run_job"`).
- **Revisions:** `gcloud run revisions list --service lighthouse --region us-east4` shows each
  deployed version, when, and whether it became ready.
- **Requests and app logs:** Cloud Logging, `resource.type="cloud_run_revision"`.
- **The scheduler:** `gcloud scheduler jobs describe lighthouse-tick --location us-east4` shows the
  last attempt and its status.

## What does the smoke test check?

`GET /readyz`, twice: first on the new revision's private address before it takes traffic, then on
the public address, through Cloudflare and the edge Worker, expecting 200 within a minute. `/readyz` pings the database with a two-second limit, so a 200 means: DNS, the edge, the
new revision starting, its secrets, and its database connection all work. It doesn't exercise any
feature.

## What rule do migrations follow?

Migrations run before the new code takes traffic, while the previous release is still serving. So
**a migration must work with the release that is live when it runs**: add first, remove in a later
release. The rule is written down beside the migrations
(`internal/store/migrations/README.md`), and a test runs every migration down to nothing and back
up (`TestMigrationsRunDownAndUpAgain`), so the "undo" scripts are known to work.

A rollback moves traffic, not the schema: because of the rule, the previous code works with the
new schema.

## What did running it for real find?

Two faults that reading the workflow didn't show:

- **The release failed its own gate once.** It asked GitHub "has CI passed on this commit?" a
  moment after CI finished, and the run that had just finished wasn't in the list yet. The gate
  now trusts the event that started it, and only asks (with retries) for releases started by hand.
- **The candidate step broke Terraform.** Passing the tag to the service update wrote the new
  revision's name into the service's template; the next Terraform change was refused for reusing
  that name. The tag is now set afterwards, as a traffic change.

## Known gaps

- **RBAC-API and GhostChat still run the earlier pipeline:** release on every push to the release
  branch, no signature check, no candidate revision, no rollback. Their branches require a pull
  request, without required checks yet. Bringing them level is part of their own reviews.
- **A manual release can be started by anyone with write access.** It still requires CI to have
  passed on the commit.
- **The candidate test is `/readyz` only:** the revision starts, reads its secrets and reaches its
  database. It doesn't exercise a feature.
- **The database is never rolled back automatically.** That is deliberate (see the rule above),
  and it means a migration that is itself wrong needs a person.
- **Database passwords generated by Terraform are still in its state.** Secrets a person obtained
  are not (see the Cloud Run page).

## Questions and answers

**What would an attacker need to deploy to the project?**
Write access to a release branch of one of the three repositories, which now means getting a pull
request merged (or control of an action the workflow runs; in Lighthouse those are pinned to commit
hashes, so a moved tag can't swap one). The facts in the token (repository, branch) are public, not secret; what protects
the system is that only GitHub can sign a token stating them, and Google checks that signature.
There is no key to steal.

**What's the difference between deploying a tag and deploying a digest?**
A tag is a movable name; a digest identifies the exact content. Deploying by digest guarantees the
service runs the image that was built, signed and copied in this run.

**What happens to the live site if a migration fails?**
Nothing: the job stops before the service is updated, and the previous revision keeps serving.

**Does the GitHub token go to Cloud Run?**
No. For deploying, it goes to Google's token-exchange service, which verifies it and returns an
access credential for the deployer service account, valid for about an hour; that credential is
attached to each Google request in the job (pushing the image, running the migration job, updating
the service). For signing, the same kind of token goes to Sigstore's certificate authority, which
returns a ten-minute certificate used once to sign the image. Pushing to GHCR uses a third
credential, the `GITHUB_TOKEN` GitHub gives every job.

**Where do the app's settings and secrets come from, if the pipeline only sets the image?**
Terraform (`deploy/cloudrun`) defines each service's environment, secret references, limits and
identity. The pipeline and Terraform each own one thing, so they don't overwrite each other.

**How long does a deploy take, and what does it cost?**
A few minutes end to end, on GitHub's free Actions minutes for public repositories; the registry
and Cloud Run stay inside their free tiers.
