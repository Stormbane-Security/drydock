// Package engine orchestrates the full drydock lifecycle:
// validate → create → wait → execute → assert → collect → destroy
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	_, _ = fmt.Fprintf(e.out, "drydock: "+format+"\n", args...)
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
		_ = e.artifactStore.Save(record)
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
		// Clean up generated temp compose dir for unified format.
		if s.IsUnifiedFormat() {
			e.cleanupGeneratedCompose(backends)
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

	// Phase 1b: Discover ephemeral ports and apply substitutions.
	for _, b := range backends {
		if cb, ok := b.(*compose.Backend); ok {
			if err := e.resolveEphemeralPorts(ctx, cb, s); err != nil {
				e.log("warning: ephemeral port resolution failed: %v", err)
			}
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

	// Phase 2b: Run ready check (unified format).
	if s.Ready != nil {
		e.log("running ready check (timeout %s, interval %s)...", s.Ready.Timeout.Duration, s.Ready.Interval.Duration)
		if err := e.runReadyCheck(ctx, s); err != nil {
			record.Status = "error"
			record.Error = fmt.Sprintf("ready check failed: %v", err)
			e.finalize(record)
			return record, fmt.Errorf("ready check: %w", err)
		}
		e.log("ready check passed")
	}

	// Phase 2c: Run pre-assertion commands (unified format "run" field).
	if len(s.Run) > 0 {
		e.log("running %d commands...", len(s.Run))
		for i, cmd := range s.Run {
			name := fmt.Sprintf("run[%d]", i)
			r := runner.Run(ctx, name, cmd, s.Dir, s.Env)
			record.CommandResults = append(record.CommandResults, r)
			if r.ExitCode != 0 || r.Error != "" {
				record.Status = "error"
				record.Error = fmt.Sprintf("run command %d failed (exit %d): %s", i, r.ExitCode, r.Error)
				e.finalize(record)
				return record, fmt.Errorf("run command %d: exit %d", i, r.ExitCode)
			}
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
	// Unified format: generate compose.yaml from inline services.
	if s.IsUnifiedFormat() {
		composeFile, portPlan, err := scenario.GenerateComposeFile(s.Services, s.Dir, s.Networks)
		if err != nil {
			return nil, fmt.Errorf("generating compose file: %w", err)
		}
		dir := filepath.Dir(composeFile)
		file := filepath.Base(composeFile)
		cb := compose.New(dir, file, "drydock-"+s.Name, s.Env)
		// Attach port plan so we can query actual ports after Create.
		if portPlan != nil {
			var mappings []compose.PortMapping
			for _, m := range portPlan.Mappings {
				mappings = append(mappings, compose.PortMapping{
					Service:       m.Service,
					IntendedHost:  m.IntendedHost,
					ContainerPort: m.ContainerPort,
				})
			}
			cb.SetPortPlan(mappings)
		}
		return []backend.Backend{cb}, nil
	}

	// Old format: use backend configuration.
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

// cleanupGeneratedCompose removes the temporary directory created for generated
// compose files in unified format scenarios.
func (e *Engine) cleanupGeneratedCompose(backends []backend.Backend) {
	for _, b := range backends {
		if cb, ok := b.(*compose.Backend); ok {
			dir := cb.Dir()
			if dir != "" {
				if err := os.RemoveAll(dir); err != nil {
					e.log("warning: removing temp compose dir: %v", err)
				}
			}
		}
	}
}

// runReadyCheck executes the scenario's ready probe in a loop until it
// succeeds or the timeout elapses.
func (e *Engine) runReadyCheck(ctx context.Context, s *scenario.Scenario) error {
	if s.Ready == nil {
		return nil
	}

	deadline := time.After(s.Ready.Timeout.Duration)
	tick := time.NewTicker(s.Ready.Interval.Duration)
	defer tick.Stop()

	var lastErr string
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for ready check: %s", lastErr)
		case <-deadline:
			return fmt.Errorf("timed out after %s waiting for ready check: %s", s.Ready.Timeout.Duration, lastErr)
		case <-tick.C:
			r := runner.Run(ctx, "ready-check", s.Ready.Cmd, s.Dir, s.Env)
			if r.ExitCode == 0 && r.Error == "" {
				return nil
			}
			lastErr = fmt.Sprintf("exit=%d stderr=%s", r.ExitCode, r.Stderr)
		}
	}
}

// SetupDebug brings up the infrastructure for a scenario without running tests.
// It returns the backends and a cleanup function. The caller must call cleanup
// when done (typically on interrupt signal).
func (e *Engine) SetupDebug(ctx context.Context, s *scenario.Scenario) ([]backend.Backend, func(), error) {
	backends, err := e.createBackends(s)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		e.log("tearing down environment...")
		destroyCtx, destroyCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer destroyCancel()
		for _, b := range backends {
			if err := b.Destroy(destroyCtx); err != nil {
				e.log("warning: destroy %s failed: %v", b.Name(), err)
			}
		}
		// Clean up generated compose file for unified format.
		if s.IsUnifiedFormat() {
			// The compose file is in a temp dir managed by the backend's dir field.
			// compose.Destroy already runs docker compose down.
		}
	}

	// Bring up all backends.
	e.log("creating environment (%d backends)...", len(backends))
	for _, b := range backends {
		e.log("  starting %s...", b.Name())
		if err := b.Create(ctx); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("%s create: %w", b.Name(), err)
		}
	}

	// Resolve ephemeral ports.
	for _, b := range backends {
		if cb, ok := b.(*compose.Backend); ok {
			if err := e.resolveEphemeralPorts(ctx, cb, s); err != nil {
				e.log("warning: ephemeral port resolution failed: %v", err)
			}
		}
	}

	// Wait for backend readiness.
	e.log("waiting for environment readiness...")
	for _, b := range backends {
		if err := b.WaitReady(ctx); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("%s wait: %w", b.Name(), err)
		}
	}

	// Run ready check if defined.
	if s.Ready != nil {
		e.log("running ready check...")
		if err := e.runReadyCheck(ctx, s); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("ready check: %w", err)
		}
	}

	return backends, cleanup, nil
}

// ServiceEndpoints returns a human-readable list of service endpoints
// extracted from the scenario's service port mappings.
func ServiceEndpoints(s *scenario.Scenario) []string {
	var endpoints []string
	for name, svc := range s.Services {
		for _, p := range svc.Ports {
			endpoints = append(endpoints, fmt.Sprintf("%s on localhost:%s", name, hostPort(p)))
		}
		if len(svc.Ports) == 0 {
			endpoints = append(endpoints, name)
		}
	}
	return endpoints
}

// hostPort extracts the host port from a port mapping like "8080:8080" or "8080".
func hostPort(mapping string) string {
	for i, c := range mapping {
		if c == ':' {
			return mapping[:i]
		}
	}
	return mapping
}

// resolveEphemeralPorts queries Docker for actual ephemeral port assignments
// and substitutes them throughout the scenario's ready check, commands, and assertions.
func (e *Engine) resolveEphemeralPorts(ctx context.Context, cb *compose.Backend, s *scenario.Scenario) error {
	subs := cb.PortSubs()

	// If no port plan was set, query using the stored plan.
	if len(subs) == 0 {
		plan := cb.GetPortPlan()
		if len(plan) == 0 {
			return nil
		}
		if err := cb.QueryPorts(ctx, plan); err != nil {
			return err
		}
		subs = cb.PortSubs()
	}

	if len(subs) == 0 {
		return nil
	}

	// Log the port mappings.
	for intended, actual := range subs {
		e.log("  port %s → %s", intended, actual)
	}

	// Apply substitutions to ready check.
	if s.Ready != nil {
		s.Ready.Cmd = substitutePort(s.Ready.Cmd, subs)
	}

	// Apply to commands.
	for i := range s.Commands {
		s.Commands[i].Run = substitutePort(s.Commands[i].Run, subs)
	}
	for i := range s.Run {
		s.Run[i] = substitutePort(s.Run[i], subs)
	}

	// Apply to assertions.
	for i := range s.Assertions {
		s.Assertions[i].Target = substitutePort(s.Assertions[i].Target, subs)
		if s.Assertions[i].Expect.Command != "" {
			s.Assertions[i].Expect.Command = substitutePort(s.Assertions[i].Expect.Command, subs)
		}
		for j := range s.Assertions[i].Args {
			s.Assertions[i].Args[j] = substitutePort(s.Assertions[i].Args[j], subs)
		}
	}

	return nil
}

// substitutePort replaces all ":intendedPort" occurrences with ":actualPort"
// in the given string.
func substitutePort(s string, subs map[string]string) string {
	for intended, actual := range subs {
		if intended == actual {
			continue
		}
		s = strings.ReplaceAll(s, ":"+intended, ":"+actual)
	}
	return s
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
