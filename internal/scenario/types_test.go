package scenario

import (
	"os"
	"path/filepath"
	"testing"
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
	for _, typ := range []string{"github-run", "github-job", "github-step", "github-artifact"} {
		s := &Scenario{
			Name:    "test",
			Backend: Backend{Type: "compose", ComposeFile: "compose.yaml"},
			Assertions: []Assertion{{Name: "check", Type: typ}},
		}
		if err := s.Validate(); err != nil {
			t.Errorf("expected %s assertion type to be valid, got: %v", typ, err)
		}
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
