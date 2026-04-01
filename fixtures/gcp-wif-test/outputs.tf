output "wif_provider" {
  description = "Full resource name of the WIF provider (pass to gcp-workload-identity-provider workflow input)"
  value       = google_iam_workload_identity_pool_provider.github.name
}

output "sa_email" {
  description = "Service account email (pass to gcp-service-account workflow input)"
  value       = google_service_account.github_actions.email
}

output "gar_repo" {
  description = "Full Artifact Registry repository path (e.g. us-central1-docker.pkg.dev/project/ci-test)"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.test.repository_id}"
}

output "gar_location" {
  description = "Artifact Registry location"
  value       = var.region
}

output "wif_pool_id" {
  description = "WIF pool ID for verification assertions"
  value       = google_iam_workload_identity_pool.github.workload_identity_pool_id
}

output "wif_provider_id" {
  description = "WIF provider ID for verification assertions"
  value       = google_iam_workload_identity_pool_provider.github.workload_identity_pool_provider_id
}

output "project_id" {
  description = "GCP project ID (passthrough for assertions)"
  value       = var.project_id
}
