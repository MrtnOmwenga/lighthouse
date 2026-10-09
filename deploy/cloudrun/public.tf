# Files the site links to that don't belong in a public repository: at present, the CV. It names
# employers and clients, which the repositories don't. The file is uploaded by hand (see
# deploy/README.md); only the bucket is managed here. US-EAST1 is inside Google's free allowance.
resource "google_storage_bucket" "public" {
  name                        = "${var.gcp_project}-public"
  location                    = "US-EAST1"
  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  force_destroy               = false
  depends_on                  = [google_project_service.api]
}

# Anyone may read what is put here, and nothing else: no listing, no writing.
resource "google_storage_bucket_iam_member" "public_read" {
  bucket = google_storage_bucket.public.name
  role   = "roles/storage.legacyObjectReader"
  member = "allUsers"
}

output "public_files" {
  description = "Where publicly linked files are served from."
  value       = "https://storage.googleapis.com/${google_storage_bucket.public.name}"
}
