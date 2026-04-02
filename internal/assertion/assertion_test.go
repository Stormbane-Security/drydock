package assertion

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/scenario"
)

func TestCheckHTTP_StatusOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer ts.Close()

	status200 := 200
	a := scenario.Assertion{
		Name:   "http-test",
		Type:   "http",
		Target: ts.URL,
		Expect: scenario.AssertionExpect{
			Status: &status200,
			Body:   "hello",
		},
	}

	result := checkHTTP(context.Background(), a)
	if !result.Passed {
		t.Errorf("expected pass, got fail: %s", result.Message)
	}
}

func TestCheckHTTP_WrongStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	status200 := 200
	a := scenario.Assertion{
		Name:   "http-test",
		Type:   "http",
		Target: ts.URL,
		Expect: scenario.AssertionExpect{
			Status: &status200,
		},
	}

	result := checkHTTP(context.Background(), a)
	if result.Passed {
		t.Error("expected fail for wrong status")
	}
}

func TestCheckHTTP_BodyNotContains(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("error message"))
	}))
	defer ts.Close()

	a := scenario.Assertion{
		Name:   "no-error",
		Type:   "http",
		Target: ts.URL,
		Expect: scenario.AssertionExpect{
			NotBody: "error",
		},
	}

	result := checkHTTP(context.Background(), a)
	if result.Passed {
		t.Error("expected fail when body contains forbidden text")
	}
}

func TestCheckHTTP_Header(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "expected-value")
		w.WriteHeader(200)
	}))
	defer ts.Close()

	a := scenario.Assertion{
		Name:   "header-check",
		Type:   "http",
		Target: ts.URL,
		Expect: scenario.AssertionExpect{
			Header:      "X-Custom",
			HeaderValue: "expected-value",
		},
	}

	result := checkHTTP(context.Background(), a)
	if !result.Passed {
		t.Errorf("expected pass for header check, got: %s", result.Message)
	}
}

func TestCheckCommand_Pass(t *testing.T) {
	a := scenario.Assertion{
		Name: "echo-test",
		Type: "command",
		Expect: scenario.AssertionExpect{
			Command: "echo hello",
			Stdout:  "hello",
		},
	}
	result := checkCommand(context.Background(), a, "", nil)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckCommand_NotStdout(t *testing.T) {
	a := scenario.Assertion{
		Name: "no-error",
		Type: "command",
		Expect: scenario.AssertionExpect{
			Command:   "echo 'ERROR: something broke'",
			NotStdout: "ERROR",
		},
	}
	result := checkCommand(context.Background(), a, "", nil)
	if result.Passed {
		t.Error("expected fail when stdout contains forbidden text")
	}
}

func TestCheckTerraformOutput_Match(t *testing.T) {
	outputs := map[string]string{
		"bucket_name": "my-bucket",
		"region":      "us-central1",
	}

	a := scenario.Assertion{
		Name: "bucket-name",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "bucket_name",
			OutputValue: "my-bucket",
		},
	}
	result := checkTerraformOutput(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckTerraformOutput_Regex(t *testing.T) {
	outputs := map[string]string{
		"endpoint": "https://api.example.com/v2",
	}

	a := scenario.Assertion{
		Name: "endpoint-format",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "endpoint",
			OutputMatch: `^https://.*\.example\.com/v\d+$`,
		},
	}
	result := checkTerraformOutput(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckTerraformOutput_Missing(t *testing.T) {
	outputs := map[string]string{}
	a := scenario.Assertion{
		Name: "missing",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "nonexistent",
			OutputValue: "whatever",
		},
	}
	result := checkTerraformOutput(a, outputs)
	if result.Passed {
		t.Error("expected fail for missing output")
	}
}

func TestCheckFile_Exists(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content here"), 0o644)

	exists := true
	a := scenario.Assertion{
		Name:   "file-exists",
		Type:   "file",
		Target: "test.txt",
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, dir)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckFile_Contains(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "main.tf"), []byte("resource \"aws_s3_bucket\" {\n  encryption {\n  }\n}"), 0o644)

	a := scenario.Assertion{
		Name:   "has-encryption",
		Type:   "file",
		Target: "main.tf",
		Expect: scenario.AssertionExpect{Contains: "encryption"},
	}
	result := checkFile(a, dir)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckFile_NotExists(t *testing.T) {
	dir := t.TempDir()
	exists := true
	a := scenario.Assertion{
		Name:   "missing-file",
		Type:   "file",
		Target: "nope.txt",
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, dir)
	if result.Passed {
		t.Error("expected fail for missing file")
	}
}

// ── Beacon assertion unit tests ──────────────────────────────────────────

// These test the beacon finding parsing and matching logic by mocking
// the beacon binary with a shell script that outputs known JSON.

func TestCheckBeacon_FindingPresent(t *testing.T) {
	dir := t.TempDir()
	// Create a fake "beacon" script that outputs a known finding.
	script := `#!/bin/sh
cat <<'JSONEOF'
[{"check_id":"tls.cert_expiry_7d","severity":"high","title":"Certificate Expires in 7 Days","asset":"api.example.com","evidence":{"expiry_date":"2026-04-07"}}]
JSONEOF
`
	scriptPath := filepath.Join(dir, "beacon")
	_ = os.WriteFile(scriptPath, []byte(script), 0o755)

	a := scenario.Assertion{
		Name:   "cert-expiry-detected",
		Type:   "beacon",
		Target: "api.example.com",
		Expect: scenario.AssertionExpect{
			CheckID:  "tls.cert_expiry_7d",
			Severity: "high",
		},
	}
	// Prepend our temp dir to PATH so "beacon" resolves to our script.
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckBeacon_FindingAbsent(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
cat <<'JSONEOF'
[{"check_id":"headers.missing_hsts","severity":"medium","title":"Missing HSTS","asset":"example.com","evidence":{}}]
JSONEOF
`
	_ = os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

	a := scenario.Assertion{
		Name:   "no-cert-expiry",
		Type:   "beacon",
		Target: "example.com",
		Expect: scenario.AssertionExpect{
			CheckID: "tls.cert_expiry_7d",
		},
	}
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if result.Passed {
		t.Error("expected fail when finding not present")
	}
	if !strings.Contains(result.Message, "not found") {
		t.Errorf("expected 'not found' message, got: %s", result.Message)
	}
}

func TestCheckBeacon_NotCheckID(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo '[]'
`
	_ = os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

	a := scenario.Assertion{
		Name:   "cors-fixed",
		Type:   "beacon",
		Target: "api.example.com",
		Expect: scenario.AssertionExpect{
			NotCheckID: "web.cors_misconfiguration",
		},
	}
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if !result.Passed {
		t.Errorf("expected pass when finding absent, got: %s", result.Message)
	}
}

func TestCheckBeacon_EvidenceMatch(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
cat <<'JSONEOF'
[{"check_id":"supply_chain.vulnerable_dependency","severity":"high","title":"Vulnerable dep","asset":"app.example.com","evidence":{"package":"express","version":"4.17.1","cve_id":"CVE-2024-29041"}}]
JSONEOF
`
	_ = os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

	a := scenario.Assertion{
		Name:   "express-cve",
		Type:   "beacon",
		Target: "app.example.com",
		Expect: scenario.AssertionExpect{
			CheckID:       "supply_chain.vulnerable_dependency",
			EvidenceKey:   "cve_id",
			EvidenceValue: "CVE-2024-29041",
		},
	}
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckBeacon_MinFindings(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
echo '[{"check_id":"a","severity":"low","title":"a","asset":"x","evidence":{}}]'
`
	_ = os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

	min := 3
	a := scenario.Assertion{
		Name:   "enough-findings",
		Type:   "beacon",
		Target: "x",
		Expect: scenario.AssertionExpect{MinFindings: &min},
	}
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if result.Passed {
		t.Error("expected fail when fewer findings than min")
	}
}

func TestCheckBeacon_WrapperFormat(t *testing.T) {
	dir := t.TempDir()
	// Beacon may output findings inside a wrapper object.
	script := `#!/bin/sh
echo '{"findings":[{"check_id":"tls.weak_cipher","severity":"medium","title":"Weak Cipher","asset":"x","evidence":{}}]}'
`
	_ = os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

	a := scenario.Assertion{
		Name:   "wrapper-format",
		Type:   "beacon",
		Target: "x",
		Expect: scenario.AssertionExpect{CheckID: "tls.weak_cipher"},
	}
	result := checkBeacon(context.Background(), a, dir, map[string]string{"PATH": dir + ":" + os.Getenv("PATH")})
	if !result.Passed {
		t.Errorf("expected pass with wrapper format, got: %s", result.Message)
	}
}

func TestAllPassed(t *testing.T) {
	results := []artifact.AssertionResult{
		{Name: "a", Passed: true},
		{Name: "b", Passed: true},
	}
	if !AllPassed(results) {
		t.Error("expected all passed")
	}

	results = append(results, artifact.AssertionResult{Name: "c", Passed: false})
	if AllPassed(results) {
		t.Error("expected not all passed")
	}
}

func TestAllPassed_Empty(t *testing.T) {
	// Vacuous truth: empty slice means all passed.
	if !AllPassed(nil) {
		t.Error("expected all passed for nil slice")
	}
	if !AllPassed([]artifact.AssertionResult{}) {
		t.Error("expected all passed for empty slice")
	}
}

func TestAllPassed_SingleFail(t *testing.T) {
	results := []artifact.AssertionResult{
		{Name: "x", Passed: false, Message: "nope"},
	}
	if AllPassed(results) {
		t.Error("expected not all passed with single failure")
	}
}

// ── checkFile additional tests ────────────────────────────────────────────

func TestCheckFile_ExistsTrue(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "present.txt"), []byte("data"), 0o644)

	exists := true
	a := scenario.Assertion{
		Name:   "exists",
		Type:   "file",
		Target: "present.txt",
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, dir)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckFile_ExistsFalse(t *testing.T) {
	dir := t.TempDir()

	exists := false
	a := scenario.Assertion{
		Name:   "not-exists",
		Type:   "file",
		Target: "gone.txt",
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, dir)
	if !result.Passed {
		t.Errorf("expected pass (file correctly absent), got: %s", result.Message)
	}
}

func TestCheckFile_ExistsFalse_ButFilePresent(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "should-not-be-here.txt"), []byte("x"), 0o644)

	exists := false
	a := scenario.Assertion{
		Name:   "should-not-exist",
		Type:   "file",
		Target: "should-not-be-here.txt",
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, dir)
	if result.Passed {
		t.Error("expected fail when file exists but should not")
	}
}

func TestCheckFile_ContainsMatch(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("database: postgres\nport: 5432"), 0o644)

	a := scenario.Assertion{
		Name:   "has-postgres",
		Type:   "file",
		Target: "config.yaml",
		Expect: scenario.AssertionExpect{Contains: "postgres"},
	}
	result := checkFile(a, dir)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckFile_ContainsNoMatch(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("database: mysql"), 0o644)

	a := scenario.Assertion{
		Name:   "has-postgres",
		Type:   "file",
		Target: "config.yaml",
		Expect: scenario.AssertionExpect{Contains: "postgres"},
	}
	result := checkFile(a, dir)
	if result.Passed {
		t.Error("expected fail when file does not contain expected text")
	}
}

func TestCheckFile_ContainsOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	a := scenario.Assertion{
		Name:   "missing",
		Type:   "file",
		Target: "no-such-file.txt",
		Expect: scenario.AssertionExpect{Contains: "anything"},
	}
	result := checkFile(a, dir)
	if result.Passed {
		t.Error("expected fail for contains check on missing file")
	}
	if !strings.Contains(result.Message, "does not exist") {
		t.Errorf("expected 'does not exist' in message, got: %s", result.Message)
	}
}

func TestCheckFile_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	a := scenario.Assertion{
		Name:   "traversal",
		Type:   "file",
		Target: "../../etc/passwd",
		Expect: scenario.AssertionExpect{Contains: "root"},
	}
	result := checkFile(a, dir)
	if result.Passed {
		t.Error("expected fail for path traversal attempt")
	}
	if !strings.Contains(result.Message, "traverses outside") {
		t.Errorf("expected 'traverses outside' in message, got: %s", result.Message)
	}
}

func TestCheckFile_AbsolutePath(t *testing.T) {
	// When target is an absolute path, baseDir is not prepended.
	tmpFile := filepath.Join(t.TempDir(), "abs-file.txt")
	_ = os.WriteFile(tmpFile, []byte("absolute content"), 0o644)

	exists := true
	a := scenario.Assertion{
		Name:   "abs-path",
		Type:   "file",
		Target: tmpFile,
		Expect: scenario.AssertionExpect{Exists: &exists},
	}
	result := checkFile(a, "/some/other/dir")
	if !result.Passed {
		t.Errorf("expected pass for absolute path, got: %s", result.Message)
	}
}

// ── checkTerraformOutput additional tests ─────────────────────────────────

func TestCheckTerraformOutput_MissingOutput(t *testing.T) {
	a := scenario.Assertion{
		Name: "missing-output",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output: "",
		},
	}
	result := checkTerraformOutput(a, map[string]string{"key": "val"})
	if result.Passed {
		t.Error("expected fail when expect.output is empty")
	}
	if !strings.Contains(result.Message, "expect.output is required") {
		t.Errorf("expected 'expect.output is required', got: %s", result.Message)
	}
}

func TestCheckTerraformOutput_ValueMismatch(t *testing.T) {
	outputs := map[string]string{"region": "us-east-1"}
	a := scenario.Assertion{
		Name: "region-check",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "region",
			OutputValue: "us-west-2",
		},
	}
	result := checkTerraformOutput(a, outputs)
	if result.Passed {
		t.Error("expected fail for value mismatch")
	}
}

func TestCheckTerraformOutput_RegexMismatch(t *testing.T) {
	outputs := map[string]string{"endpoint": "http://plain.example.com"}
	a := scenario.Assertion{
		Name: "must-be-https",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "endpoint",
			OutputMatch: `^https://`,
		},
	}
	result := checkTerraformOutput(a, outputs)
	if result.Passed {
		t.Error("expected fail for regex mismatch")
	}
}

func TestCheckTerraformOutput_InvalidRegex(t *testing.T) {
	outputs := map[string]string{"val": "test"}
	a := scenario.Assertion{
		Name: "bad-regex",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "val",
			OutputMatch: `[invalid`,
		},
	}
	result := checkTerraformOutput(a, outputs)
	if result.Passed {
		t.Error("expected fail for invalid regex")
	}
	if !strings.Contains(result.Message, "invalid regex") {
		t.Errorf("expected 'invalid regex' in message, got: %s", result.Message)
	}
}

func TestCheckTerraformOutput_ValueOnlyNoRegex(t *testing.T) {
	outputs := map[string]string{"name": "my-bucket"}
	a := scenario.Assertion{
		Name: "exact-match",
		Type: "terraform",
		Expect: scenario.AssertionExpect{
			Output:      "name",
			OutputValue: "my-bucket",
		},
	}
	result := checkTerraformOutput(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass for exact value match, got: %s", result.Message)
	}
}

// ── checkPort tests ───────────────────────────────────────────────────────

func TestCheckPort_Open(t *testing.T) {
	// Start a real TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	open := true
	a := scenario.Assertion{
		Name:   "port-open",
		Type:   "port",
		Target: addr,
		Expect: scenario.AssertionExpect{Open: &open},
	}
	result := checkPort(context.Background(), a)
	if !result.Passed {
		t.Errorf("expected pass for open port, got: %s", result.Message)
	}
}

func TestCheckPort_Closed(t *testing.T) {
	// Use a port that is almost certainly not listening.
	// Bind and immediately close to get a known-free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	open := true
	a := scenario.Assertion{
		Name:   "port-closed",
		Type:   "port",
		Target: addr,
		Expect: scenario.AssertionExpect{Open: &open},
	}
	result := checkPort(context.Background(), a)
	if result.Passed {
		t.Error("expected fail for closed port")
	}
}

func TestCheckPort_ExpectClosed(t *testing.T) {
	// Bind and close to get a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	closed := false
	a := scenario.Assertion{
		Name:   "expect-closed",
		Type:   "port",
		Target: addr,
		Expect: scenario.AssertionExpect{Open: &closed},
	}
	result := checkPort(context.Background(), a)
	if !result.Passed {
		t.Errorf("expected pass for correctly closed port, got: %s", result.Message)
	}
}

func TestCheckPort_DefaultExpectOpen(t *testing.T) {
	// When Open is nil, the default expectation is open=true.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	a := scenario.Assertion{
		Name:   "default-open",
		Type:   "port",
		Target: ln.Addr().String(),
		Expect: scenario.AssertionExpect{},
	}
	result := checkPort(context.Background(), a)
	if !result.Passed {
		t.Errorf("expected pass with default open expectation, got: %s", result.Message)
	}
}

// ── Run integration test ──────────────────────────────────────────────────

func TestRun_MultipleAssertionTypes(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello world"), 0o644)

	exists := true
	assertions := []scenario.Assertion{
		{
			Name:   "file-check",
			Type:   "file",
			Target: "test.txt",
			Expect: scenario.AssertionExpect{
				Exists:   &exists,
				Contains: "hello",
			},
		},
		{
			Name: "terraform-check",
			Type: "terraform",
			Expect: scenario.AssertionExpect{
				Output:      "key",
				OutputValue: "value",
			},
		},
	}
	outputs := map[string]string{"key": "value"}

	results := Run(context.Background(), assertions, outputs, dir, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("assertion %q failed: %s", r.Name, r.Message)
		}
	}
	if !AllPassed(results) {
		t.Error("expected AllPassed to return true")
	}
}

func TestRun_SetsNameAndType(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)

	exists := true
	assertions := []scenario.Assertion{
		{
			Name:   "my-assertion",
			Type:   "file",
			Target: "f.txt",
			Expect: scenario.AssertionExpect{Exists: &exists},
		},
	}
	results := Run(context.Background(), assertions, nil, dir, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "my-assertion" {
		t.Errorf("expected name 'my-assertion', got %q", results[0].Name)
	}
	if results[0].Type != "file" {
		t.Errorf("expected type 'file', got %q", results[0].Type)
	}
}

func TestRun_UnsupportedType(t *testing.T) {
	assertions := []scenario.Assertion{
		{Name: "bad", Type: "quantum-entanglement"},
	}
	results := Run(context.Background(), assertions, nil, "", nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Passed {
		t.Error("expected fail for unsupported assertion type")
	}
	if !strings.Contains(results[0].Message, "unsupported") {
		t.Errorf("expected 'unsupported' in message, got: %s", results[0].Message)
	}
}

// ── checkCommand additional tests ─────────────────────────────────────────

func TestCheckCommand_MissingCommand(t *testing.T) {
	a := scenario.Assertion{
		Name: "no-command",
		Type: "command",
		Expect: scenario.AssertionExpect{
			Stdout: "something",
		},
	}
	result := checkCommand(context.Background(), a, "", nil)
	if result.Passed {
		t.Error("expected fail when expect.command is empty")
	}
}

func TestCheckCommand_ExitCode(t *testing.T) {
	exitCode := 42
	a := scenario.Assertion{
		Name: "exit-check",
		Type: "command",
		Expect: scenario.AssertionExpect{
			Command:  "exit 42",
			ExitCode: &exitCode,
		},
	}
	result := checkCommand(context.Background(), a, "", nil)
	if !result.Passed {
		t.Errorf("expected pass for expected exit code, got: %s", result.Message)
	}
}

func TestCheckCommand_ExitCodeMismatch(t *testing.T) {
	exitCode := 0
	a := scenario.Assertion{
		Name: "exit-mismatch",
		Type: "command",
		Expect: scenario.AssertionExpect{
			Command:  "exit 1",
			ExitCode: &exitCode,
		},
	}
	result := checkCommand(context.Background(), a, "", nil)
	if result.Passed {
		t.Error("expected fail for exit code mismatch")
	}
}

// ── GitHub Actions assertion tests ────────────────────────────────────────

func TestCheckGitHubRun_Pass(t *testing.T) {
	outputs := map[string]string{"run.conclusion": "success"}
	a := scenario.Assertion{
		Name: "run-check",
		Type: "github-run",
		Expect: scenario.AssertionExpect{Conclusion: "success"},
	}
	result := checkGitHubRun(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckGitHubRun_Fail(t *testing.T) {
	outputs := map[string]string{"run.conclusion": "failure"}
	a := scenario.Assertion{
		Name: "run-check",
		Type: "github-run",
		Expect: scenario.AssertionExpect{Conclusion: "success"},
	}
	result := checkGitHubRun(a, outputs)
	if result.Passed {
		t.Error("expected fail for conclusion mismatch")
	}
}

func TestCheckGitHubRun_MissingConclusion(t *testing.T) {
	a := scenario.Assertion{
		Name: "run-check",
		Type: "github-run",
		Expect: scenario.AssertionExpect{},
	}
	result := checkGitHubRun(a, map[string]string{})
	if result.Passed {
		t.Error("expected fail when expect.conclusion is empty")
	}
}

func TestCheckGitHubJob_Pass(t *testing.T) {
	outputs := map[string]string{"job.build.conclusion": "success"}
	a := scenario.Assertion{
		Name: "job-check",
		Type: "github-job",
		Expect: scenario.AssertionExpect{Job: "build", Conclusion: "success"},
	}
	result := checkGitHubJob(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckGitHubJob_MissingJob(t *testing.T) {
	a := scenario.Assertion{
		Name: "job-check",
		Type: "github-job",
		Expect: scenario.AssertionExpect{Conclusion: "success"},
	}
	result := checkGitHubJob(a, map[string]string{})
	if result.Passed {
		t.Error("expected fail when expect.job is empty")
	}
}

func TestCheckGitHubStep_Pass(t *testing.T) {
	outputs := map[string]string{"job.build.step.lint.conclusion": "success"}
	a := scenario.Assertion{
		Name: "step-check",
		Type: "github-step",
		Expect: scenario.AssertionExpect{Job: "build", StepName: "lint", Conclusion: "success"},
	}
	result := checkGitHubStep(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckGitHubArtifact_Pass(t *testing.T) {
	outputs := map[string]string{"artifact.coverage-report": "present"}
	a := scenario.Assertion{
		Name: "artifact-check",
		Type: "github-artifact",
		Expect: scenario.AssertionExpect{ArtifactName: "coverage-report"},
	}
	result := checkGitHubArtifact(a, outputs)
	if !result.Passed {
		t.Errorf("expected pass, got: %s", result.Message)
	}
}

func TestCheckGitHubArtifact_Missing(t *testing.T) {
	a := scenario.Assertion{
		Name: "artifact-check",
		Type: "github-artifact",
		Expect: scenario.AssertionExpect{ArtifactName: "missing-artifact"},
	}
	result := checkGitHubArtifact(a, map[string]string{})
	if result.Passed {
		t.Error("expected fail for missing artifact")
	}
}

func TestCheckGitHubArtifact_MissingName(t *testing.T) {
	a := scenario.Assertion{
		Name: "artifact-check",
		Type: "github-artifact",
		Expect: scenario.AssertionExpect{},
	}
	result := checkGitHubArtifact(a, map[string]string{})
	if result.Passed {
		t.Error("expected fail when artifact_name is empty")
	}
}
