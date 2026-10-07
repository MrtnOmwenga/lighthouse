# Nightly backups of all three databases, and a weekly test that they can be restored.
#
# Neon's free plan keeps six hours of history and Atlas's free tier keeps none, so without this a
# bad migration or a deleted project would be the end of the data.
#
#   Objective: lose at most 24 hours of data; the restore test records how long a restore takes.
#
# Both run as Cloud Run jobs in the stock PostgreSQL image, pinned by digest, with the script
# passed as an argument: there is no image of our own to build or keep. Cloud Scheduler starts
# them (with the tick, that is the free tier's three jobs). The bucket is in us-east1, where the
# free tier's 5 GB applies; it keeps every version of an object, and deletes anything older than
# 30 days.

locals {
  backup_image  = "docker.io/library/postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24" # postgres:17-alpine, to match the servers
  backup_script = file("${path.module}/backup/backup.sh")
  backup_jobs = {
    backup       = { schedule = "17 2 * * *", description = "Nightly backups of the three databases" }
    restore-test = { schedule = "47 3 * * 0", description = "Weekly: restore the latest backups into a scratch database and check them" }
  }
  # Environment variable => the Secret Manager secret it is read from.
  backup_secrets = {
    LIGHTHOUSE_PASSWORD = google_secret_manager_secret.s["lighthouse-db-owner"]
    REDACTED_PASSWORD   = google_secret_manager_secret.s["redacted-db-owner"]
    MONGODB_URI         = google_secret_manager_secret.s["ghostchat-mongodb-uri"]
  }
}

resource "google_storage_bucket" "backups" {
  name                        = "${var.gcp_project}-backups"
  location                    = "US-EAST1"
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = false
  versioning {
    enabled = true
  }
  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type = "Delete"
    }
  }
  lifecycle_rule {
    condition {
      days_since_noncurrent_time = 30
    }
    action {
      type = "Delete"
    }
  }
  depends_on = [google_project_service.api]
}

# Its own identity: it can read the three database credentials and use this bucket, nothing else.
resource "google_service_account" "backup" {
  account_id   = "backup"
  display_name = "Backups and restore tests"
  depends_on   = [google_project_service.api]
}

resource "google_secret_manager_secret_iam_member" "backup" {
  for_each  = local.backup_secrets
  secret_id = each.value.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.backup.email}"
}

resource "google_storage_bucket_iam_member" "backup" {
  bucket = google_storage_bucket.backups.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.backup.email}"
}

resource "google_cloud_run_v2_job" "backup" {
  for_each            = local.backup_jobs
  name                = each.key
  location            = var.region
  deletion_protection = false

  template {
    task_count = 1
    template {
      service_account = google_service_account.backup.email
      max_retries     = 1
      timeout         = "900s"
      containers {
        image   = local.backup_image
        command = ["sh"]
        args    = ["-c", local.backup_script, "backup.sh", each.key]
        resources {
          limits = { cpu = "1", memory = "1Gi" }
        }
        env {
          name  = "BUCKET"
          value = google_storage_bucket.backups.name
        }
        env {
          name  = "LIGHTHOUSE_URL"
          value = "postgres://lighthouse_owner@${local.lighthouse_host}/lighthouse?sslmode=require"
        }
        env {
          name  = "REDACTED_URL"
          value = "postgres://rbac_owner@${local.redacted_host}/rbac?sslmode=require"
        }
        env {
          name  = "REQUIRE" # the restore test fails if a database's dump is missing
          value = "all"
        }
        dynamic "env" {
          for_each = local.backup_secrets
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = env.value.secret_id
                version = "latest"
              }
            }
          }
        }
      }
    }
  }
  depends_on = [google_secret_manager_secret_iam_member.backup, google_secret_manager_secret_version.s]
}

# The scheduler's account may start these two jobs.
resource "google_cloud_run_v2_job_iam_member" "scheduler_runs_backup" {
  for_each = local.backup_jobs
  name     = google_cloud_run_v2_job.backup[each.key].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.scheduler.email}"
}

resource "google_cloud_scheduler_job" "backup" {
  for_each         = local.backup_jobs
  name             = each.key
  region           = var.region
  description      = each.value.description
  schedule         = each.value.schedule
  time_zone        = "Etc/UTC"
  attempt_deadline = "180s" # starting the job, not running it

  http_target {
    http_method = "POST"
    uri         = "https://run.googleapis.com/v2/projects/${var.gcp_project}/locations/${var.region}/jobs/${google_cloud_run_v2_job.backup[each.key].name}:run"
    oauth_token {
      service_account_email = google_service_account.scheduler.email
    }
  }
  depends_on = [google_cloud_run_v2_job_iam_member.scheduler_runs_backup]
}

# An email when a backup or a restore test says it failed.
resource "google_monitoring_alert_policy" "backups" {
  count        = var.alert_email == "" ? 0 : 1
  display_name = "Backups: a backup or a restore test failed"
  combiner     = "OR"
  conditions {
    display_name = "A backup job reported a failure"
    condition_matched_log {
      filter = <<-EOT
        resource.type="cloud_run_job"
        resource.labels.job_name=("backup" OR "restore-test")
        textPayload:("BACKUP FAILED" OR "RESTORE TEST FAILED")
      EOT
    }
  }
  alert_strategy {
    notification_rate_limit { period = "3600s" }
    auto_close = "1800s"
  }
  notification_channels = [google_monitoring_notification_channel.owner_email[0].id]
  documentation {
    content   = "The failing line says what went wrong. Run it again by hand with: gcloud run jobs execute backup --region ${var.region} (or restore-test)."
    mime_type = "text/markdown"
  }
}
