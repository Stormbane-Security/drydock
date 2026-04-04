package ghactions

import (
	"context"
	"testing"
)

func TestNew_DefaultTrigger(t *testing.T) {
	b := New("owner/repo", "ci.yaml", "main", "", nil, nil)
	if b.trigger != "workflow_dispatch" {
		t.Errorf("expected default trigger 'workflow_dispatch', got %q", b.trigger)
	}
}

func TestNew_CustomTrigger(t *testing.T) {
	b := New("owner/repo", "ci.yaml", "main", "push", nil, nil)
	if b.trigger != "push" {
		t.Errorf("expected trigger 'push', got %q", b.trigger)
	}
}

func TestNew_FieldAssignment(t *testing.T) {
	inputs := map[string]string{"key": "value"}
	env := map[string]string{"GH_TOKEN": "abc"}
	b := New("owner/repo", "test.yaml", "dev", "push", inputs, env)

	if b.repo != "owner/repo" {
		t.Errorf("repo = %q, want 'owner/repo'", b.repo)
	}
	if b.workflow != "test.yaml" {
		t.Errorf("workflow = %q, want 'test.yaml'", b.workflow)
	}
	if b.ref != "dev" {
		t.Errorf("ref = %q, want 'dev'", b.ref)
	}
	if b.inputs["key"] != "value" {
		t.Errorf("inputs[key] = %q, want 'value'", b.inputs["key"])
	}
	if len(b.env) != 1 {
		t.Errorf("expected 1 env var, got %d", len(b.env))
	}
}

func TestName(t *testing.T) {
	b := New("owner/repo", "ci.yaml", "main", "", nil, nil)
	if b.Name() != "github-actions" {
		t.Errorf("Name() = %q, want 'github-actions'", b.Name())
	}
}

func TestNew_NilInputsAndEnv(t *testing.T) {
	b := New("owner/repo", "ci.yaml", "main", "", nil, nil)
	if b.inputs != nil {
		t.Error("expected nil inputs")
	}
	if len(b.env) != 0 {
		t.Errorf("expected 0 env vars, got %d", len(b.env))
	}
}

func TestDestroy_ZeroState(t *testing.T) {
	// Destroy should be safe to call even with no runID or testBranch.
	b := New("owner/repo", "ci.yaml", "main", "", nil, nil)
	err := b.Destroy(context.Background())
	if err != nil {
		t.Errorf("Destroy on zero state should not error, got: %v", err)
	}
}

func TestDestroy_WithRunID(t *testing.T) {
	// Even with a runID set, Destroy ignores gh errors.
	b := New("owner/repo", "ci.yaml", "main", "", nil, nil)
	b.runID = 12345
	b.testBranch = "drydock-test-branch"
	// This will fail because gh isn't authenticated, but Destroy ignores errors.
	err := b.Destroy(context.Background())
	if err != nil {
		t.Errorf("Destroy should not return error (ignores gh failures), got: %v", err)
	}
}

func TestNew_MultipleEnvVars(t *testing.T) {
	env := map[string]string{
		"GH_TOKEN":      "abc",
		"GH_ENTERPRISE": "true",
	}
	b := New("owner/repo", "ci.yaml", "main", "", nil, env)
	if len(b.env) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(b.env))
	}
}
