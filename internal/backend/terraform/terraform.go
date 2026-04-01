// Package terraform implements the Terraform backend for drydock.
// It manages init, plan, apply, output, and destroy of Terraform configurations.
package terraform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Backend manages a Terraform environment.
type Backend struct {
	dir         string            // scenario directory
	tfDir       string            // relative path to terraform root
	vars        map[string]string // -var arguments
	workspace   string
	autoApprove bool
	env         []string
}

// New creates a Terraform backend.
func New(dir, tfDir string, vars map[string]string, workspace string, autoApprove bool, env map[string]string) *Backend {
	var envSlice []string
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	// TF_IN_AUTOMATION suppresses interactive prompts.
	envSlice = append(envSlice, "TF_IN_AUTOMATION=1")

	return &Backend{
		dir:         dir,
		tfDir:       tfDir,
		vars:        vars,
		workspace:   workspace,
		autoApprove: autoApprove,
		env:         envSlice,
	}
}

func (b *Backend) Name() string { return "terraform" }

func (b *Backend) tfPath() string {
	return filepath.Join(b.dir, b.tfDir)
}

func (b *Backend) run(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "terraform", args...)
	cmd.Dir = b.tfPath()
	if len(b.env) > 0 {
		cmd.Env = append(cmd.Environ(), b.env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Create runs terraform init + apply.
func (b *Backend) Create(ctx context.Context) error {
	// Init.
	_, stderr, err := b.run(ctx, "init", "-input=false")
	if err != nil {
		return fmt.Errorf("terraform init: %s: %w", stderr, err)
	}

	// Workspace.
	if b.workspace != "" {
		// Try to select, create if it doesn't exist.
		_, _, err := b.run(ctx, "workspace", "select", b.workspace)
		if err != nil {
			_, stderr, err = b.run(ctx, "workspace", "new", b.workspace)
			if err != nil {
				return fmt.Errorf("terraform workspace new: %s: %w", stderr, err)
			}
		}
	}

	// Build apply args.
	args := []string{"apply", "-input=false"}
	if b.autoApprove {
		args = append(args, "-auto-approve")
	}
	for k, v := range b.vars {
		args = append(args, "-var", k+"="+v)
	}

	_, stderr, err = b.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("terraform apply: %s: %w", stderr, err)
	}
	return nil
}

// WaitReady is a no-op for Terraform — apply blocks until complete.
func (b *Backend) WaitReady(ctx context.Context) error {
	return nil
}

// Logs returns the terraform plan output as a "log."
func (b *Backend) Logs(ctx context.Context) (map[string]string, error) {
	args := []string{"plan", "-input=false", "-no-color"}
	for k, v := range b.vars {
		args = append(args, "-var", k+"="+v)
	}
	stdout, _, err := b.run(ctx, args...)
	if err != nil {
		return map[string]string{"terraform_plan": "error: " + err.Error()}, nil
	}
	return map[string]string{"terraform_plan": stdout}, nil
}

// Outputs returns terraform output values.
func (b *Backend) Outputs(ctx context.Context) (map[string]string, error) {
	stdout, stderr, err := b.run(ctx, "output", "-json")
	if err != nil {
		return nil, fmt.Errorf("terraform output: %s: %w", stderr, err)
	}

	var raw map[string]struct {
		Value any    `json:"value"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return nil, fmt.Errorf("parsing terraform output: %w", err)
	}

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.Value.(type) {
		case string:
			out[k] = val
		default:
			b, _ := json.Marshal(val)
			out[k] = string(b)
		}
	}
	return out, nil
}

// Destroy runs terraform destroy.
func (b *Backend) Destroy(ctx context.Context) error {
	args := []string{"destroy", "-input=false"}
	if b.autoApprove {
		args = append(args, "-auto-approve")
	}
	for k, v := range b.vars {
		args = append(args, "-var", k+"="+v)
	}

	_, stderr, err := b.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("terraform destroy: %s: %w", strings.TrimSpace(stderr), err)
	}

	// Clean up workspace if we created one.
	if b.workspace != "" {
		_, _, _ = b.run(ctx, "workspace", "select", "default")
		_, _, _ = b.run(ctx, "workspace", "delete", b.workspace)
	}

	return nil
}

// Plan runs terraform plan and returns the human-readable output.
func (b *Backend) Plan(ctx context.Context) (string, error) {
	args := []string{"plan", "-input=false", "-no-color"}
	for k, v := range b.vars {
		args = append(args, "-var", k+"="+v)
	}
	stdout, stderr, err := b.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("terraform plan: %s: %w", stderr, err)
	}
	return stdout, nil
}

// Validate runs terraform validate.
func (b *Backend) Validate(ctx context.Context) error {
	_, stderr, err := b.run(ctx, "init", "-input=false", "-backend=false")
	if err != nil {
		return fmt.Errorf("terraform init: %s: %w", stderr, err)
	}
	_, stderr, err = b.run(ctx, "validate")
	if err != nil {
		return fmt.Errorf("terraform validate: %s: %w", stderr, err)
	}
	return nil
}
