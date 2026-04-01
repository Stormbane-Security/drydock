# Workload Identity Federation pool for GitHub Actions OIDC.
resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github-actions-test"
  display_name              = "GitHub Actions (test)"
  description               = "WIF pool for CI/CD testing via Drydock"
}

# WIF provider with OIDC claim attribute mapping and conditions.
#
# GitHub OIDC token claims mapped to attributes:
#   - repository:       owner/repo
#   - environment:      GitHub deployment environment name
#   - job_workflow_ref: reusable workflow ref (proves caller uses an approved workflow)
#   - ref:             git ref (branch/tag)
#   - actor:           GitHub user who triggered the run
#   - workflow:        workflow name
resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-oidc"
  display_name                       = "GitHub OIDC"

  attribute_mapping = {
    "google.subject"             = "assertion.sub"
    "attribute.repository"       = "assertion.repository"
    "attribute.repository_owner" = "assertion.repository_owner"
    "attribute.ref"              = "assertion.ref"
    "attribute.environment"      = "assertion.environment"
    "attribute.workflow"         = "assertion.workflow"
    "attribute.job_workflow_ref" = "assertion.job_workflow_ref"
    "attribute.actor"            = "assertion.actor"
  }

  # Attribute condition: only tokens from the specific test repo are accepted.
  # Additional conditions for environment and reusable workflow ref are appended
  # when the corresponding variables are set.
  attribute_condition = join(" && ", compact([
    "attribute.repository == \"${local.full_repo}\"",
    var.environment != "" ? "attribute.environment == \"${var.environment}\"" : "",
    var.reusable_workflow_ref != "" ? "attribute.job_workflow_ref == \"${var.reusable_workflow_ref}\"" : "",
  ]))

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}
