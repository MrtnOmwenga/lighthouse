# Credentials live in Secret Manager; each service reads only its own (and its migration job, which
# runs as the same account). They come from two places:
#
#   - Generated here: the database passwords. Terraform creates them, so they are in its state
#     (encrypted at rest in a private bucket).
#   - Kept in Infisical (infisical.tf): everything a person had to obtain somewhere, such as the
#     OAuth client secret. Terraform reads those only while it runs and passes them to Secret
#     Manager through write-only arguments, so they are never written to its state or plans.
#     Changing a value in Infisical raises its version there, and the next apply copies it across.
#
# The services never talk to Infisical: if it is unreachable, nothing that is running notices.
#
# Secret Manager's free tier covers six secret versions; the seventh onward costs about $0.06 a
# month each. The demos' token-signing keys and the edge secret are plain environment variables
# instead (services.tf): only principals who can deploy to a service can read its configuration,
# and those can already run code that reads its secrets.

resource "random_password" "redacted_jwt" {
  length  = 64
  special = false
}

resource "random_password" "ghostchat_jwt" {
  length  = 64
  special = false
}

resource "random_password" "edge" {
  length  = 48
  special = false
}

resource "random_password" "edge_demo" {
  for_each = toset(["redacted", "ghostchat"])
  length   = 48
  special  = false
}

locals {
  generated = {
    lighthouse-db-owner = { service = "lighthouse", value = neon_project.lighthouse.database_password }
    lighthouse-db-app   = { service = "lighthouse", value = random_password.lighthouse_app.result }
    redacted-db-owner   = { service = "redacted", value = neon_project.redacted.database_password }
    redacted-db-app     = { service = "redacted", value = random_password.redacted_app.result }
  }
  # Secret Manager name => where the value is kept in Infisical.
  kept = {
    lighthouse-github-secret = { service = "lighthouse", folder = "/lighthouse", name = "GITHUB_OAUTH_CLIENT_SECRET" }
    lighthouse-smtp-password = { service = "lighthouse", folder = "/lighthouse", name = "SMTP_PASSWORD" }
    lighthouse-metrics-token = { service = "lighthouse", folder = "/lighthouse", name = "GRAFANA_PROM_TOKEN" }
    ghostchat-mongodb-uri    = { service = "ghostchat", folder = "/ghostchat", name = "MONGODB_URI" }
  }
  secrets = merge(local.generated, local.kept)

  # GhostChat connects to the database named in the URI; with none, Mongo's default ("test") is
  # one its user may not write to. Add /ghostchat when the URI names no database.
  mongodb_uri = ephemeral.infisical_secret.kept["ghostchat-mongodb-uri"].value
  kept_values = merge({ for k, v in ephemeral.infisical_secret.kept : k => v.value }, {
    ghostchat-mongodb-uri = can(regex("^mongodb(\\+srv)?://[^/]+/[^?/]+", local.mongodb_uri)) ? local.mongodb_uri : replace(local.mongodb_uri, "/^(mongodb(\\+srv)?://[^/?]+)/?/", "$${1}/ghostchat")
  })
}

resource "google_secret_manager_secret" "s" {
  for_each  = local.secrets
  secret_id = each.key
  replication {
    auto {}
  }
  depends_on = [google_project_service.api]
}

resource "google_secret_manager_secret_version" "s" {
  for_each = local.secrets
  secret   = google_secret_manager_secret.s[each.key].id
  # One or the other: a generated value, or a value kept in Infisical, written without being
  # stored, and written again whenever its version there changes.
  secret_data            = try(local.generated[each.key].value, null)
  secret_data_wo         = contains(keys(local.kept), each.key) ? local.kept_values[each.key] : null
  secret_data_wo_version = try(data.infisical_secret_metadata.kept[each.key].secret_version, null)
  lifecycle {
    create_before_destroy = true # the services read "latest": never leave a moment with none
  }
}

resource "google_secret_manager_secret_iam_member" "reader" {
  for_each  = local.secrets
  secret_id = google_secret_manager_secret.s[each.key].id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.run[each.value.service].email}"
}
