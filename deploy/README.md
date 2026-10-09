# Deploying Lighthouse and the demos

There are two ways to run everything, from the same images:

- **Cloud Run (live):** [`cloudrun/`](cloudrun/). Three services that scale to zero, behind a
  Cloudflare edge. This is what serves `martinomwenga.com` today, inside free tiers.
- **Kubernetes:** [`terraform/`](terraform/) and [`k8s/`](k8s/). One k3s host with no open
  inbound ports, kept in step by Flux. Proven end to end on a local kind cluster; not the live
  deployment (see [why](#why-cloud-run-and-not-the-k3s-host)).

## Cloud Run (live)

```
                         Cloudflare edge (DNS, TLS, caching)
visitors ──▶ martinomwenga.com ──┐   Worker: routes by hostname, adds the visitor's IP
             redacted.… ─────────┼─▶ and a secret header, caches static files
             ghostchat.… ────────┘        │
                                          ▼
                    Cloud Run (us-east4, scale to zero, one instance each)
                    ├─ lighthouse ──▶ Neon Postgres (project "lighthouse")
                    ├─ redacted ────▶ Neon Postgres (project "redacted")
                    └─ ghostchat ───▶ MongoDB Atlas M0
Cloud Scheduler ── every 15 min, Google-signed token ──▶ lighthouse POST /internal/tick
GitHub Actions ── OIDC, no stored keys ──▶ Artifact Registry ──▶ migrate job ──▶ deploy by digest
```

| File | What |
|---|---|
| `services.tf` | The three services (one instance max, request-based billing, session affinity for WebSockets) and the two migration jobs |
| `scheduler.tf` | Lighthouse's clock: an idle Cloud Run instance gets no CPU, so Cloud Scheduler calls `/internal/tick` |
| `databases.tf` | Two Neon projects, one per app |
| `secrets.tf` | Secret Manager: the credentials, each readable only by its own service |
| `infisical.tf` | Reads the secrets a person had to obtain from Infisical, as ephemeral values that never reach Terraform's state |
| `backups.tf` | Nightly backups of the three databases to a bucket, a weekly restore test, and an alert when either fails (`backup/backup.sh` is the script) |
| `grafana.tf` | What is done with the figures Lighthouse reports after each round: a dashboard, an alert when they stop arriving, an alert on server errors |
| `alerts.tf` | An email when a sign-in is refused or a scheduler call is rejected (a log-based alert) |
| `identity.tf` | A service account per service, the scheduler's caller identity, and keyless deploys from GitHub |
| `registry.tf` | Artifact Registry, with a cleanup policy |
| `edge.tf`, `edge/worker.js` | The Cloudflare Worker, its routes and DNS |

### Staying inside the free tiers

Every choice below exists because a free allowance has a limit:

| Limit | Choice |
|---|---|
| Cloud Run: 180,000 vCPU-seconds, 360,000 GiB-seconds, 2M requests a month; us-east4 is a Tier 1 region | Scale to zero, CPU only while handling requests, one instance per service |
| Neon: **100 compute-hours a month per project**, a compute sleeps after 5 idle minutes | A project per app, and ticks every **15 minutes**, not every minute: the database then sleeps most of the time (roughly 60–70 hours a month instead of the ~180 an always-awake one would use) |
| Secret Manager: 6 active secret versions | Seven are kept, one over: both apps' owner and app database passwords, the OAuth client secret, the MongoDB URI, and the mailbox password for incident emails (about $0.06 a month). The demos' signing keys and the edge secret are plain environment variables. |
| Artifact Registry: 0.5 GB | The two newest versions of each image are kept (about 290 MB); only the amd64 image is copied |
| Google egress: 1 GB a month within North America | The Worker caches static files at the edge, so repeat downloads never reach Google |
| Cloud Scheduler: 3 jobs per billing account | One |

A budget alert on the billing account is the safety net.

### The CV

The CV names employers and clients, which these repositories don't, so the file isn't in the
repository. It lives in a small public bucket (`public.tf`) and `site.yaml` links to it. To
replace it:

```sh
gcloud storage cp CV.pdf gs://<project>-public/martin-omwenga-cv.pdf --cache-control="public, max-age=300" --content-type=application/pdf
```

### Security

- **Nothing bypasses the edge.** Cloud Run URLs (`*.run.app`) are public. Each service runs with
  an `EDGE_SECRET` of its own: any request without the Worker's secret header is refused (health
  checks and Lighthouse's tick excepted), so the Cloudflare layer can't be skipped and the visitor
  IP it passes on can't be forged. The secrets differ, so a service can't use the one it receives
  to pass as the edge to another.
- **A clock for services that are asleep.** A timer inside a service that has scaled to zero
  doesn't fire. The Worker has a Cloudflare cron trigger: each hour it calls the demos' own
  clean-up paths (`/internal/…`), with that service's secret. No visitor's request reaches a path
  under `/internal/`: the Worker answers 404 itself.
- **The tick needs a Google-signed identity token** for Lighthouse's audience, issued to the
  scheduler's service account. Lighthouse verifies it with the standard library
  ([`internal/oidc`](../internal/oidc/oidc.go)): RS256 only, issuer, audience, expiry, the caller's
  email, key rotation. Anything else gets a 404.
- **No cloud keys anywhere.** Each repository's release workflow exchanges GitHub's OIDC token for
  a short-lived Google one (Workload Identity Federation). Google accepts it only from the three
  repositories, each only from its own release branch.
- **Least privilege:** each service's account reads only its own secrets. The deployer can push
  images, update the services and run the migration jobs, and act as the three runtime accounts
  to do so. The apps connect as least-privilege database roles their migrations create; the owner
  password is injected only into the migration jobs.

### What you need first

1. **A domain on Cloudflare** (free plan): its zone ID, your account ID, and an API token with
   *Zone:DNS:Edit* and *Zone:Workers Routes:Edit* on the zone, and *Account:Workers Scripts:Edit*.
2. **Google Cloud:** a project with billing enabled (the free tier needs a billing account), a
   budget alert, and `gcloud auth application-default login`.
3. **Neon:** an account (free plan) and an API key; the organization ID from its settings.
4. **MongoDB Atlas:** a free M0 cluster in AWS us-east-1, a user with readWrite on `ghostchat` only,
   and network access from anywhere (`0.0.0.0/0`: Cloud Run has no fixed address; the password and
   TLS protect the connection). Network access is set per Atlas *project*, not per cluster.
5. **A GitHub OAuth app** for the owner sign-in: callback `https://<domain>/auth/github/callback`.
6. **Terraform state:** a Cloud Storage bucket in the same project, created once by hand because
   Terraform can't keep its state in a bucket it has yet to create:
   `gcloud storage buckets create gs://<project>-tfstate --location us-east1 --uniform-bucket-level-access --public-access-prevention`
   then `gcloud storage buckets update gs://<project>-tfstate --versioning` (see `backend.hcl.example`).
7. **Infisical:** a project holding the secrets, and a machine identity that can read it.
8. **Optional:** a Grafana Cloud stack (metrics, a dashboard, the alert when Lighthouse stops
   reporting) and a mailbox for incident emails.

### Steps

```sh
cd deploy/cloudrun
cp terraform.tfvars.example terraform.tfvars   # project, Neon org, Cloudflare IDs, domain, OAuth client ID
cp backend.hcl.example backend.hcl
gcloud auth application-default login          # Google resources, and the state bucket
export CLOUDFLARE_API_TOKEN=... NEON_API_KEY=...
export INFISICAL_UNIVERSAL_AUTH_CLIENT_ID=... INFISICAL_UNIVERSAL_AUTH_CLIENT_SECRET=...   # a machine identity that can read the project
export GRAFANA_URL=... GRAFANA_AUTH=...          # only with metrics_push_url set
terraform init -backend-config=backend.hcl
terraform apply            # the services start with a placeholder image
terraform output github_actions
```

Set the four `github_actions` outputs as **Actions variables** (not secrets: none is a credential)
in each of the three repositories, then push to each release branch: the release workflow copies
the image, runs the migrations and deploys. Finally, sign in at `https://<domain>/console/` and add
HTTP monitors named `Lighthouse`, `Redacted` and `GhostChat` (their names become the slugs the site
content refers to) for `/readyz`, `/health/ready` and `/health`, checked every 900 seconds.

### Operating it

- **Deploying:** push to the release branch. Build, sign, copy, migrate, deploy by digest, smoke
  test: [`release.yml`](../.github/workflows/release.yml).
- **Watching:** Lighthouse itself, and `gcloud run services logs read <service> --region us-east4`.
- **Health checks:** use `/readyz`. Cloud Run's front end reserves `/healthz` for itself and answers
  404 to it, so it only works inside the container.
- **Backups:** Neon keeps six hours of point-in-time history on the free plan; restore a branch
  from the Neon console. The nightly `pg_dump` exists only on the Kubernetes path. The demos' data
  is disposable by design.
- **Rotating a secret:** change it in Terraform (or `terraform apply -replace=random_password.<name>`
  for a generated one), then redeploy. For database passwords, `ALTER ROLE` first.

## Kubernetes (k3s)

The same apps on one k3s host: every pod non-root, read-only, without capabilities, under
restricted Pod Security, behind default-deny network policies. Visitors arrive through a
Cloudflare Tunnel and SSH through a second one behind Cloudflare Access, so the host has no open
inbound ports. Flux applies `k8s/production` and commits each new image tag.

```
visitors ──▶ Cloudflare (TLS, DNS) ──tunnel──▶ cloudflared ─┬─▶ lighthouse   (namespace lighthouse, + PostgreSQL)
                                                            ├─▶ redacted     (namespace demos, + PostgreSQL)
                                                            └─▶ ghostchat    (namespace demos, + MongoDB, Redis)
GitHub Actions ──▶ GHCR (multi-arch images, signed) ──▶ Flux (commits new tags, applies deploy/k8s/production)
```

| Folder | What |
|---|---|
| `terraform/` | An Oracle Cloud Always Free Arm VM with no inbound rules (cloud-init installs k3s from a checksum-verified binary, and cloudflared for SSH), the two tunnels, DNS, the Access policy in front of SSH, and a backups bucket with a 30-day expiry |
| `k8s/base/` | The workloads, network policies and the nightly backup |
| `k8s/production/` | Per-deployment settings (`lighthouse.env`, `ghostchat.env`) and the image tags Flux updates |
| `k8s/flux/` | What Flux applies, and the image automation |
| `k8s/scripts/create-secrets.sh` | Creates the secrets (passwords generated on the spot, never written to disk) |

CI renders the manifests and validates them (kubeconform), checks every container is locked down,
and validates both Terraform stacks. On a local kind cluster every pod ran under restricted Pod
Security and the network policies blocked what they should.

To run it on a machine of your own instead of Oracle, skip `terraform/`: install k3s, create a
Cloudflare tunnel for its token (cloudflared runs inside the cluster), and start from step 3.

```sh
# 1. The machine and the tunnels (Oracle).
cd deploy/terraform && terraform init -backend-config=backend.hcl && terraform apply
# 2. SSH through the admin tunnel (~/.ssh/config: ProxyCommand cloudflared access ssh --hostname %h),
#    then fetch the kubeconfig and forward the API server.
ssh lighthouse sudo cat /etc/rancher/k3s/k3s.yaml > ~/.kube/lighthouse.yaml
ssh -N -L 6443:127.0.0.1:6443 lighthouse
# 3. Settings: deploy/k8s/production/lighthouse.env and ghostchat.env. Commit them.
# 4. Namespaces, then secrets.
kubectl apply -f deploy/k8s/base/namespaces.yaml
TUNNEL_TOKEN=... GITHUB_CLIENT_SECRET=... BACKUP_ENDPOINT=... BACKUP_BUCKET=... \
BACKUP_ACCESS_KEY_ID=... BACKUP_SECRET_ACCESS_KEY=... deploy/k8s/scripts/create-secrets.sh
# 5. Flux.
flux bootstrap github --owner=MrtnOmwenga --repository=lighthouse --branch=main \
  --path=deploy/k8s/flux --personal --components-extra=image-reflector-controller,image-automation-controller
```

Make the GHCR packages public once (or give the cluster a pull secret). Backups: Lighthouse's
database is dumped nightly; restore with `pg_restore --clean` from a pod in the `lighthouse`
namespace.

## Why Cloud Run, and not the k3s host

The plan was the Oracle host: free, and a whole Kubernetes platform. It never came up:

- **Oracle's account setup never finished.** The welcome notice said billing was "still being set
  up"; a day later the tenancy had no support identifier (so no way to open a support request) and
  the billing API answered `SubscriptionNotFound`. The Pay As You Go upgrade couldn't start.
- **No free Arm capacity.** Oracle's capacity report for the region said `OUT_OF_HOST_CAPACITY` for
  every size, down to 1 CPU and 6 GB, in every fault domain; 32 retries over a morning agreed. Free
  accounts are served last, and the account also turned out to be limited to 2 CPUs and 12 GB.

AWS was ruled out on cost (for an account past its first year, the server, its public IPv4
address and its disk cost $16–28 a month; free serverless options can't hold WebSockets without
rewriting the demos). Hetzner (€4.99 a month) would have kept k3s, but can require identity
documents. Cloud Run runs the existing containers, WebSockets included, within its free tier.

## What deploying for real found

`terraform validate`, CI and the local cluster can't catch what only real accounts reject:

- **OCI state lock:** `NotImplemented: AWS chunked encoding not supported`. Newer AWS SDKs send
  chunked checksums that OCI's S3 API refuses; set `AWS_REQUEST_CHECKSUM_CALCULATION` and
  `AWS_RESPONSE_CHECKSUM_VALIDATION` to `when_required`. `skip_s3_checksum` alone isn't enough.
- **OCI lifecycle rule:** `InsufficientServicePermissions`. Object Storage expires objects as a
  service principal, which needs an IAM policy even in your own tenancy.
- **A new Google project's first Cloud Run deploy** failed with a generic "internal error" (code
  7): Cloud Run's service agent is created when the API is enabled and takes a minute or two to
  exist. Retrying worked.
- **Neon's free compute is per project, and a monitor that checks every minute keeps a database
  awake around the clock** (~180 compute-hours at the smallest size, against 100). Hence a project
  per app and a 15-minute tick.
- **Keyless deploys and branch names:** the Workload Identity condition first allowed only `main`;
  two of the repositories release from `master`. It now names each repository with its own branch.
- **Secret scanners flagged secret *names*:** lines like `PASSWORD = "lighthouse-db-app"` look like
  password literals to gitleaks and GitGuardian. They're now references to the secret resources.
- **Atlas network access is per project:** a cluster recreated in a new project doesn't inherit
  the old project's access list, and GhostChat's first deploy couldn't connect.
- **Cloud Run reserves `/healthz`** (see *Health checks* above).
- **Google limits a workload identity provider's display name to 32 characters.**
