package runner

import (
	"context"
	"os"
	"strings"
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

// ── RunExec tests ─────────────────────────────────────────────────────────

func TestRunExec_ValidCommand(t *testing.T) {
	result := RunExec(context.Background(), "echo", []string{"echo", "hello"}, "", nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}
	if result.Error != "" {
		t.Errorf("expected no error, got %q", result.Error)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestRunExec_FailingCommand(t *testing.T) {
	result := RunExec(context.Background(), "false", []string{"false"}, "", nil)
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for failing command")
	}
}

func TestRunExec_EmptyArgv(t *testing.T) {
	result := RunExec(context.Background(), "empty", nil, "", nil)
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1, got %d", result.ExitCode)
	}
	if result.Error != "empty command" {
		t.Errorf("expected 'empty command' error, got %q", result.Error)
	}
}

func TestRunExec_EmptySlice(t *testing.T) {
	result := RunExec(context.Background(), "empty", []string{}, "", nil)
	if result.ExitCode != -1 {
		t.Errorf("expected exit code -1, got %d", result.ExitCode)
	}
	if result.Error != "empty command" {
		t.Errorf("expected 'empty command' error, got %q", result.Error)
	}
}

func TestRunExec_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := RunExec(ctx, "sleep", []string{"sleep", "60"}, "", nil)
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code for cancelled context")
	}
}

func TestRunExec_WithEnv(t *testing.T) {
	result := RunExec(context.Background(), "printenv", []string{"printenv", "DRYDOCK_TEST_VAR"}, "", map[string]string{
		"DRYDOCK_TEST_VAR": "test_value_42",
	})
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "test_value_42" {
		t.Errorf("expected stdout 'test_value_42', got %q", result.Stdout)
	}
}

func TestRunExec_WithWorkingDir(t *testing.T) {
	dir := t.TempDir()
	result := RunExec(context.Background(), "pwd", []string{"pwd"}, dir, nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	// On macOS, /var/folders is symlinked from /private/var/folders, so resolve.
	got := strings.TrimSpace(result.Stdout)
	resolvedDir, _ := os.Getwd()
	_ = resolvedDir
	// Just check the dir name suffix is present, since temp dirs may have symlinks.
	if !strings.HasSuffix(got, dir[strings.LastIndex(dir, "/"):]) && got != dir {
		// More robust: resolve both to real paths and compare.
		realGot, err1 := realPath(got)
		realDir, err2 := realPath(dir)
		if err1 != nil || err2 != nil || realGot != realDir {
			t.Errorf("expected working dir %q, got %q", dir, got)
		}
	}
}

func realPath(p string) (string, error) {
	return os.Readlink(p) // fall back handled by caller
}

func TestRunExec_MultipleArgs(t *testing.T) {
	result := RunExec(context.Background(), "printf", []string{"printf", "%s %s", "hello", "world"}, "", nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Stdout)
	}
}

func TestRunExec_Stderr(t *testing.T) {
	result := RunExec(context.Background(), "stderr", []string{"sh", "-c", "echo errmsg >&2"}, "", nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stderr) != "errmsg" {
		t.Errorf("expected stderr 'errmsg', got %q", result.Stderr)
	}
}

func TestRunExec_CommandSetsName(t *testing.T) {
	result := RunExec(context.Background(), "my-name", []string{"echo"}, "", nil)
	if result.Name != "my-name" {
		t.Errorf("expected name 'my-name', got %q", result.Name)
	}
}

// ── Additional Run (shell mode) tests ─────────────────────────────────────

func TestRun_WithWorkingDir(t *testing.T) {
	dir := t.TempDir()
	// Create a file in the temp dir to prove we're in it.
	if err := os.WriteFile(dir+"/marker.txt", []byte("found"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := Run(context.Background(), "cat", "cat marker.txt", dir, nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if result.Stdout != "found" {
		t.Errorf("expected 'found', got %q", result.Stdout)
	}
}

func TestRun_CapturesStderr(t *testing.T) {
	result := Run(context.Background(), "stderr", "echo oops >&2", "", nil)
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if strings.TrimSpace(result.Stderr) != "oops" {
		t.Errorf("expected stderr 'oops', got %q", result.Stderr)
	}
}

// ── Additional RunAll tests ───────────────────────────────────────────────

func TestRunAll_ExpectExitNonZero_Mismatch(t *testing.T) {
	// Command exits 0 but we expect exit 3 -- should be treated as failure.
	exitCode := 3
	specs := []CommandSpec{
		{Name: "wrong-exit", Run: "echo ok", ExpectExit: &exitCode},
		{Name: "unreachable", Run: "echo never"},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (stop on mismatch), got %d", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error when exit code does not match expected")
	}
}

func TestRunAll_PerCommandEnv(t *testing.T) {
	specs := []CommandSpec{
		{
			Name: "env-check",
			Run:  "echo $BASE_VAR-$CMD_VAR",
			Env:  map[string]string{"CMD_VAR": "cmd"},
		},
	}
	baseEnv := map[string]string{"BASE_VAR": "base"}
	results := RunAll(context.Background(), specs, "", baseEnv)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if strings.TrimSpace(results[0].Stdout) != "base-cmd" {
		t.Errorf("expected 'base-cmd', got %q", results[0].Stdout)
	}
}

func TestRunAll_PerCommandDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/proof.txt", []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	specs := []CommandSpec{
		{Name: "in-dir", Run: "cat proof.txt", Dir: dir},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != 0 {
		t.Errorf("expected exit 0, got %d; stderr: %s", results[0].ExitCode, results[0].Stderr)
	}
	if strings.TrimSpace(results[0].Stdout) != "here" {
		t.Errorf("expected 'here', got %q", results[0].Stdout)
	}
}

func TestRunAll_AllSucceed(t *testing.T) {
	specs := []CommandSpec{
		{Name: "a", Run: "echo a"},
		{Name: "b", Run: "echo b"},
		{Name: "c", Run: "echo c"},
	}
	results := RunAll(context.Background(), specs, "", nil)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.ExitCode != 0 {
			t.Errorf("command %q failed with exit %d", r.Name, r.ExitCode)
		}
	}
}
