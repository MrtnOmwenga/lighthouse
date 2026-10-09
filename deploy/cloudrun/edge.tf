# Cloudflare in front: DNS, TLS, caching and the edge Worker, which routes each hostname to its
# Cloud Run service. The DNS records only exist so the hostnames resolve to Cloudflare; the Worker
# answers before any origin is contacted (100:: is the documentation "discard" address).

locals {
  hosts = {
    lighthouse = var.domain
    redacted   = "redacted.${var.domain}"
    ghostchat  = "ghostchat.${var.domain}"
  }
  edge_secret = {
    lighthouse = random_password.edge.result
    redacted   = random_password.edge_demo["redacted"].result
    ghostchat  = random_password.edge_demo["ghostchat"].result
  }
  # Work that would otherwise wait on a timer inside an instance that isn't running.
  edge_cron = {
    redacted  = ["/internal/housekeeping"] # expired demo agencies and dead refresh tokens
    ghostchat = ["/internal/anchor"]       # the key log's daily timestamp, and upgrading pending ones
  }
}

resource "cloudflare_dns_record" "app" {
  for_each = local.hosts
  zone_id  = var.cloudflare_zone_id
  name     = each.value
  type     = "AAAA"
  content  = "100::"
  proxied  = true
  ttl      = 1
}

resource "cloudflare_workers_script" "edge" {
  account_id         = var.cloudflare_account_id
  script_name        = "portfolio-edge"
  content            = file("${path.module}/edge/worker.js")
  main_module        = "worker.js"
  compatibility_date = "2026-09-01"
  bindings = [
    {
      type = "plain_text"
      name = "ORIGINS"
      text = jsonencode({ for k, host in local.hosts : host => google_cloud_run_v2_service.app[k].uri })
    },
    {
      type = "plain_text"
      name = "PAGE_HOSTS" # whose public pages the edge keeps a copy of
      text = jsonencode([local.hosts.lighthouse])
    },
    {
      type = "secret_text"
      name = "EDGE_SECRETS" # one per host, so a service can't pass as the edge to another
      text = jsonencode(merge({ for k, host in local.hosts : host => local.edge_secret[k] }, var.guide_origin == "" ? {} : { guide = var.guide_edge_secret }))
    },
    {
      type = "plain_text"
      name = "GUIDE" # the site's assistant, a separate service: its one path is sent there
      text = var.guide_origin == "" ? "null" : jsonencode({ host = local.hosts.lighthouse, path = "/api/guide", origin = var.guide_origin })
    },
    {
      type = "plain_text"
      name = "CRON" # paths the Worker calls each hour: a clock for services that scale to zero
      text = jsonencode({ for k, paths in local.edge_cron : local.hosts[k] => paths })
    },
  ]
}

resource "cloudflare_workers_cron_trigger" "edge" {
  account_id  = var.cloudflare_account_id
  script_name = cloudflare_workers_script.edge.script_name
  schedules   = [{ cron = "17 * * * *" }]
}

resource "cloudflare_workers_route" "app" {
  for_each = local.hosts
  zone_id  = var.cloudflare_zone_id
  pattern  = "${each.value}/*"
  script   = cloudflare_workers_script.edge.script_name
}
