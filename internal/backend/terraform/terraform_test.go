package terraform

import (
	"context"
	"strings"
	"testing"
)

func TestNew_TFInAutomation(t *testing.T) {
	b := New("/work", "infra", nil, "default", true, nil)
	found := false
	for _, e := range b.env {
		if e == "TF_IN_AUTOMATION=1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TF_IN_AUTOMATION=1 in env")
	}
}

func TestNew_EnvVarsPreserved(t *testing.T) {
	env := map[string]string{
		"AWS_REGION":  "us-east-1",
		"TF_VAR_name": "test",
	}
	b := New("/work", "infra", nil, "default", true, env)
	// Should have the 2 user env vars + TF_IN_AUTOMATION.
	if len(b.env) != 3 {
		t.Errorf("expected 3 env vars, got %d: %v", len(b.env), b.env)
	}
}

func TestNew_FieldAssignment(t *testing.T) {
	vars := map[string]string{"region": "us-west-2"}
	b := New("/work", "modules/base", vars, "my-workspace", false, nil)

	if b.dir != "/work" {
		t.Errorf("dir = %q, want '/work'", b.dir)
	}
	if b.tfDir != "modules/base" {
		t.Errorf("tfDir = %q, want 'modules/base'", b.tfDir)
	}
	if b.vars["region"] != "us-west-2" {
		t.Errorf("vars[region] = %q, want 'us-west-2'", b.vars["region"])
	}
	if b.workspace != "my-workspace" {
		t.Errorf("workspace = %q, want 'my-workspace'", b.workspace)
	}
	if b.autoApprove {
		t.Error("autoApprove should be false")
	}
}

func TestName(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	if b.Name() != "terraform" {
		t.Errorf("Name() = %q, want 'terraform'", b.Name())
	}
}

func TestTfPath(t *testing.T) {
	b := New("/work", "modules/infra", nil, "", true, nil)
	path := b.tfPath()
	if path != "/work/modules/infra" {
		t.Errorf("tfPath() = %q, want '/work/modules/infra'", path)
	}
}

func TestTfPath_SimpleDir(t *testing.T) {
	b := New("/project", ".", nil, "", true, nil)
	path := b.tfPath()
	// filepath.Join cleans the path, so "/project" + "." = "/project"
	if path != "/project" {
		t.Errorf("tfPath() = %q, want '/project'", path)
	}
}

func TestWaitReady_NoOp(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	err := b.WaitReady(context.Background())
	if err != nil {
		t.Errorf("WaitReady should be no-op, got: %v", err)
	}
}

func TestNew_NilVars(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	if b.vars != nil {
		t.Error("expected nil vars")
	}
}

func TestNew_EmptyWorkspace(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	if b.workspace != "" {
		t.Errorf("expected empty workspace, got %q", b.workspace)
	}
}

func TestNew_NilEnv(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	// Should still have TF_IN_AUTOMATION.
	if len(b.env) != 1 {
		t.Errorf("expected 1 env var (TF_IN_AUTOMATION), got %d", len(b.env))
	}
}

func TestNew_AutoApproveTrue(t *testing.T) {
	b := New("/work", "tf", nil, "", true, nil)
	if !b.autoApprove {
		t.Error("autoApprove should be true")
	}
}

func TestNew_EnvFormat(t *testing.T) {
	b := New("/work", "tf", nil, "", true, map[string]string{"KEY": "VALUE"})
	for _, e := range b.env {
		if !strings.Contains(e, "=") {
			t.Errorf("env var missing '=': %q", e)
		}
	}
}
