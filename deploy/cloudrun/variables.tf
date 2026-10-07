variable "gcp_project" {
  type        = string
  description = "The Google Cloud project ID."
}

variable "region" {
  type        = string
  default     = "us-east4"
  description = "Cloud Run region. us-east4 (N. Virginia) sits next to the databases and gets the free North America egress."
}

variable "neon_org_id" {
  type        = string
  description = "The Neon organization that owns the projects (Organization settings in the Neon console)."
}

variable "neon_region" {
  type    = string
  default = "aws-us-east-1"
}

variable "cloudflare_account_id" { type = string }
variable "cloudflare_zone_id" { type = string }

variable "domain" {
  type        = string
  description = "The zone's name, e.g. martinomwenga.com. Lighthouse serves the apex; demos get subdomains."
}

variable "github_owner" {
  type    = string
  default = "MrtnOmwenga"
}

variable "github_repos" {
  type        = map(object({ repo = string, branch = string }))
  description = "Which GitHub repository (and which of its branches) deploys which service."
  default = {
    lighthouse = { repo = "lighthouse", branch = "main" }
    redacted   = { repo = "RBAC-API", branch = "master" }
    ghostchat  = { repo = "GhostChat", branch = "master" }
  }
}

variable "github_client_id" {
  type        = string
  description = "The GitHub OAuth app for Lighthouse's owner sign-in."
}

variable "github_client_secret" {
  type      = string
  sensitive = true
}

variable "owner_github_id" {
  type    = number
  default = 103695661
}

variable "ghostchat_mongodb_uri" {
  type        = string
  sensitive   = true
  description = "MongoDB Atlas connection string for GhostChat (a user limited to the ghostchat database)."
}

variable "tick_schedule" {
  type        = string
  default     = "*/15 * * * *"
  description = "How often Cloud Scheduler asks Lighthouse to run due checks. Neon's free plan allows 100 compute-hours a month per project; ticks every 15 minutes let the database sleep most of the time."
}

variable "alert_email" {
  type        = string
  default     = ""
  description = "Where security alerts go (a refused sign-in, a rejected scheduler call). Empty: no alerts."
}
