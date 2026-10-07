# Security events worth an email. Lighthouse logs each one as a structured line (msg "security",
# with an "event"); these alerts match on the log itself, so nothing has to run to raise them and
# they cost nothing. The email carries the matching log entry.
#
#   refused        a GitHub account other than the owner's tried to sign in to the console
#   sign_in_failed a sign-in that didn't complete (a forged or replayed link shows up here)
#   tick_rejected  something other than the scheduler called /internal/tick with a token
#
# Rate limits and requests that bypass the edge are logged too, and not alerted on: scanners
# produce those all day.

resource "google_monitoring_notification_channel" "owner_email" {
  count        = var.alert_email == "" ? 0 : 1
  display_name = "Owner (email)"
  type         = "email"
  labels       = { email_address = var.alert_email }
  depends_on   = [google_project_service.api]
}

resource "google_monitoring_alert_policy" "security_events" {
  count        = var.alert_email == "" ? 0 : 1
  display_name = "Lighthouse: sign-in refused or scheduler call rejected"
  combiner     = "OR"
  conditions {
    display_name = "A security event was logged"
    condition_matched_log {
      filter = <<-EOT
        resource.type="cloud_run_revision"
        resource.labels.service_name="${google_cloud_run_v2_service.app["lighthouse"].name}"
        jsonPayload.msg="security"
        jsonPayload.event=("refused" OR "sign_in_failed" OR "tick_rejected")
      EOT
    }
  }
  alert_strategy {
    # At most one email an hour, however many events; the incident closes itself after a day.
    notification_rate_limit { period = "3600s" }
    auto_close = "86400s"
  }
  notification_channels = [google_monitoring_notification_channel.owner_email[0].id]
  documentation {
    content   = "Open the console's Security screen (https://${var.domain}/console/security) to see who tried. Nothing was let in: these are refusals."
    mime_type = "text/markdown"
  }
}
