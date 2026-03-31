// Package backend defines the interface for environment providers.
package backend

import "context"

// Backend manages the lifecycle of a test environment.
type Backend interface {
	// Name returns the backend identifier (e.g. "compose", "terraform").
	Name() string

	// Create brings up the environment. For Compose this means `docker compose up`,
	// for Terraform this means `terraform init && terraform apply`.
	Create(ctx context.Context) error

	// WaitReady blocks until the environment is healthy or the context expires.
	WaitReady(ctx context.Context) error

	// Logs returns collected container/resource logs as a map of name → log content.
	Logs(ctx context.Context) (map[string]string, error)

	// Outputs returns key-value outputs from the environment (e.g. terraform outputs).
	Outputs(ctx context.Context) (map[string]string, error)

	// Destroy tears down the environment. Must be safe to call multiple times.
	Destroy(ctx context.Context) error
}
