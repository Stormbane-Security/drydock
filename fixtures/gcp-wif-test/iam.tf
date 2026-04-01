# Service account used by GitHub Actions via WIF.
resource "google_service_account" "github_actions" {
  account_id   = "github-actions-test"
  display_name = "GitHub Actions CI/CD Test"
  description  = "Service account for Drydock CI/CD testing"
}

# Allow the GitHub Actions WIF identity to impersonate this service account.
resource "google_service_account_iam_member" "wif_binding" {
  service_account_id = google_service_account.github_actions.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${local.full_repo}"
}

# Grant the service account permission to push images to Artifact Registry.
resource "google_artifact_registry_repository_iam_member" "writer" {
  repository = google_artifact_registry_repository.test.name
  location   = var.region
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.github_actions.email}"
}

# Grant the service account permission to read images (for Trivy scan after push).
resource "google_artifact_registry_repository_iam_member" "reader" {
  repository = google_artifact_registry_repository.test.name
  location   = var.region
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${google_service_account.github_actions.email}"
}
