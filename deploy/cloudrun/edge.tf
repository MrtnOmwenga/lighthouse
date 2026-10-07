# Cloudflare in front: DNS, TLS, caching and the edge Worker, which routes each hostname to its
# Cloud Run service. The DNS records only exist so the hostnames resolve to Cloudflare; the Worker
# answers before any origin is contacted (100:: is the documentation "discard" address).

locals {
  hosts = {
    lighthouse = var.domain
    redacted   = "redacted.${var.domain}"
    ghostchat  = "ghostchat.${var.domain}"
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
      name = "SECRET_HOSTS"
      text = jsonencode([local.hosts.lighthouse])
    },
    {
      type = "secret_text"
      name = "EDGE_SECRET"
      text = random_password.edge.result
    },
  ]
}

resource "cloudflare_workers_route" "app" {
  for_each = local.hosts
  zone_id  = var.cloudflare_zone_id
  pattern  = "${each.value}/*"
  script   = cloudflare_workers_script.edge.script_name
}
