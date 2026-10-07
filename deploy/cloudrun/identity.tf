# Who can do what.
#
# - Each service runs as its own service account, which can read only its own secrets.
# - Cloud Scheduler calls Lighthouse's /internal/tick as a service account that has no roles at
#   all: Lighthouse checks the identity token Google issues for it.
# - GitHub Actions deploys through Workload Identity Federation: no key is ever stored. Only each
#   repository's release branch can get a token, and only for the deployer.

locals {
  services = toset(["lighthouse", "redacted", "ghostchat"])
}

resource "google_service_account" "run" {
  for_each     = local.services
  account_id   = "run-${each.key}"
  display_name = "Cloud Run: ${each.key}"
  depends_on   = [google_project_service.api]
}

resource "google_service_account" "scheduler" {
  account_id   = "lighthouse-tick"
  display_name = "Cloud Scheduler: calls Lighthouse's /internal/tick"
  depends_on   = [google_project_service.api]
}

resource "google_service_account" "deployer" {
  account_id   = "github-deployer"
  display_name = "GitHub Actions: pushes images and deploys"
  depends_on   = [google_project_service.api]
}

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github"
  display_name              = "GitHub Actions"
  depends_on                = [google_project_service.api]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-actions"
  display_name                       = "GitHub Actions (release branch)"
  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }
  # Only the named repositories, each from its own release branch. Checked by Google before any
  # token is issued.
  attribute_condition = join(" || ", [for r in values(var.github_repos) : "(assertion.repository == '${var.github_owner}/${r.repo}' && assertion.ref == 'refs/heads/${r.branch}')"])
  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

resource "google_service_account_iam_member" "deployer_from_github" {
  for_each           = var.github_repos
  service_account_id = google_service_account.deployer.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_owner}/${each.value.repo}"
}

# The deployer updates the services and runs the migration jobs (run.developer), and deploys them
# as their runtime accounts (serviceAccountUser on those accounts only).
resource "google_project_iam_member" "deployer_run" {
  project = var.gcp_project
  role    = "roles/run.developer"
  member  = "serviceAccount:${google_service_account.deployer.email}"
}

# So a failed migration's output can be shown in the workflow's log.
resource "google_project_iam_member" "deployer_reads_logs" {
  project = var.gcp_project
  role    = "roles/logging.viewer"
  member  = "serviceAccount:${google_service_account.deployer.email}"
}

resource "google_service_account_iam_member" "deployer_acts_as_runtime" {
  for_each           = local.services
  service_account_id = google_service_account.run[each.key].name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deployer.email}"
}
