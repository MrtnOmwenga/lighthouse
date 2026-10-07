terraform {
  required_version = ">= 1.10"
  required_providers {
    google     = { source = "hashicorp/google", version = "~> 8.4" }
    cloudflare = { source = "cloudflare/cloudflare", version = "~> 5.26" }
    neon       = { source = "kislerdm/neon", version = "~> 0.18" }
    random     = { source = "hashicorp/random", version = "~> 3.9" }
    infisical  = { source = "infisical/infisical", version = ">= 0.15" }
  }
  # State lives in OCI Object Storage's S3-compatible API (free), next to the k3s stack's state
  # but under its own key. See backend.hcl.example.
  backend "s3" {}
}

provider "google" {
  project = var.gcp_project
  region  = var.region
}

provider "cloudflare" {} # CLOUDFLARE_API_TOKEN
provider "neon" {}       # NEON_API_KEY
