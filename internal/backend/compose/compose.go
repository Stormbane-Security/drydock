// Package compose implements the Docker Compose backend.
package compose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Backend manages a Docker Compose environment.
type Backend struct {
	composeFile string
	projectName string
	dir         string
	env         []string
}

// New creates a Compose backend.
func New(dir, composeFile, projectName string, env map[string]string) *Backend {
	// Build env slice.
	var envSlice []string
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	return &Backend{
		composeFile: composeFile,
		projectName: projectName,
		dir:         dir,
		env:         envSlice,
	}
}

func (b *Backend) Name() string { return "compose" }

// Dir returns the working directory for this compose backend.
func (b *Backend) Dir() string { return b.dir }

func (b *Backend) run(ctx context.Context, args ...string) (string, string, error) {
	fullArgs := []string{"-f", filepath.Join(b.dir, b.composeFile)}
	if b.projectName != "" {
		fullArgs = append(fullArgs, "-p", b.projectName)
	}
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, fullArgs...)...)
	cmd.Dir = b.dir
	if len(b.env) > 0 {
		cmd.Env = append(cmd.Environ(), b.env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Create runs `docker compose up -d`.
func (b *Backend) Create(ctx context.Context) error {
	_, stderr, err := b.run(ctx, "up", "-d", "--wait", "--build")
	if err != nil {
		return fmt.Errorf("compose up: %s: %w", stderr, err)
	}
	return nil
}

// WaitReady polls container health until all services are healthy.
func (b *Backend) WaitReady(ctx context.Context) error {
	deadline := time.After(2 * time.Minute)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timed out waiting for services to be ready")
		case <-tick.C:
			stdout, _, err := b.run(ctx, "ps", "--format", "json")
			if err != nil {
				continue
			}
			if allHealthy(stdout) {
				return nil
			}
		}
	}
}

// allHealthy checks if all containers are running (and healthy if health checks defined).
func allHealthy(jsonOutput string) bool {
	lines := strings.Split(strings.TrimSpace(jsonOutput), "\n")
	if len(lines) == 0 {
		return false
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var svc struct {
			State  string `json:"State"`
			Health string `json:"Health"`
		}
		if err := json.Unmarshal([]byte(line), &svc); err != nil {
			return false
		}
		if svc.State != "running" {
			return false
		}
		if svc.Health != "" && svc.Health != "healthy" {
			return false
		}
	}
	return true
}

// Logs returns container logs for all services.
func (b *Backend) Logs(ctx context.Context) (map[string]string, error) {
	// Get service names.
	stdout, _, err := b.run(ctx, "ps", "--services")
	if err != nil {
		return nil, fmt.Errorf("listing services: %w", err)
	}

	logs := make(map[string]string)
	for _, svc := range strings.Split(strings.TrimSpace(stdout), "\n") {
		svc = strings.TrimSpace(svc)
		if svc == "" {
			continue
		}
		out, _, err := b.run(ctx, "logs", "--no-color", svc)
		if err != nil {
			logs[svc] = fmt.Sprintf("error collecting logs: %v", err)
		} else {
			logs[svc] = out
		}
	}
	return logs, nil
}

// Outputs returns an empty map — Compose doesn't have native outputs.
func (b *Backend) Outputs(ctx context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// Destroy runs `docker compose down -v --remove-orphans`.
func (b *Backend) Destroy(ctx context.Context) error {
	_, stderr, err := b.run(ctx, "down", "-v", "--remove-orphans", "--timeout", "30")
	if err != nil {
		return fmt.Errorf("compose down: %s: %w", stderr, err)
	}
	return nil
}
