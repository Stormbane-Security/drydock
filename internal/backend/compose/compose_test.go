package compose

import (
	"testing"
)

// ── allHealthy ────────────────────────────────────────────────────────────

func TestAllHealthy_SingleRunning(t *testing.T) {
	json := `{"State":"running","Health":""}`
	if !allHealthy(json) {
		t.Error("expected healthy for running container with no healthcheck")
	}
}

func TestAllHealthy_MultipleRunning(t *testing.T) {
	json := `{"State":"running","Health":""}
{"State":"running","Health":""}`
	if !allHealthy(json) {
		t.Error("expected healthy for all running containers")
	}
}

func TestAllHealthy_HealthyWithCheck(t *testing.T) {
	json := `{"State":"running","Health":"healthy"}`
	if !allHealthy(json) {
		t.Error("expected healthy for running container with healthy health status")
	}
}

func TestAllHealthy_Unhealthy(t *testing.T) {
	json := `{"State":"running","Health":"unhealthy"}`
	if allHealthy(json) {
		t.Error("expected not healthy for unhealthy container")
	}
}

func TestAllHealthy_Starting(t *testing.T) {
	json := `{"State":"running","Health":"starting"}`
	if allHealthy(json) {
		t.Error("expected not healthy for starting container")
	}
}

func TestAllHealthy_NotRunning(t *testing.T) {
	json := `{"State":"exited","Health":""}`
	if allHealthy(json) {
		t.Error("expected not healthy for exited container")
	}
}

func TestAllHealthy_MixedStates(t *testing.T) {
	json := `{"State":"running","Health":"healthy"}
{"State":"running","Health":"unhealthy"}`
	if allHealthy(json) {
		t.Error("expected not healthy when one container is unhealthy")
	}
}

func TestAllHealthy_OneExited(t *testing.T) {
	json := `{"State":"running","Health":""}
{"State":"exited","Health":""}`
	if allHealthy(json) {
		t.Error("expected not healthy when one container is exited")
	}
}

func TestAllHealthy_EmptyInput(t *testing.T) {
	// Empty input after TrimSpace produces [""], which skips the empty line
	// and vacuously returns true (no unhealthy container found).
	// This is the current behavior — callers should not pass empty strings.
	if !allHealthy("") {
		t.Error("empty input vacuously returns true (no containers to check)")
	}
}

func TestAllHealthy_InvalidJSON(t *testing.T) {
	if allHealthy("not json") {
		t.Error("expected not healthy for invalid JSON")
	}
}

func TestAllHealthy_EmptyLines(t *testing.T) {
	json := `{"State":"running","Health":""}

{"State":"running","Health":"healthy"}
`
	if !allHealthy(json) {
		t.Error("expected healthy (empty lines should be skipped)")
	}
}

func TestAllHealthy_CreatedState(t *testing.T) {
	json := `{"State":"created","Health":""}`
	if allHealthy(json) {
		t.Error("expected not healthy for created (not yet running) container")
	}
}

func TestAllHealthy_RestartingState(t *testing.T) {
	json := `{"State":"restarting","Health":""}`
	if allHealthy(json) {
		t.Error("expected not healthy for restarting container")
	}
}

// ── New ───────────────────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	b := New("/work", "compose.yaml", "test-project", map[string]string{"FOO": "bar"})
	if b.Name() != "compose" {
		t.Errorf("Name() = %q, want 'compose'", b.Name())
	}
	if b.Dir() != "/work" {
		t.Errorf("Dir() = %q, want '/work'", b.Dir())
	}
	if b.composeFile != "compose.yaml" {
		t.Errorf("composeFile = %q, want 'compose.yaml'", b.composeFile)
	}
	if b.projectName != "test-project" {
		t.Errorf("projectName = %q, want 'test-project'", b.projectName)
	}
}

func TestNew_NilEnv(t *testing.T) {
	b := New("/work", "compose.yaml", "proj", nil)
	if len(b.env) != 0 {
		t.Errorf("expected 0 env vars, got %d", len(b.env))
	}
}

func TestNew_MultipleEnv(t *testing.T) {
	b := New("/work", "compose.yaml", "proj", map[string]string{
		"A": "1",
		"B": "2",
	})
	if len(b.env) != 2 {
		t.Errorf("expected 2 env vars, got %d", len(b.env))
	}
	// Verify format is KEY=VALUE.
	for _, e := range b.env {
		if len(e) < 3 { // minimum "X=Y"
			t.Errorf("env var too short: %q", e)
		}
	}
}

func TestNew_EmptyProjectName(t *testing.T) {
	b := New("/work", "compose.yaml", "", nil)
	if b.projectName != "" {
		t.Errorf("expected empty project name, got %q", b.projectName)
	}
}

// ── Outputs ──────────────────────────────────────────────────────────────

func TestOutputs_AlwaysEmpty(t *testing.T) {
	b := New("/work", "compose.yaml", "proj", nil)
	// Outputs doesn't use any external commands — it always returns empty.
	outs, err := b.Outputs(nil)
	if err != nil {
		t.Errorf("Outputs returned error: %v", err)
	}
	if len(outs) != 0 {
		t.Errorf("expected empty outputs, got %d entries", len(outs))
	}
}
