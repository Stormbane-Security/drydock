# Artifact Registry repository for container images.
resource "google_artifact_registry_repository" "test" {
  repository_id = "ci-test"
  location      = var.region
  format        = "DOCKER"
  description   = "Test container registry for Drydock CI/CD validation"

  cleanup_policies {
    id     = "delete-old"
    action = "DELETE"

    condition {
      older_than = "86400s" # 1 day — test images are ephemeral
    }
  }
}
