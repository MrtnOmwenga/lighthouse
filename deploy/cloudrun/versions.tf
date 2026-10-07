terraform {
  required_version = ">= 1.10"
  required_providers {
    google     = { source = "hashicorp/google", version = "~> 8.4" }
    cloudflare = { source = "cloudflare/cloudflare", version = "~> 5.26" }
    neon       = { source = "kislerdm/neon", version = "~> 0.18" }
    random     = { source = "hashicorp/random", version = "~> 3.9" }
    infisical  = { source = "infisical/infisical", version = ">= 0.15" }
    grafana    = { source = "grafana/grafana", version = ">= 3.0" }
  }
  # State lives in a Cloud Storage bucket in the same project (versioned, private; created once by
  # hand, see deploy/README.md). See backend.hcl.example.
  backend "gcs" {}
}

provider "google" {
  project = var.gcp_project
  region  = var.region
}

provider "cloudflare" {} # CLOUDFLARE_API_TOKEN
provider "neon" {}       # NEON_API_KEY
