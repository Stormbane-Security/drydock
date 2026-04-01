// Package ghactions implements a GitHub Actions backend that triggers
// workflow runs and polls for completion via the gh CLI.
package ghactions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Backend triggers and monitors a GitHub Actions workflow run.
type Backend struct {
	repo     string
	workflow string
	ref      string
	trigger  string
	inputs   map[string]string
	env      []string

	// State populated during Create.
	runID      int64
	testBranch string
}

// New creates a GitHub Actions backend.
func New(repo, workflow, ref, trigger string, inputs map[string]string, env map[string]string) *Backend {
	var envSlice []string
	for k, v := range env {
		envSlice = append(envSlice, k+"="+v)
	}
	if trigger == "" {
		trigger = "workflow_dispatch"
	}
	return &Backend{
		repo:     repo,
		workflow: workflow,
		ref:      ref,
		trigger:  trigger,
		inputs:   inputs,
		env:      envSlice,
	}
}

func (b *Backend) Name() string { return "github-actions" }

func (b *Backend) gh(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	if len(b.env) > 0 {
		cmd.Env = append(cmd.Environ(), b.env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Create triggers a workflow run.
func (b *Backend) Create(ctx context.Context) error {
	if b.trigger == "push" {
		return b.createViaPush(ctx)
	}
	return b.createViaDispatch(ctx)
}

func (b *Backend) createViaDispatch(ctx context.Context) error {
	beforeTime := time.Now().UTC()

	args := []string{"workflow", "run", b.workflow, "--repo", b.repo, "--ref", b.ref}
	for k, v := range b.inputs {
		args = append(args, "--field", k+"="+v)
	}

	_, stderr, err := b.gh(ctx, args...)
	if err != nil {
		return fmt.Errorf("triggering workflow: %s: %w", strings.TrimSpace(stderr), err)
	}

	return b.findRun(ctx, b.ref, "workflow_dispatch", beforeTime)
}

func (b *Backend) createViaPush(ctx context.Context) error {
	b.testBranch = fmt.Sprintf("drydock-test-%d", time.Now().UnixNano())

	// Get the SHA of the target ref.
	stdout, stderr, err := b.gh(ctx, "api",
		fmt.Sprintf("repos/%s/git/ref/heads/%s", b.repo, b.ref),
		"--jq", ".object.sha")
	if err != nil {
		return fmt.Errorf("getting ref SHA: %s: %w", strings.TrimSpace(stderr), err)
	}
	sha := strings.TrimSpace(stdout)

	beforeTime := time.Now().UTC()

	// Create the test branch.
	payload := fmt.Sprintf(`{"ref":"refs/heads/%s","sha":"%s"}`, b.testBranch, sha)
	_, stderr, err = b.gh(ctx, "api",
		fmt.Sprintf("repos/%s/git/refs", b.repo),
		"--method", "POST",
		"--input", "-",
		"--silent")
	// Retry with raw input since --input reads from stdin.
	cmd := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/git/refs", b.repo),
		"--method", "POST",
		"--input", "-")
	cmd.Stdin = strings.NewReader(payload)
	if len(b.env) > 0 {
		cmd.Env = append(cmd.Environ(), b.env...)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating test branch %s: %s: %w", b.testBranch, errBuf.String(), err)
	}

	return b.findRun(ctx, b.testBranch, "push", beforeTime)
}

// findRun polls gh run list until it finds a matching run created after beforeTime.
func (b *Backend) findRun(ctx context.Context, branch, event string, beforeTime time.Time) error {
	deadline := time.After(90 * time.Second)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()

	// Wait a moment for GitHub to register the run.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timed out waiting for workflow run to appear (branch=%s, event=%s)", branch, event)
		case <-tick.C:
			stdout, _, err := b.gh(ctx, "run", "list",
				"--repo", b.repo,
				"--workflow", b.workflow,
				"--branch", branch,
				"--json", "databaseId,createdAt,event,status",
				"--limit", "10")
			if err != nil {
				continue
			}

			var runs []struct {
				DatabaseID int64     `json:"databaseId"`
				CreatedAt  time.Time `json:"createdAt"`
				Event      string    `json:"event"`
				Status     string    `json:"status"`
			}
			if err := json.Unmarshal([]byte(stdout), &runs); err != nil {
				continue
			}

			for _, r := range runs {
				if r.Event == event && r.CreatedAt.After(beforeTime.Add(-5*time.Second)) {
					b.runID = r.DatabaseID
					return nil
				}
			}
		}
	}
}

// WaitReady polls until the workflow run completes.
func (b *Backend) WaitReady(ctx context.Context) error {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			stdout, _, err := b.gh(ctx, "run", "view",
				strconv.FormatInt(b.runID, 10),
				"--repo", b.repo,
				"--json", "status,conclusion")
			if err != nil {
				continue
			}

			var run struct {
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			}
			if err := json.Unmarshal([]byte(stdout), &run); err != nil {
				continue
			}

			if run.Status == "completed" {
				return nil
			}
		}
	}
}

// Logs returns the workflow run logs keyed by job name.
func (b *Backend) Logs(ctx context.Context) (map[string]string, error) {
	stdout, _, err := b.gh(ctx, "run", "view",
		strconv.FormatInt(b.runID, 10),
		"--repo", b.repo,
		"--log")
	if err != nil {
		return nil, fmt.Errorf("fetching run logs: %w", err)
	}

	logs := make(map[string]string)
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		jobName := parts[0]
		logs[jobName] += line + "\n"
	}
	return logs, nil
}

// Outputs returns structured run metadata for assertions.
// Keys follow the pattern:
//
//	run.conclusion, run.status
//	job.<name>.conclusion
//	job.<name>.step.<stepName>.conclusion, job.<name>.step.<stepName>.status
//	artifact.<name>
func (b *Backend) Outputs(ctx context.Context) (map[string]string, error) {
	outputs := make(map[string]string)

	// Get run with jobs.
	stdout, _, err := b.gh(ctx, "run", "view",
		strconv.FormatInt(b.runID, 10),
		"--repo", b.repo,
		"--json", "conclusion,status,jobs")
	if err != nil {
		return outputs, fmt.Errorf("fetching run details: %w", err)
	}

	var run struct {
		Conclusion string `json:"conclusion"`
		Status     string `json:"status"`
		Jobs       []struct {
			Name       string `json:"name"`
			Conclusion string `json:"conclusion"`
			Status     string `json:"status"`
			Steps      []struct {
				Name       string `json:"name"`
				Conclusion string `json:"conclusion"`
				Status     string `json:"status"`
			} `json:"steps"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal([]byte(stdout), &run); err != nil {
		return outputs, fmt.Errorf("parsing run details: %w", err)
	}

	outputs["run.conclusion"] = run.Conclusion
	outputs["run.status"] = run.Status

	for _, job := range run.Jobs {
		prefix := "job." + job.Name
		outputs[prefix+".conclusion"] = job.Conclusion
		outputs[prefix+".status"] = job.Status

		for _, step := range job.Steps {
			stepPrefix := prefix + ".step." + step.Name
			outputs[stepPrefix+".conclusion"] = step.Conclusion
			outputs[stepPrefix+".status"] = step.Status
		}
	}

	// Get artifacts.
	stdout, _, err = b.gh(ctx, "api",
		fmt.Sprintf("repos/%s/actions/runs/%d/artifacts", b.repo, b.runID),
		"--jq", ".artifacts[].name")
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(stdout), "\n") {
			name = strings.TrimSpace(name)
			if name != "" {
				outputs["artifact."+name] = "present"
			}
		}
	}

	return outputs, nil
}

// Destroy cancels the run if still active and deletes any test branch.
func (b *Backend) Destroy(ctx context.Context) error {
	if b.runID > 0 {
		// Cancel if still running — ignore errors (may already be complete).
		_, _, _ = b.gh(ctx, "run", "cancel",
			strconv.FormatInt(b.runID, 10),
			"--repo", b.repo)
	}

	if b.testBranch != "" {
		_, _, _ = b.gh(ctx, "api", "--method", "DELETE",
			fmt.Sprintf("repos/%s/git/refs/heads/%s", b.repo, b.testBranch))
	}

	return nil
}
