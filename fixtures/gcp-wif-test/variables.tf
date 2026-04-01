variable "project_id" {
  description = "GCP project ID for test infrastructure"
  type        = string
}

variable "region" {
  description = "GCP region for Artifact Registry"
  type        = string
  default     = "us-central1"
}

variable "github_org" {
  description = "GitHub organization that owns the test repo"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name for CI/CD testing"
  type        = string
}

variable "environment" {
  description = "GitHub deployment environment name to restrict WIF to"
  type        = string
  default     = ""
}

variable "reusable_workflow_ref" {
  description = "Full ref of the reusable workflow to restrict WIF to (e.g. stormbane-security/bosun/.github/workflows/docker.yml@refs/heads/main)"
  type        = string
  default     = ""
}
