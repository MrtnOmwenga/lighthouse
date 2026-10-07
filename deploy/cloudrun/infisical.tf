# Infisical is where secrets that a person had to obtain are kept and changed: one place, with a
# history and access control. Terraform signs in as a machine identity (INFISICAL_UNIVERSAL_AUTH_
# CLIENT_ID and _CLIENT_SECRET in the environment) and reads them while it runs.
#
# Values are read as ephemeral resources, which exist only for the duration of a plan or apply and
# are never saved. Their version numbers are read as ordinary data, so a change is noticed.

provider "infisical" {
  host = var.infisical_host
}

ephemeral "infisical_secret" "kept" {
  for_each     = local.kept
  workspace_id = var.infisical_project_id
  env_slug     = var.infisical_environment
  folder_path  = each.value.folder
  name         = each.value.name
}

data "infisical_secret_metadata" "kept" {
  for_each         = local.kept
  project_id       = var.infisical_project_id
  environment_slug = var.infisical_environment
  folder_path      = each.value.folder
  name             = each.value.name
}
