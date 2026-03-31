package artifact

import (
	"testing"
	"time"

	"github.com/stormbane-security/drydock/internal/runner"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	record := &RunRecord{
		ID:        "test-run-001",
		Scenario:  "my-scenario",
		StartedAt: time.Now().UTC(),
		FinishedAt: time.Now().UTC().Add(30 * time.Second),
		Duration:  30 * time.Second,
		Status:    "pass",
		CommandResults: []runner.Result{
			{Name: "check", Command: "echo ok", ExitCode: 0, Stdout: "ok\n"},
		},
		Logs: map[string]string{
			"web": "server started on :8080",
		},
	}

	if err := store.Save(record); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load("test-run-001")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.ID != "test-run-001" {
		t.Errorf("expected ID 'test-run-001', got %q", loaded.ID)
	}
	if loaded.Status != "pass" {
		t.Errorf("expected status 'pass', got %q", loaded.Status)
	}
	if len(loaded.CommandResults) != 1 {
		t.Errorf("expected 1 command result, got %d", len(loaded.CommandResults))
	}
}

func TestStore_List(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Save(&RunRecord{ID: "run-a", Status: "pass"})
	store.Save(&RunRecord{ID: "run-b", Status: "fail"})

	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 runs, got %d", len(ids))
	}
}

func TestGenerateRunID(t *testing.T) {
	id := GenerateRunID("my-scenario")
	if id == "" {
		t.Fatal("expected non-empty run ID")
	}
	if len(id) < 20 {
		t.Errorf("run ID seems too short: %q", id)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with/slash", "with_slash"},
		{"with:colon", "with_colon"},
		{"normal-name.log", "normal-name.log"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
