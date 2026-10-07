# The three apps on Cloud Run. Each scales to zero when idle and runs at most one instance: the
# demos keep real-time state in memory, and one instance keeps everything inside the free tier.
# Terraform creates each service with a placeholder image; from then on, each repository's release
# workflow deploys its own image, so the image is ignored here.

locals {
  placeholder = "us-docker.pkg.dev/cloudrun/container/hello"
  public_url  = "https://${var.domain}"

  lighthouse_db = "postgres://lighthouse_api@${local.lighthouse_host}/lighthouse?sslmode=require"
  redacted_db   = "postgres://rbac_app_login@${local.redacted_host}/rbac?sslmode=require"

  env = {
    lighthouse = {
      LIGHTHOUSE_ENV         = "production"
      PUBLIC_URL             = local.public_url
      OWNER_NAME             = "Martin Omwenga"
      CLIENT_IP_HEADER       = "X-Client-IP" # set by the edge Worker; trusted because of EDGE_SECRET
      CHECK_WORKERS          = "8"
      SANDBOX_LIMIT_PER_HOUR = "6"
      GITHUB_CLIENT_ID       = var.github_client_id
      OWNER_GITHUB_ID        = tostring(var.owner_github_id)
      SCHEDULE               = "external"
      CONFIRM_SECONDS        = "15"   # a failed check is re-checked 15 s later, so an outage is confirmed within a minute
      WARM_THRESHOLD_MS      = "1000" # a slower answer is a demo waking up: recorded apart, then checked again
      TICK_CALLER            = google_service_account.scheduler.email
      DATABASE_URL           = local.lighthouse_db
      EDGE_SECRET            = random_password.edge.result
      # Incident emails: sent from smtp_username to alert_email when a public monitor goes down
      # or recovers. Lighthouse sends none unless ALERT_TO is set.
      SMTP_HOST     = var.smtp_username == "" ? "" : var.smtp_host
      SMTP_PORT     = "587"
      SMTP_USERNAME = var.smtp_username
      ALERT_FROM    = var.smtp_username
      ALERT_TO      = var.smtp_username == "" ? "" : var.alert_email
      # Where each round of checks reports its figures (grafana.tf says what is done with them).
      METRICS_PUSH_URL  = var.metrics_push_url
      METRICS_PUSH_USER = var.metrics_push_user
    }
    redacted = {
      NODE_ENV              = "production"
      DATABASE_URL          = local.redacted_db
      DEMO_MODE             = "true"
      RATE_LIMIT_PER_MINUTE = "300"
      LOG_LEVEL             = "info"
      JWT_SECRET            = random_password.redacted_jwt.result
    }
    ghostchat = {
      NODE_ENV       = "production"
      SECURE_COOKIES = "true"
      CORS_ORIGINS   = "https://ghostchat.${var.domain}"
      JWT_SECRET     = random_password.ghostchat_jwt.result
    }
  }
  # Environment variables read from Secret Manager: the variable's name, and the secret it comes from.
  secret_env = {
    lighthouse = {
      PGPASSWORD           = google_secret_manager_secret.s["lighthouse-db-app"].secret_id
      GITHUB_CLIENT_SECRET = google_secret_manager_secret.s["lighthouse-github-secret"].secret_id
      SMTP_PASSWORD        = google_secret_manager_secret.s["lighthouse-smtp-password"].secret_id
      METRICS_PUSH_TOKEN   = google_secret_manager_secret.s["lighthouse-metrics-token"].secret_id
    }
    redacted  = { PGPASSWORD = google_secret_manager_secret.s["redacted-db-app"].secret_id }
    ghostchat = { MONGODB_URI = google_secret_manager_secret.s["ghostchat-mongodb-uri"].secret_id }
  }
  shape = {
    lighthouse = { port = 8080, health = "/readyz", memory = "256Mi", timeout = "300s", args = ["serve"] }
    redacted   = { port = 3000, health = "/health/ready", memory = "512Mi", timeout = "3600s", args = ["dist/main.js"] }
    ghostchat  = { port = 5000, health = "/health", memory = "512Mi", timeout = "3600s", args = [] }
  }
}

resource "google_cloud_run_v2_service" "app" {
  for_each            = local.services
  name                = each.key
  location            = var.region
  ingress             = "INGRESS_TRAFFIC_ALL"
  deletion_protection = false

  template {
    service_account = google_service_account.run[each.key].email
    timeout         = local.shape[each.key].timeout # WebSockets stay open this long, then reconnect
    # Off: with one instance there is nothing to stick to, and turning it on makes Google's front
    # end set a 30-day cookie (GAESA) on every visitor, which the privacy page says doesn't happen.
    # It becomes useful, for the demos' WebSockets, only with more than one instance.
    session_affinity                 = false
    max_instance_request_concurrency = 250

    scaling {
      min_instance_count = 0
      max_instance_count = 1
    }

    containers {
      image = local.placeholder
      args  = local.shape[each.key].args
      ports {
        container_port = local.shape[each.key].port
      }
      resources {
        limits   = { cpu = "1", memory = local.shape[each.key].memory }
        cpu_idle = true # billed only while handling requests
      }
      dynamic "env" {
        for_each = local.env[each.key]
        content {
          name  = env.key
          value = env.value
        }
      }
      dynamic "env" {
        for_each = local.secret_env[each.key]
        content {
          name = env.key
          value_source {
            secret_key_ref {
              secret  = env.value
              version = "latest"
            }
          }
        }
      }
      startup_probe {
        http_get {
          path = local.shape[each.key].health
        }
        initial_delay_seconds = 0
        period_seconds        = 3
        timeout_seconds       = 3
        failure_threshold     = 20
      }
    }
  }

  lifecycle {
    ignore_changes = [
      template[0].containers[0].image, # the release workflow deploys images
      client,
      client_version,
    ]
  }
  depends_on = [google_secret_manager_secret_iam_member.reader, google_secret_manager_secret_version.s]
}

# Everyone may call the services; Lighthouse itself refuses anything that didn't come through the
# edge (EDGE_SECRET), and the demos are public by design.
resource "google_cloud_run_v2_service_iam_member" "public" {
  for_each = local.services
  name     = google_cloud_run_v2_service.app[each.key].name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Migrations run as one-off jobs before each deploy: the database owner applies them and creates
# the app's least-privilege login role.
locals {
  migrations = {
    lighthouse = {
      args = ["migrate"]
      env = {
        MIGRATE_DATABASE_URL = "postgres://lighthouse_owner@${local.lighthouse_host}/lighthouse?sslmode=require"
        DATABASE_URL         = local.lighthouse_db
      }
      secrets = {
        PGPASSWORD      = google_secret_manager_secret.s["lighthouse-db-owner"].secret_id
        APP_DB_PASSWORD = google_secret_manager_secret.s["lighthouse-db-app"].secret_id
      }
    }
    redacted = {
      args = ["dist/database/migrate.js"]
      env = {
        MIGRATION_DATABASE_URL = "postgres://rbac_owner@${local.redacted_host}/rbac?sslmode=require"
        DATABASE_URL           = local.redacted_db
      }
      secrets = {
        PGPASSWORD      = google_secret_manager_secret.s["redacted-db-owner"].secret_id
        APP_DB_PASSWORD = google_secret_manager_secret.s["redacted-db-app"].secret_id
      }
    }
  }
}

resource "google_cloud_run_v2_job" "migrate" {
  for_each            = local.migrations
  name                = "${each.key}-migrate"
  location            = var.region
  deletion_protection = false

  template {
    task_count = 1
    template {
      service_account = google_service_account.run[each.key].email
      max_retries     = 0
      timeout         = "300s"
      containers {
        image = local.placeholder
        args  = each.value.args
        resources {
          limits = { cpu = "1", memory = "512Mi" }
        }
        dynamic "env" {
          for_each = each.value.env
          content {
            name  = env.key
            value = env.value
          }
        }
        dynamic "env" {
          for_each = each.value.secrets
          content {
            name = env.key
            value_source {
              secret_key_ref {
                secret  = env.value
                version = "latest"
              }
            }
          }
        }
      }
    }
  }

  lifecycle {
    ignore_changes = [template[0].template[0].containers[0].image, client, client_version]
  }
  depends_on = [google_secret_manager_secret_iam_member.reader, google_secret_manager_secret_version.s]
}
