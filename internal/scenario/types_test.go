package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestLoad(t *testing.T) {
	yaml := `
name: test-scenario
description: A test scenario
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: check-health
    run: curl -sf http://localhost:8080/health
timeout: 5m
assertions:
  - name: port-open
    type: port
    target: "localhost:8080"
    expect:
      open: true
tags:
  - test
  - ci
artifacts:
  container_logs: true
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if s.Name != "test-scenario" {
		t.Errorf("expected name 'test-scenario', got %q", s.Name)
	}
	if s.Backend.Type != "compose" {
		t.Errorf("expected backend type 'compose', got %q", s.Backend.Type)
	}
	if s.Backend.ComposeFile != "compose.yaml" {
		t.Errorf("expected compose_file 'compose.yaml', got %q", s.Backend.ComposeFile)
	}
	if len(s.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(s.Commands))
	}
	if s.Commands[0].Name != "check-health" {
		t.Errorf("expected command name 'check-health', got %q", s.Commands[0].Name)
	}
	if s.Timeout.Minutes() != 5 {
		t.Errorf("expected 5m timeout, got %v", s.Timeout.Duration)
	}
	if len(s.Assertions) != 1 {
		t.Fatalf("expected 1 assertion, got %d", len(s.Assertions))
	}
	if s.Assertions[0].Type != "port" {
		t.Errorf("expected assertion type 'port', got %q", s.Assertions[0].Type)
	}
	if len(s.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(s.Tags))
	}
	if !s.Artifacts.ContainerLogs {
		t.Error("expected container_logs=true")
	}
	if s.Dir != dir {
		t.Errorf("expected dir %q, got %q", dir, s.Dir)
	}
}

func TestLoad_TerraformBackend(t *testing.T) {
	yaml := `
name: tf-test
backend:
  type: terraform
  terraform_dir: ./infra
  terraform_vars:
    project_id: test-project
    region: us-central1
commands:
  - name: validate
    run: terraform validate
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	_ = os.WriteFile(path, []byte(yaml), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Backend.Type != "terraform" {
		t.Errorf("expected terraform, got %q", s.Backend.Type)
	}
	if s.Backend.TerraformDir != "./infra" {
		t.Errorf("expected ./infra, got %q", s.Backend.TerraformDir)
	}
	if s.Backend.TerraformVars["project_id"] != "test-project" {
		t.Error("expected terraform_vars.project_id = test-project")
	}
}

func TestValidate_Valid(t *testing.T) {
	s := &Scenario{
		Name: "valid",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Commands: []Command{{Name: "test", Run: "echo hello"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidate_MissingName(t *testing.T) {
	s := &Scenario{
		Backend:  Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Commands: []Command{{Name: "test", Run: "echo hello"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestValidate_MissingBackendType(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Commands: []Command{{Name: "test", Run: "echo hello"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing backend type")
	}
}

func TestValidate_ComposeRequiresFile(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "compose"},
		Commands: []Command{{Name: "test", Run: "echo hello"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for compose without compose_file")
	}
}

func TestValidate_TerraformRequiresDir(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "terraform"},
		Commands: []Command{{Name: "test", Run: "echo hello"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for terraform without terraform_dir")
	}
}

func TestValidate_NoCommandsOrAssertions(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error when no commands or assertions")
	}
}

func TestValidate_InvalidAssertionType(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Assertions: []Assertion{
			{Name: "bad", Type: "magic"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for invalid assertion type")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()

	// Create a scenario in a subdirectory.
	subDir := filepath.Join(dir, "my-test")
	_ = os.MkdirAll(subDir, 0o755)
	yaml := `name: sub-test
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: test
    run: echo ok`
	_ = os.WriteFile(filepath.Join(subDir, "scenario.yaml"), []byte(yaml), 0o644)

	// Create a scenario as a top-level YAML file.
	yaml2 := `name: top-test
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: test
    run: echo ok`
	_ = os.WriteFile(filepath.Join(dir, "top.yaml"), []byte(yaml2), 0o644)

	scenarios, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 2 {
		t.Errorf("expected 2 scenarios, got %d", len(scenarios))
	}
}

func TestLoad_WithFixture(t *testing.T) {
	yaml := `
name: fixture-test
fixture:
  module: ./fixtures/gcp
  vars:
    project_id: test-proj
    region: us-central1
backend:
  type: github-actions
  repo: org/repo
  workflow: ci.yml
commands:
  - name: check
    run: echo ok
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	_ = os.WriteFile(path, []byte(yaml), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Fixture == nil {
		t.Fatal("expected fixture to be parsed")
	}
	if s.Fixture.Module != "./fixtures/gcp" {
		t.Errorf("expected fixture module './fixtures/gcp', got %q", s.Fixture.Module)
	}
	if s.Fixture.Vars["project_id"] != "test-proj" {
		t.Error("expected fixture vars.project_id = test-proj")
	}
}

func TestValidate_FixtureRequiresModule(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Fixture: &Fixture{},
		Backend: Backend{Type: "github-actions", Repo: "org/repo", Workflow: "ci.yml"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for fixture without module")
	}
}

func TestValidate_FixtureOptional(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected no error without fixture, got: %v", err)
	}
}

func TestValidate_GitHubActionsBackend(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "github-actions", Repo: "org/repo", Workflow: "ci.yml"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidate_GitHubActionsRequiresRepo(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "github-actions", Workflow: "ci.yml"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for github-actions without repo")
	}
}

func TestValidate_GitHubActionsRequiresWorkflow(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "github-actions", Repo: "org/repo"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for github-actions without workflow")
	}
}

func TestValidate_GitHubAssertionTypes(t *testing.T) {
	for _, typ := range []string{"github-run", "github-job", "github-step", "github-artifact", "ghcollect"} {
		s := &Scenario{
			Name:    "test",
			Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
			Assertions: []Assertion{{
				Name:   "check",
				Type:   typ,
				Target: "./snap/manifest.json",
			}},
		}
		if err := s.Validate(); err != nil {
			t.Errorf("expected %s assertion type to be valid, got: %v", typ, err)
		}
	}
}

func TestValidate_GhcollectRequiresTarget(t *testing.T) {
	s := &Scenario{
		Name:    "gc",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Assertions: []Assertion{
			{Name: "a", Type: "ghcollect"},
		},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for ghcollect without target")
	}
}

func TestValidate_AssertionsOnly(t *testing.T) {
	s := &Scenario{
		Name:    "assertions-only",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Assertions: []Assertion{
			{Name: "check", Type: "http"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected assertions-only to be valid, got: %v", err)
	}
}

// ── Unified format tests ──────────────────────────────────────────────────

func TestLoad_UnifiedFormat(t *testing.T) {
	content := `
name: unified-test
description: A unified format scenario
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: test
run:
  - curl -sf http://localhost:8080
assertions:
  - name: web-up
    type: port
    target: "localhost:8080"
    expect:
      open: true
timeout: 3m
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Name != "unified-test" {
		t.Errorf("expected name 'unified-test', got %q", s.Name)
	}
	if !s.IsUnifiedFormat() {
		t.Error("expected IsUnifiedFormat() == true")
	}
	if len(s.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(s.Services))
	}
	if s.Services["web"].Image != "nginx:alpine" {
		t.Errorf("expected web image 'nginx:alpine', got %q", s.Services["web"].Image)
	}
	if len(s.Run) != 1 {
		t.Errorf("expected 1 run command, got %d", len(s.Run))
	}
	if s.Timeout.Duration != 3*time.Minute {
		t.Errorf("expected 3m timeout, got %v", s.Timeout.Duration)
	}
}

func TestIsUnifiedFormat_True(t *testing.T) {
	s := &Scenario{
		Services: map[string]ComposeService{
			"web": {Image: "nginx"},
		},
	}
	if !s.IsUnifiedFormat() {
		t.Error("expected true when services present")
	}
}

func TestIsUnifiedFormat_False(t *testing.T) {
	s := &Scenario{}
	if s.IsUnifiedFormat() {
		t.Error("expected false when services empty")
	}
}

func TestIsUnifiedFormat_FalseEmptyMap(t *testing.T) {
	s := &Scenario{
		Services: map[string]ComposeService{},
	}
	if s.IsUnifiedFormat() {
		t.Error("expected false when services map is empty")
	}
}

func TestValidate_UnifiedFormat_Valid(t *testing.T) {
	s := &Scenario{
		Name: "unified",
		Services: map[string]ComposeService{
			"web": {Image: "nginx:alpine"},
		},
		Run: []string{"curl http://localhost"},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidate_UnifiedFormat_MissingName(t *testing.T) {
	s := &Scenario{
		Services: map[string]ComposeService{
			"web": {Image: "nginx:alpine"},
		},
		Run: []string{"curl http://localhost"},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for missing name in unified format")
	}
}

func TestValidate_UnifiedFormat_ServiceWithoutImageOrBuild(t *testing.T) {
	s := &Scenario{
		Name: "bad-service",
		Services: map[string]ComposeService{
			"web": {Ports: []string{"8080:80"}},
		},
		Run: []string{"echo ok"},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for service without image or build")
	}
	if !strings.Contains(err.Error(), "must have either image or build") {
		t.Errorf("expected 'must have either image or build' error, got: %v", err)
	}
}

func TestValidate_UnifiedFormat_NoRunCommandsOrAssertions(t *testing.T) {
	s := &Scenario{
		Name: "empty",
		Services: map[string]ComposeService{
			"web": {Image: "nginx"},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for no run/commands/assertions")
	}
	if !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected 'at least one' error, got: %v", err)
	}
}

func TestValidate_UnifiedFormat_WithBuild(t *testing.T) {
	s := &Scenario{
		Name: "build-service",
		Services: map[string]ComposeService{
			"app": {Build: &ComposeBuild{Context: "."}},
		},
		Assertions: []Assertion{{Name: "check", Type: "http"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid with build, got: %v", err)
	}
}

func TestValidate_UnifiedFormat_AssertionsOnlyValid(t *testing.T) {
	s := &Scenario{
		Name: "assertions-only-unified",
		Services: map[string]ComposeService{
			"web": {Image: "nginx"},
		},
		Assertions: []Assertion{{Name: "check", Type: "port"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidate_UnifiedFormat_CommandsOnlyValid(t *testing.T) {
	s := &Scenario{
		Name: "commands-only-unified",
		Services: map[string]ComposeService{
			"web": {Image: "nginx"},
		},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

// ── Old format additional validation ──────────────────────────────────────

func TestValidate_OldFormat_ComposeWithoutFile(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "compose"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for compose without compose_file")
	}
	if !strings.Contains(err.Error(), "compose_file") {
		t.Errorf("expected compose_file error, got: %v", err)
	}
}

func TestValidate_OldFormat_UnsupportedBackendType(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "kubernetes"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported backend type")
	}
	if !strings.Contains(err.Error(), "unsupported backend type") {
		t.Errorf("expected 'unsupported backend type' error, got: %v", err)
	}
}

func TestValidate_AssertionUnsupportedType(t *testing.T) {
	s := &Scenario{
		Name:    "test",
		Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Assertions: []Assertion{
			{Name: "bad", Type: "smoke-signal"},
		},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported assertion type")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("expected 'unsupported type' error, got: %v", err)
	}
}

func TestValidate_HybridBackend(t *testing.T) {
	s := &Scenario{
		Name:    "hybrid",
		Backend: Backend{Type: "hybrid", ComposeFile: "compose.yaml", TerraformDir: "./infra"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("expected valid hybrid, got: %v", err)
	}
}

func TestValidate_HybridBackend_MissingBoth(t *testing.T) {
	s := &Scenario{
		Name:    "hybrid",
		Backend: Backend{Type: "hybrid"},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err == nil {
		t.Error("expected error for hybrid without compose_file, terraform_dir, or github repo+workflow")
	}
}

func TestValidate_HybridBackend_GitHubOnly(t *testing.T) {
	s := &Scenario{
		Name: "hybrid-ga",
		Backend: Backend{
			Type:     "hybrid",
			Repo:     "acme/lab",
			Workflow: "smoke.yml",
		},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("expected valid hybrid with github-actions only: %v", err)
	}
}

func TestValidate_HybridBackend_GitHubPartial(t *testing.T) {
	s := &Scenario{
		Name: "hybrid-partial",
		Backend: Backend{
			Type:     "hybrid",
			Repo:     "acme/lab",
			Workflow: "",
		},
		Commands: []Command{{Name: "test", Run: "echo ok"}},
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error when only repo is set for github-actions component")
	}
}

// ── Duration YAML unmarshaling ────────────────────────────────────────────

func TestDuration_UnmarshalYAML_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{`"5m"`, 5 * time.Minute},
		{`"30s"`, 30 * time.Second},
		{`"2h"`, 2 * time.Hour},
		{`"1m30s"`, 90 * time.Second},
		{`"500ms"`, 500 * time.Millisecond},
	}
	for _, tc := range cases {
		var d Duration
		if err := yaml.Unmarshal([]byte(tc.input), &d); err != nil {
			t.Errorf("Unmarshal(%s): %v", tc.input, err)
			continue
		}
		if d.Duration != tc.want {
			t.Errorf("Unmarshal(%s) = %v, want %v", tc.input, d.Duration, tc.want)
		}
	}
}

func TestDuration_UnmarshalYAML_Invalid(t *testing.T) {
	cases := []string{
		`"not-a-duration"`,
		`"5 minutes"`,
		`"abc"`,
	}
	for _, input := range cases {
		var d Duration
		if err := yaml.Unmarshal([]byte(input), &d); err == nil {
			t.Errorf("expected error for %s, got none", input)
		}
	}
}

func TestDuration_MarshalYAML(t *testing.T) {
	d := Duration{Duration: 5 * time.Minute}
	data, err := yaml.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != "5m0s" {
		t.Errorf("expected '5m0s', got %q", got)
	}
}

// ── Load edge cases ──────────────────────────────────────────────────────

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("{{invalid yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for malformed YAML")
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/scenario.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoad_DefaultTimeout(t *testing.T) {
	content := `
name: no-timeout
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: test
    run: echo ok
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	_ = os.WriteFile(path, []byte(content), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Timeout.Duration != 10*time.Minute {
		t.Errorf("expected default timeout 10m, got %v", s.Timeout.Duration)
	}
}

func TestLoad_ReadyCheckDefaults(t *testing.T) {
	content := `
name: ready-test
services:
  web:
    image: nginx
ready:
  cmd: curl -sf http://localhost
run:
  - echo ok
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	_ = os.WriteFile(path, []byte(content), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Ready == nil {
		t.Fatal("expected ready to be non-nil")
	}
	if s.Ready.Timeout.Duration != 60*time.Second {
		t.Errorf("expected ready timeout 60s, got %v", s.Ready.Timeout.Duration)
	}
	if s.Ready.Interval.Duration != 2*time.Second {
		t.Errorf("expected ready interval 2s, got %v", s.Ready.Interval.Duration)
	}
}

func TestLoad_SetsDir(t *testing.T) {
	content := `
name: dir-test
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: test
    run: echo ok
`
	dir := t.TempDir()
	path := filepath.Join(dir, "scenario.yaml")
	_ = os.WriteFile(path, []byte(content), 0o644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Dir != dir {
		t.Errorf("expected Dir %q, got %q", dir, s.Dir)
	}
}

func TestLoadDir_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	scenarios, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 0 {
		t.Errorf("expected 0 scenarios, got %d", len(scenarios))
	}
}

func TestLoadDir_YMLExtension(t *testing.T) {
	dir := t.TempDir()
	content := `
name: yml-test
backend:
  type: compose
  compose_file: compose.yaml
commands:
  - name: test
    run: echo ok
`
	_ = os.WriteFile(filepath.Join(dir, "test.yml"), []byte(content), 0o644)

	scenarios, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 1 {
		t.Errorf("expected 1 scenario, got %d", len(scenarios))
	}
}

func TestLoadDir_SkipsNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# hello"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte("{}"), 0o644)

	scenarios, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 0 {
		t.Errorf("expected 0 scenarios, got %d", len(scenarios))
	}
}

func TestLoadDir_NonexistentDir(t *testing.T) {
	_, err := LoadDir("/nonexistent/dir")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLoadDir_RecursesIntoSubdirectories(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "databases")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	scenario1 := `
name: redis-test
services:
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
assertions:
  - name: check
    type: port
    target: "localhost:6379"
    expect:
      open: true
`
	scenario2 := `
name: postgres-test
services:
  pg:
    image: postgres:16
    ports: ["5432:5432"]
assertions:
  - name: check
    type: port
    target: "localhost:5432"
    expect:
      open: true
`
	_ = os.WriteFile(filepath.Join(sub, "redis.yaml"), []byte(scenario1), 0o644)
	_ = os.WriteFile(filepath.Join(sub, "postgres.yaml"), []byte(scenario2), 0o644)

	scenarios, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 2 {
		t.Errorf("expected 2 scenarios from nested dir, got %d", len(scenarios))
	}
}

func TestLoadDir_SkipsSupportDirectories(t *testing.T) {
	// Simulates: envoy.yaml (scenario) + envoy/ (config dir with non-scenario yaml)
	root := t.TempDir()

	scenarioYAML := `
name: envoy-test
services:
  envoy:
    image: envoyproxy/envoy:v1.29-latest
    ports: ["9901:9901"]
assertions:
  - name: check
    type: port
    target: "localhost:9901"
    expect:
      open: true
`
	_ = os.WriteFile(filepath.Join(root, "envoy.yaml"), []byte(scenarioYAML), 0o644)

	// Create support directory with a non-scenario YAML config file.
	supportDir := filepath.Join(root, "envoy")
	if err := os.MkdirAll(supportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(supportDir, "envoy.yaml"), []byte("admin:\n  address:\n    socket_address: { address: 0.0.0.0, port_value: 9901 }"), 0o644)

	scenarios, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 1 {
		t.Errorf("expected 1 scenario (support dir skipped), got %d", len(scenarios))
	}
	if scenarios[0].Name != "envoy-test" {
		t.Errorf("expected scenario name 'envoy-test', got %q", scenarios[0].Name)
	}
}

func TestLoadDir_DeepNesting(t *testing.T) {
	// tests/category/subcategory/scenario.yaml — three levels deep
	root := t.TempDir()
	deep := filepath.Join(root, "infra", "gcp")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `
name: gcp-test
services:
  app:
    image: nginx
    ports: ["8080:80"]
assertions:
  - name: check
    type: port
    target: "localhost:8080"
    expect:
      open: true
`
	_ = os.WriteFile(filepath.Join(deep, "compute.yaml"), []byte(content), 0o644)

	scenarios, err := LoadDir(root)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(scenarios) != 1 {
		t.Errorf("expected 1 scenario from deep nesting, got %d", len(scenarios))
	}
}

// ── Validate command fields ───────────────────────────────────────────────

func TestValidate_CommandMissingName(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Commands: []Command{{Run: "echo ok"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for command without name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_CommandMissingRun(t *testing.T) {
	s := &Scenario{
		Name:     "test",
		Backend:  Backend{Type: "compose", ComposeFile: "compose.yaml"},
		Commands: []Command{{Name: "test"}},
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for command without run")
	}
	if !strings.Contains(err.Error(), "run is required") {
		t.Errorf("unexpected error: %v", err)
	}
}
