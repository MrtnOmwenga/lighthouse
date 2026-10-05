---
title: "Shipping the apps: the pipeline"
project: lighthouse (the same pipeline runs in RBAC-API and GhostChat)
topics: [ci-cd, github-actions, docker, cosign, workload-identity-federation, cloud-run, migrations]
sources:
  - .github/workflows/release.yml
  - .github/workflows/ci.yml
  - Dockerfile
  - deploy/cloudrun/identity.tf
  - deploy/cloudrun/registry.tf
verified: 2026-10-05
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

## What does the "deploy" job do?

- **Copies the image** from GHCR to Artifact Registry with `crane copy --platform linux/amd64`.
  Cloud Run can't pull from GHCR, and it only runs amd64, so only that architecture is copied
  (which also keeps storage inside the registry's free 0.5 GB).
- **Pins the digest.** Everything after the copy refers to `image@sha256:…`, never a tag. A tag can
  be moved to different content; a digest can't.
- **Migrates.** A separate Cloud Run job runs the app's migration command on the new image, as the
  database owner, and the workflow waits for it. If it fails, the deploy stops and the previous
  version keeps serving.
- **Deploys** by changing only the service's image. The service's settings, secrets and limits
  belong to Terraform, which ignores the image field.
- **Smoke-tests** the public address through Cloudflare for up to a minute, so the whole path is
  tested, not the container alone.

The job is skipped when the `GCP_*` variables aren't set (a fork still builds cleanly), and deploys
never overlap (`concurrency`).

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

## Known gaps

- **Nothing forces code through CI before it deploys.** The release branches have no branch
  protection: pull requests run the tests, but a direct push would deploy untested code. Fix: a
  branch ruleset requiring a pull request and passing checks.
- **The signature isn't verified at deploy time.** The image is signed, but the deploy job doesn't
  run `cosign verify` before copying it, so the signature currently proves origin only to someone
  who checks it.
- **No automatic rollback.** A failed smoke test fails the job, but the new revision keeps serving.
  Cloud Run keeps earlier revisions, so rolling back is one command, done by hand.
- **Migrations must tolerate the old code** for the seconds between the migration and the new
  revision taking traffic. Nothing enforces that discipline yet (add first, remove in a later
  release).

## Questions and answers

**What would an attacker need to deploy to the project?**
Write access to a release branch of one of the three repositories. There is no key to steal: Google
issues a deploy credential only to a workflow running on those branches.

**What's the difference between deploying a tag and deploying a digest?**
A tag is a movable name; a digest identifies the exact content. Deploying by digest guarantees the
service runs the image that was built, signed and copied in this run.

**What happens to the live site if a migration fails?**
Nothing: the job stops before the service is updated, and the previous revision keeps serving.

**Where do the app's settings and secrets come from, if the pipeline only sets the image?**
Terraform (`deploy/cloudrun`) defines each service's environment, secret references, limits and
identity. The pipeline and Terraform each own one thing, so they don't overwrite each other.

**How long does a deploy take, and what does it cost?**
A few minutes end to end, on GitHub's free Actions minutes for public repositories; the registry
and Cloud Run stay inside their free tiers.
