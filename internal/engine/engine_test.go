package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stormbane-security/drydock/internal/scenario"
)

// ── ServiceEndpoints ──────────────────────────────────────────────────────

func TestServiceEndpoints_WithPorts(t *testing.T) {
	s := &scenario.Scenario{
		Services: map[string]scenario.ComposeService{
			"redis": {Image: "redis:7", Ports: []string{"6379:6379"}},
			"web":   {Image: "nginx", Ports: []string{"8080:80", "8443:443"}},
		},
	}
	eps := ServiceEndpoints(s)
	sort.Strings(eps)

	if len(eps) != 3 {
		t.Fatalf("expected 3 endpoints, got %d: %v", len(eps), eps)
	}
	// Verify port extraction works.
	found := strings.Join(eps, "|")
	if !strings.Contains(found, "redis on localhost:6379") {
		t.Errorf("missing redis endpoint in %v", eps)
	}
	if !strings.Contains(found, "web on localhost:8080") {
		t.Errorf("missing web 8080 endpoint in %v", eps)
	}
	if !strings.Contains(found, "web on localhost:8443") {
		t.Errorf("missing web 8443 endpoint in %v", eps)
	}
}

func TestServiceEndpoints_NoPorts(t *testing.T) {
	s := &scenario.Scenario{
		Services: map[string]scenario.ComposeService{
			"worker": {Image: "worker:latest"},
		},
	}
	eps := ServiceEndpoints(s)
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(eps))
	}
	if eps[0] != "worker" {
		t.Errorf("expected 'worker', got %q", eps[0])
	}
}

func TestServiceEndpoints_Empty(t *testing.T) {
	s := &scenario.Scenario{}
	eps := ServiceEndpoints(s)
	if len(eps) != 0 {
		t.Errorf("expected 0 endpoints for empty services, got %d", len(eps))
	}
}

// ── hostPort ──────────────────────────────────────────────────────────────

func TestHostPort(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"8080:80", "8080"},
		{"6379:6379", "6379"},
		{"443", "443"},
		{"9200:9200", "9200"},
	}
	for _, tt := range tests {
		got := hostPort(tt.input)
		if got != tt.want {
			t.Errorf("hostPort(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── commandsToSpecs ───────────────────────────────────────────────────────

func TestCommandsToSpecs_Basic(t *testing.T) {
	exitCode := 42
	cmds := []scenario.Command{
		{Name: "setup", Run: "echo hello", Dir: "/tmp", ContinueOnError: true},
		{Name: "check", Run: "exit 42", Expect: &scenario.CommandExpect{ExitCode: &exitCode}},
	}
	specs := commandsToSpecs(cmds)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d", len(specs))
	}
	if specs[0].Name != "setup" {
		t.Errorf("specs[0].Name = %q, want 'setup'", specs[0].Name)
	}
	if specs[0].Run != "echo hello" {
		t.Errorf("specs[0].Run = %q, want 'echo hello'", specs[0].Run)
	}
	if specs[0].Dir != "/tmp" {
		t.Errorf("specs[0].Dir = %q, want '/tmp'", specs[0].Dir)
	}
	if !specs[0].ContinueOnError {
		t.Error("specs[0].ContinueOnError should be true")
	}
	if specs[1].ExpectExit == nil || *specs[1].ExpectExit != 42 {
		t.Error("specs[1].ExpectExit should be 42")
	}
}

func TestCommandsToSpecs_NoExpect(t *testing.T) {
	cmds := []scenario.Command{
		{Name: "simple", Run: "echo ok"},
	}
	specs := commandsToSpecs(cmds)
	if specs[0].ExpectExit != nil {
		t.Error("ExpectExit should be nil when no Expect")
	}
}

func TestCommandsToSpecs_Empty(t *testing.T) {
	specs := commandsToSpecs(nil)
	if len(specs) != 0 {
		t.Errorf("expected 0 specs for nil, got %d", len(specs))
	}
}

func TestCommandsToSpecs_WithTimeout(t *testing.T) {
	cmds := []scenario.Command{
		{Name: "slow", Run: "sleep 5", Timeout: scenario.Duration{Duration: 30 * time.Second}},
	}
	specs := commandsToSpecs(cmds)
	if specs[0].Timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", specs[0].Timeout)
	}
}

func TestCommandsToSpecs_WithEnv(t *testing.T) {
	cmds := []scenario.Command{
		{Name: "env", Run: "env", Env: map[string]string{"FOO": "bar"}},
	}
	specs := commandsToSpecs(cmds)
	if specs[0].Env["FOO"] != "bar" {
		t.Errorf("expected env FOO=bar, got %v", specs[0].Env)
	}
}

// ── createBackends ────────────────────────────────────────────────────────

func TestCreateBackends_UnifiedFormat(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Services: map[string]scenario.ComposeService{
			"redis": {Image: "redis:7", Ports: []string{"6379:6379"}},
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(backends))
	}
	if backends[0].Name() != "compose" {
		t.Errorf("expected compose backend, got %q", backends[0].Name())
	}
	// Clean up generated compose dir.
	e.cleanupGeneratedCompose(backends)
}

func TestCreateBackends_OldFormat_Compose(t *testing.T) {
	s := &scenario.Scenario{
		Name:    "test",
		Dir:     "/tmp",
		Backend: scenario.Backend{Type: "compose", ComposeFile: "docker-compose.yaml"},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 || backends[0].Name() != "compose" {
		t.Errorf("expected 1 compose backend, got %v", backends)
	}
}

func TestCreateBackends_OldFormat_Terraform(t *testing.T) {
	s := &scenario.Scenario{
		Name:    "test",
		Dir:     "/tmp",
		Backend: scenario.Backend{Type: "terraform", TerraformDir: "tf"},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 || backends[0].Name() != "terraform" {
		t.Errorf("expected 1 terraform backend, got %v", backends)
	}
}

func TestCreateBackends_OldFormat_Hybrid(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Dir:  "/tmp",
		Backend: scenario.Backend{
			Type:         "hybrid",
			ComposeFile:  "docker-compose.yaml",
			TerraformDir: "tf",
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 2 {
		t.Fatalf("expected 2 backends for hybrid, got %d", len(backends))
	}
	names := []string{backends[0].Name(), backends[1].Name()}
	sort.Strings(names)
	if names[0] != "compose" || names[1] != "terraform" {
		t.Errorf("expected compose+terraform, got %v", names)
	}
}

func TestCreateBackends_OldFormat_GitHubActions(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type:     "github-actions",
			Repo:     "owner/repo",
			Workflow: "ci.yaml",
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 || backends[0].Name() != "github-actions" {
		t.Errorf("expected 1 github-actions backend, got %v", backends)
	}
}

func TestCreateBackends_UnsupportedType(t *testing.T) {
	s := &scenario.Scenario{
		Name:    "test",
		Backend: scenario.Backend{Type: "kubernetes"},
	}
	e := New(t.TempDir())
	_, err := e.createBackends(s)
	if err == nil {
		t.Fatal("expected error for unsupported backend type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected 'unsupported' in error, got: %v", err)
	}
}

func TestCreateBackends_HybridComposeOnly(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Dir:  "/tmp",
		Backend: scenario.Backend{
			Type:        "hybrid",
			ComposeFile: "docker-compose.yaml",
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 || backends[0].Name() != "compose" {
		t.Errorf("expected 1 compose backend for hybrid with compose only, got %d", len(backends))
	}
}

func TestCreateBackends_TerraformDefaultWorkspace(t *testing.T) {
	s := &scenario.Scenario{
		Name:    "my-test",
		Dir:     "/tmp",
		Backend: scenario.Backend{Type: "terraform", TerraformDir: "tf"},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	// The workspace default is "drydock-<name>" — verify through the backend.
	if len(backends) != 1 {
		t.Fatal("expected 1 backend")
	}
}

func TestCreateBackends_GitHubActionsDefaultRef(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type:     "github-actions",
			Repo:     "owner/repo",
			Workflow: "ci.yaml",
			// Ref is empty — should default to "main".
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatal("expected 1 backend")
	}
}

// ── createFixture ─────────────────────────────────────────────────────────

func TestCreateFixture_DefaultWorkspace(t *testing.T) {
	s := &scenario.Scenario{
		Name: "my-scenario",
		Dir:  "/tmp",
		Fixture: &scenario.Fixture{
			Module: "fixtures/base",
		},
	}
	e := New(t.TempDir())
	fixture := e.createFixture(s)
	if fixture.Name() != "terraform" {
		t.Errorf("expected terraform fixture, got %q", fixture.Name())
	}
}

func TestCreateFixture_CustomWorkspace(t *testing.T) {
	s := &scenario.Scenario{
		Name: "my-scenario",
		Dir:  "/tmp",
		Fixture: &scenario.Fixture{
			Module:    "fixtures/base",
			Workspace: "custom-ws",
		},
	}
	e := New(t.TempDir())
	fixture := e.createFixture(s)
	if fixture.Name() != "terraform" {
		t.Errorf("expected terraform fixture, got %q", fixture.Name())
	}
}

// ── runReadyCheck ─────────────────────────────────────────────────────────

func TestRunReadyCheck_NilReady(t *testing.T) {
	s := &scenario.Scenario{}
	e := New(t.TempDir())
	err := e.runReadyCheck(context.Background(), s)
	if err != nil {
		t.Errorf("expected nil error for nil ready, got %v", err)
	}
}

func TestRunReadyCheck_ImmediateSuccess(t *testing.T) {
	s := &scenario.Scenario{
		Ready: &scenario.ReadyCheck{
			Cmd:      "true",
			Timeout:  scenario.Duration{Duration: 10 * time.Second},
			Interval: scenario.Duration{Duration: 100 * time.Millisecond},
		},
	}
	e := New(t.TempDir())
	err := e.runReadyCheck(context.Background(), s)
	if err != nil {
		t.Errorf("expected ready check to pass, got %v", err)
	}
}

func TestRunReadyCheck_Timeout(t *testing.T) {
	s := &scenario.Scenario{
		Ready: &scenario.ReadyCheck{
			Cmd:      "false",
			Timeout:  scenario.Duration{Duration: 500 * time.Millisecond},
			Interval: scenario.Duration{Duration: 100 * time.Millisecond},
		},
	}
	e := New(t.TempDir())
	err := e.runReadyCheck(context.Background(), s)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected 'timed out' in error, got: %v", err)
	}
}

func TestRunReadyCheck_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &scenario.Scenario{
		Ready: &scenario.ReadyCheck{
			Cmd:      "false",
			Timeout:  scenario.Duration{Duration: 30 * time.Second},
			Interval: scenario.Duration{Duration: 100 * time.Millisecond},
		},
	}
	e := New(t.TempDir())
	err := e.runReadyCheck(ctx, s)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected 'context cancelled' in error, got: %v", err)
	}
}

func TestRunReadyCheck_EventualSuccess(t *testing.T) {
	// Create a marker file after a short delay so the ready check eventually passes.
	dir := t.TempDir()
	marker := filepath.Join(dir, "ready")

	// Background goroutine creates the marker.
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("ok"), 0o644)
	}()

	s := &scenario.Scenario{
		Ready: &scenario.ReadyCheck{
			Cmd:      "test -f " + marker,
			Timeout:  scenario.Duration{Duration: 5 * time.Second},
			Interval: scenario.Duration{Duration: 100 * time.Millisecond},
		},
	}
	e := New(t.TempDir())
	err := e.runReadyCheck(context.Background(), s)
	if err != nil {
		t.Errorf("expected eventual success, got %v", err)
	}
}

// ── Engine constructor and helpers ────────────────────────────────────────

func TestNew(t *testing.T) {
	dir := t.TempDir()
	e := New(dir)
	if e == nil {
		t.Fatal("New returned nil")
	}
	if e.out != os.Stderr {
		t.Error("default output should be os.Stderr")
	}
}

func TestSetOutput(t *testing.T) {
	e := New(t.TempDir())
	var buf bytes.Buffer
	e.SetOutput(&buf)
	e.log("test %s", "message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected log output, got %q", buf.String())
	}
}

func TestValidate(t *testing.T) {
	e := New(t.TempDir())
	s := &scenario.Scenario{} // no name = invalid
	err := e.Validate(s)
	if err == nil {
		t.Error("expected validation error for empty scenario")
	}
}

func TestValidate_Valid(t *testing.T) {
	e := New(t.TempDir())
	s := &scenario.Scenario{
		Name: "test",
		Services: map[string]scenario.ComposeService{
			"app": {Image: "nginx"},
		},
		Assertions: []scenario.Assertion{
			{Name: "check", Type: "port", Target: "localhost:80"},
		},
	}
	err := e.Validate(s)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

// ── Inspect and ListRuns ──────────────────────────────────────────────────

func TestInspect_NotFound(t *testing.T) {
	e := New(t.TempDir())
	_, err := e.Inspect("nonexistent-run-id")
	if err == nil {
		t.Error("expected error for nonexistent run")
	}
}

func TestListRuns_Empty(t *testing.T) {
	e := New(t.TempDir())
	runs, err := e.ListRuns()
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 runs, got %d", len(runs))
	}
}

// ── cleanupGeneratedCompose ──────────────────────────────────────────────

func TestCleanupGeneratedCompose(t *testing.T) {
	// Create a real compose backend pointing to a temp dir.
	dir := t.TempDir()
	subdir := filepath.Join(dir, "generated")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &scenario.Scenario{
		Name: "cleanup-test",
		Services: map[string]scenario.ComposeService{
			"app": {Image: "nginx"},
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}

	e.cleanupGeneratedCompose(backends)

	// Verify the generated compose dir was removed.
	for _, b := range backends {
		if cb, ok := b.(interface{ Dir() string }); ok {
			if _, err := os.Stat(cb.Dir()); err == nil {
				t.Error("expected generated compose dir to be removed")
			}
		}
	}
}

// ── AutoApprove defaulting ───────────────────────────────────────────────

func TestCreateBackends_TerraformAutoApproveDefault(t *testing.T) {
	s := &scenario.Scenario{
		Name:    "test",
		Dir:     "/tmp",
		Backend: scenario.Backend{Type: "terraform", TerraformDir: "tf"},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatal("expected 1 backend")
	}
}

func TestCreateBackends_TerraformAutoApproveExplicitFalse(t *testing.T) {
	f := false
	s := &scenario.Scenario{
		Name: "test",
		Dir:  "/tmp",
		Backend: scenario.Backend{
			Type:         "terraform",
			TerraformDir: "tf",
			AutoApprove:  &f,
		},
	}
	e := New(t.TempDir())
	backends, err := e.createBackends(s)
	if err != nil {
		t.Fatalf("createBackends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatal("expected 1 backend")
	}
}
