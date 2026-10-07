# Lighthouse reports a few numbers to Grafana Cloud at the end of every round of checks
# (internal/metrics). This file is what is done with them: a dashboard, and two alerts.
#
# The first alert is the point. Lighthouse watches the other apps and can't report its own
# outage; Grafana Cloud is outside it, and notices when the numbers stop arriving.
#
# The provider signs in with GRAFANA_URL and GRAFANA_AUTH (a service account token).

provider "grafana" {}

locals {
  metrics_on = var.metrics_push_url != ""
  prometheus = "grafanacloud-prom" # the stack's own Prometheus data source
  expression = "__expr__"
}

resource "grafana_folder" "lighthouse" {
  count = local.metrics_on ? 1 : 0
  title = "Lighthouse"
}

resource "grafana_contact_point" "owner" {
  count = local.metrics_on && var.alert_email != "" ? 1 : 0
  name  = "Lighthouse owner"
  email {
    addresses = [var.alert_email]
  }
}

resource "grafana_rule_group" "lighthouse" {
  count            = local.metrics_on && var.alert_email != "" ? 1 : 0
  name             = "lighthouse"
  folder_uid       = grafana_folder.lighthouse[0].uid
  interval_seconds = 300

  # Rounds arrive every 15 minutes. None in 40 minutes means two were missed: the scheduler has
  # stopped calling, Lighthouse can't start, or it can't reach its database.
  rule {
    name           = "Lighthouse has stopped reporting"
    condition      = "C"
    for            = "0s"
    no_data_state  = "Alerting" # no numbers at all is exactly the case to hear about
    exec_err_state = "Error"
    annotations = {
      summary     = "No round of checks has reported for 40 minutes."
      description = "Lighthouse reports after every round (every 15 minutes). Check https://${var.domain}/readyz, then the Cloud Scheduler job and the service's logs."
    }
    data {
      ref_id         = "A"
      datasource_uid = local.prometheus
      relative_time_range {
        from = 2400
        to   = 0
      }
      model = jsonencode({ refId = "A", instant = true, expr = "count_over_time(lighthouse_tick_checks[40m])" })
    }
    data {
      ref_id         = "C"
      datasource_uid = local.expression
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({ refId = "C", type = "threshold", expression = "A", conditions = [{ evaluator = { type = "lt", params = [1] } }] })
    }
    notification_settings {
      contact_point = grafana_contact_point.owner[0].name
    }
  }

  # Any request Lighthouse answered with a server error in the last half hour.
  rule {
    name           = "Lighthouse answered with a server error"
    condition      = "C"
    for            = "0s"
    no_data_state  = "OK"
    exec_err_state = "Error"
    annotations = {
      summary     = "Lighthouse answered at least one request with a 5xx status in the last 30 minutes."
      description = "Each failure is logged with its request id: filter the service's logs for \"request failed\"."
    }
    data {
      ref_id         = "A"
      datasource_uid = local.prometheus
      relative_time_range {
        from = 1800
        to   = 0
      }
      model = jsonencode({ refId = "A", instant = true, expr = "sum(sum_over_time(lighthouse_http_requests{class=\"5xx\"}[30m]))" })
    }
    data {
      ref_id         = "C"
      datasource_uid = local.expression
      relative_time_range {
        from = 0
        to   = 0
      }
      model = jsonencode({ refId = "C", type = "threshold", expression = "A", conditions = [{ evaluator = { type = "gt", params = [0] } }] })
    }
    notification_settings {
      contact_point = grafana_contact_point.owner[0].name
    }
  }
}

locals {
  panel_source = { type = "prometheus", uid = local.prometheus }
  panels = [
    { title = "Monitors up", type = "stat", w = 6, expr = "sum(lighthouse_monitor_up)", legend = "" },
    { title = "Open incidents", type = "stat", w = 6, expr = "max(lighthouse_incidents_open)", legend = "" },
    { title = "Minutes since the last round", type = "stat", w = 6, expr = "(time() - max(timestamp(lighthouse_tick_checks))) / 60", legend = "" },
    { title = "Server errors, last 24 h", type = "stat", w = 6, expr = "sum(sum_over_time(lighthouse_http_requests{class=\"5xx\"}[24h]))", legend = "" },
    { title = "Each monitor (1 = up)", type = "timeseries", w = 12, expr = "lighthouse_monitor_up", legend = "{{monitor}}" },
    { title = "Checks per round", type = "timeseries", w = 12, expr = "lighthouse_tick_checks", legend = "checks" },
    { title = "How long a round takes (seconds)", type = "timeseries", w = 12, expr = "lighthouse_tick_duration_seconds", legend = "seconds" },
    { title = "Requests answered between rounds", type = "timeseries", w = 12, expr = "lighthouse_http_requests", legend = "{{class}}" },
  ]
}

resource "grafana_dashboard" "lighthouse" {
  count  = local.metrics_on ? 1 : 0
  folder = grafana_folder.lighthouse[0].uid
  config_json = jsonencode({
    uid           = "lighthouse"
    title         = "Lighthouse"
    schemaVersion = 39
    time          = { from = "now-24h", to = "now" }
    refresh       = "5m"
    panels = [for i, p in local.panels : {
      id         = i + 1
      title      = p.title
      type       = p.type
      datasource = local.panel_source
      gridPos    = { h = p.type == "stat" ? 5 : 8, w = p.w, x = p.type == "stat" ? i * 6 : (i % 2) * 12, y = p.type == "stat" ? 0 : 5 + floor((i - 4) / 2) * 8 }
      targets    = [{ refId = "A", datasource = local.panel_source, expr = p.expr, legendFormat = p.legend }]
    }]
  })
}
