package runner

import (
	"context"
	"testing"
)

func TestRun_Success(t *testing.T) {
	result := Run(context.Background(), "echo", "echo hello", "", nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestRun_Failure(t *testing.T) {
	result := Run(context.Background(), "fail", "exit 42", "", nil)
	if result.ExitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestRun_WithEnv(t *testing.T) {
	result := Run(context.Background(), "env", "echo $MY_VAR", "", map[string]string{"MY_VAR": "hello"})
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", result.Stdout)
	}
}

func TestRun_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	result := Run(ctx, "sleep", "sleep 60", "", nil)
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for cancelled command")
	}
}

func TestRunAll_StopsOnFailure(t *testing.T) {
	specs := []CommandSpec{
		{Name: "ok", Run: "echo first"},
		{Name: "fail", Run: "exit 1"},
		{Name: "never", Run: "echo never"},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 2 {
		t.Errorf("expected 2 results (stop on failure), got %d", len(results))
	}
}

func TestRunAll_ContinueOnError(t *testing.T) {
	specs := []CommandSpec{
		{Name: "ok", Run: "echo first"},
		{Name: "fail", Run: "exit 1", ContinueOnError: true},
		{Name: "also-ok", Run: "echo third"},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 3 {
		t.Errorf("expected 3 results (continue on error), got %d", len(results))
	}
}

func TestRunAll_ExpectedExitCode(t *testing.T) {
	exitCode := 2
	specs := []CommandSpec{
		{Name: "expected-fail", Run: "exit 2", ExpectExit: &exitCode},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != "" {
		t.Errorf("expected no error for expected exit code, got %q", results[0].Error)
	}
}
