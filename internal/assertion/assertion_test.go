package assertion

import (
	"context"
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
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content here"), 0o644)

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
	os.WriteFile(filepath.Join(dir, "main.tf"), []byte("resource \"aws_s3_bucket\" {\n  encryption {\n  }\n}"), 0o644)

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
	os.WriteFile(scriptPath, []byte(script), 0o755)

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
	os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

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
	os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

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
	os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

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
	os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

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
	os.WriteFile(filepath.Join(dir, "beacon"), []byte(script), 0o755)

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
