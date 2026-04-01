// Package engine orchestrates the full drydock lifecycle:
// validate → create → wait → execute → assert → collect → destroy
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/assertion"
	"github.com/stormbane-security/drydock/internal/backend"
	"github.com/stormbane-security/drydock/internal/backend/compose"
	"github.com/stormbane-security/drydock/internal/backend/ghactions"
	tf "github.com/stormbane-security/drydock/internal/backend/terraform"
	"github.com/stormbane-security/drydock/internal/interpolate"
	"github.com/stormbane-security/drydock/internal/runner"
	"github.com/stormbane-security/drydock/internal/scenario"
)

// Engine runs drydock scenarios.
type Engine struct {
	artifactStore *artifact.Store
	out           io.Writer
}

// New creates an Engine with the given artifact store.
func New(artifactDir string) *Engine {
	return &Engine{
		artifactStore: artifact.NewStore(artifactDir),
		out:           os.Stderr,
	}
}

// SetOutput sets the writer for status messages.
func (e *Engine) SetOutput(w io.Writer) {
	e.out = w
}

func (e *Engine) log(format string, args ...any) {
	fmt.Fprintf(e.out, "drydock: "+format+"\n", args...)
}

// Run executes a full scenario lifecycle and returns the run record.
func (e *Engine) Run(ctx context.Context, s *scenario.Scenario) (*artifact.RunRecord, error) {
	runID := artifact.GenerateRunID(s.Name)

	record := &artifact.RunRecord{
		ID:        runID,
		Scenario:  s.Name,
		StartedAt: time.Now().UTC(),
		Status:    "running",
	}

	// Apply scenario timeout.
	ctx, cancel := context.WithTimeout(ctx, s.Timeout.Duration)
	defer cancel()

	// Phase 0: Create fixture (if present).
	if s.Fixture != nil {
		fixture := e.createFixture(s)

		// Defer fixture destroy FIRST so it runs LAST (after backend destroy).
		defer func() {
			e.log("destroying fixture...")
			destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer destroyCancel()
			if err := fixture.Destroy(destroyCtx); err != nil {
				e.log("warning: fixture destroy failed: %v", err)
			}
		}()

		e.log("creating fixture (%s)...", s.Fixture.Module)
		if err := fixture.Create(ctx); err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("fixture create failed: %v", err)
			e.finalize(record)
			return record, fmt.Errorf("fixture create: %w", err)
		}

		e.log("collecting fixture outputs...")
		fixtureOutputs, err := fixture.Outputs(ctx)
		if err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("fixture outputs failed: %v", err)
			e.finalize(record)
			return record, fmt.Errorf("fixture outputs: %w", err)
		}

		e.log("interpolating fixture variables (%d outputs)...", len(fixtureOutputs))
		if err := interpolate.Scenario(s, fixtureOutputs); err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("fixture interpolation failed: %v", err)
			e.finalize(record)
			return record, fmt.Errorf("fixture interpolation: %w", err)
		}
	}

	// Create backend(s).
	backends, err := e.createBackends(s)
	if err != nil {
		record.Status = "error"
		record.Error = err.Error()
		record.FinishedAt = time.Now().UTC()
		record.Duration = record.FinishedAt.Sub(record.StartedAt)
		e.artifactStore.Save(record) //nolint:errcheck
		return record, err
	}

	// Ensure destroy runs no matter what.
	defer func() {
		e.log("destroying environment...")
		destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer destroyCancel()
		for _, b := range backends {
			if err := b.Destroy(destroyCtx); err != nil {
				e.log("warning: destroy %s failed: %v", b.Name(), err)
			}
		}
	}()

	// Phase 1: Create environment.
	e.log("creating environment (%d backends)...", len(backends))
	for _, b := range backends {
		e.log("  starting %s...", b.Name())
		if err := b.Create(ctx); err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("%s create failed: %v", b.Name(), err)
			e.finalize(record)
			return record, fmt.Errorf("%s create: %w", b.Name(), err)
		}
	}

	// Phase 2: Wait for readiness.
	e.log("waiting for environment readiness...")
	for _, b := range backends {
		if err := b.WaitReady(ctx); err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("%s not ready: %v", b.Name(), err)
			e.finalize(record)
			return record, fmt.Errorf("%s wait: %w", b.Name(), err)
		}
	}

	// Phase 3: Run setup commands.
	if len(s.Setup) > 0 {
		e.log("running %d setup commands...", len(s.Setup))
		setupSpecs := commandsToSpecs(s.Setup)
		setupResults := runner.RunAll(ctx, setupSpecs, s.Dir, s.Env)
		record.CommandResults = append(record.CommandResults, setupResults...)
		for _, r := range setupResults {
			if r.ExitCode != 0 && r.Error != "" {
				record.Status = "error"
				record.Error = fmt.Sprintf("setup command %q failed: %s", r.Name, r.Error)
				e.finalize(record)
				return record, fmt.Errorf("setup failed: %s", r.Error)
			}
		}
	}

	// Phase 4: Run test commands.
	e.log("running %d commands...", len(s.Commands))
	cmdSpecs := commandsToSpecs(s.Commands)
	cmdResults := runner.RunAll(ctx, cmdSpecs, s.Dir, s.Env)
	record.CommandResults = append(record.CommandResults, cmdResults...)

	// Check for command failures.
	commandsPassed := true
	for _, r := range cmdResults {
		if r.ExitCode != 0 || r.Error != "" {
			commandsPassed = false
		}
	}

	// Phase 5: Collect outputs from backends.
	allOutputs := make(map[string]string)
	for _, b := range backends {
		outs, err := b.Outputs(ctx)
		if err != nil {
			e.log("warning: collecting outputs from %s: %v", b.Name(), err)
		}
		for k, v := range outs {
			allOutputs[k] = v
		}
	}
	record.Outputs = allOutputs

	// Phase 6: Run assertions.
	if len(s.Assertions) > 0 {
		e.log("running %d assertions...", len(s.Assertions))
		assertResults := assertion.Run(ctx, s.Assertions, allOutputs, s.Dir, s.Env)
		record.AssertionResults = assertResults

		for _, r := range assertResults {
			status := "PASS"
			if !r.Passed {
				status = "FAIL"
			}
			e.log("  [%s] %s: %s", status, r.Name, r.Message)
		}
	}

	// Phase 7: Collect logs.
	if s.Artifacts.ContainerLogs {
		e.log("collecting container logs...")
		for _, b := range backends {
			logs, err := b.Logs(ctx)
			if err != nil {
				e.log("warning: collecting logs from %s: %v", b.Name(), err)
				continue
			}
			if record.Logs == nil {
				record.Logs = make(map[string]string)
			}
			for k, v := range logs {
				record.Logs[b.Name()+"_"+k] = v
			}
		}
	}

	// Determine final status.
	assertionsPassed := assertion.AllPassed(record.AssertionResults)
	if commandsPassed && assertionsPassed {
		record.Status = "pass"
	} else {
		record.Status = "fail"
		if !commandsPassed {
			record.Error = "one or more commands failed"
		} else {
			record.Error = "one or more assertions failed"
		}
	}

	e.finalize(record)
	return record, nil
}

func (e *Engine) finalize(record *artifact.RunRecord) {
	record.FinishedAt = time.Now().UTC()
	record.Duration = record.FinishedAt.Sub(record.StartedAt)

	e.log("run %s finished: %s (%.1fs)", record.ID, record.Status, record.Duration.Seconds())

	if err := e.artifactStore.Save(record); err != nil {
		e.log("warning: saving artifacts: %v", err)
	}
}

// Validate checks a scenario without running it.
func (e *Engine) Validate(s *scenario.Scenario) error {
	return s.Validate()
}

// Destroy tears down an environment for a scenario (useful for manual cleanup).
func (e *Engine) Destroy(ctx context.Context, s *scenario.Scenario) error {
	backends, err := e.createBackends(s)
	if err != nil {
		return err
	}
	for _, b := range backends {
		if err := b.Destroy(ctx); err != nil {
			return fmt.Errorf("destroying %s: %w", b.Name(), err)
		}
	}
	// Destroy fixture if present.
	if s.Fixture != nil {
		fixture := e.createFixture(s)
		if err := fixture.Destroy(ctx); err != nil {
			return fmt.Errorf("destroying fixture: %w", err)
		}
	}
	return nil
}

// Inspect loads and returns a previous run record.
func (e *Engine) Inspect(runID string) (*artifact.RunRecord, error) {
	return e.artifactStore.Load(runID)
}

// ListRuns returns all run IDs.
func (e *Engine) ListRuns() ([]string, error) {
	return e.artifactStore.List()
}

func (e *Engine) createBackends(s *scenario.Scenario) ([]backend.Backend, error) {
	var backends []backend.Backend

	autoApprove := true
	if s.Backend.AutoApprove != nil {
		autoApprove = *s.Backend.AutoApprove
	}

	switch s.Backend.Type {
	case "compose":
		backends = append(backends, compose.New(s.Dir, s.Backend.ComposeFile, "drydock-"+s.Name, s.Env))
	case "terraform":
		workspace := s.Backend.Workspace
		if workspace == "" {
			workspace = "drydock-" + s.Name
		}
		backends = append(backends, tf.New(s.Dir, s.Backend.TerraformDir, s.Backend.TerraformVars, workspace, autoApprove, s.Env))
	case "hybrid":
		if s.Backend.ComposeFile != "" {
			backends = append(backends, compose.New(s.Dir, s.Backend.ComposeFile, "drydock-"+s.Name, s.Env))
		}
		if s.Backend.TerraformDir != "" {
			workspace := s.Backend.Workspace
			if workspace == "" {
				workspace = "drydock-" + s.Name
			}
			backends = append(backends, tf.New(s.Dir, s.Backend.TerraformDir, s.Backend.TerraformVars, workspace, autoApprove, s.Env))
		}
	case "github-actions":
		ref := s.Backend.Ref
		if ref == "" {
			ref = "main"
		}
		backends = append(backends, ghactions.New(s.Backend.Repo, s.Backend.Workflow, ref, s.Backend.Trigger, s.Backend.Inputs, s.Env))
	default:
		return nil, fmt.Errorf("unsupported backend type: %q", s.Backend.Type)
	}

	return backends, nil
}

func (e *Engine) createFixture(s *scenario.Scenario) *tf.Backend {
	workspace := s.Fixture.Workspace
	if workspace == "" {
		workspace = "drydock-fixture-" + s.Name
	}
	return tf.New(s.Dir, s.Fixture.Module, s.Fixture.Vars, workspace, true, s.Env)
}

func commandsToSpecs(commands []scenario.Command) []runner.CommandSpec {
	specs := make([]runner.CommandSpec, len(commands))
	for i, c := range commands {
		var expectExit *int
		if c.Expect != nil && c.Expect.ExitCode != nil {
			expectExit = c.Expect.ExitCode
		}
		specs[i] = runner.CommandSpec{
			Name:            c.Name,
			Run:             c.Run,
			Dir:             c.Dir,
			Env:             c.Env,
			Timeout:         c.Timeout.Duration,
			ContinueOnError: c.ContinueOnError,
			ExpectExit:      expectExit,
		}
	}
	return specs
}
