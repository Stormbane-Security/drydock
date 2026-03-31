# Example Terraform configuration for validation testing.
# This represents a security-hardened GCS bucket — the kind of thing
# Bosun would generate or a team would write and want to validate.

terraform {
  required_version = ">= 1.5"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

variable "project_id" {
  description = "GCP project ID"
  type        = string
  default     = "drydock-test"
}

variable "bucket_name" {
  description = "Name of the GCS bucket"
  type        = string
  default     = "drydock-test-bucket"
}

variable "region" {
  description = "GCS bucket location"
  type        = string
  default     = "us-central1"
}

resource "google_storage_bucket" "main" {
  name          = var.bucket_name
  location      = var.region
  project       = var.project_id
  force_destroy = true

  # Security hardening: uniform bucket-level access (no per-object ACLs)
  uniform_bucket_level_access = true

  # Security hardening: versioning for recovery
  versioning {
    enabled = true
  }

  # Security hardening: encryption with CMEK
  encryption {
    default_kms_key_name = "projects/${var.project_id}/locations/${var.region}/keyRings/drydock/cryptoKeys/bucket-key"
  }

  # Lifecycle: auto-delete old versions after 90 days
  lifecycle_rule {
    condition {
      age                   = 90
      with_state            = "ARCHIVED"
    }
    action {
      type = "Delete"
    }
  }

  # Public access prevention
  public_access_prevention = "enforced"
}

output "bucket_name" {
  value = google_storage_bucket.main.name
}

output "bucket_url" {
  value = google_storage_bucket.main.url
}
