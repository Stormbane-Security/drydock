package assertion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/scenario"
)

func TestCheckHTTP_StatusOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.WriteHeader(200)
		w.Write([]byte("hello world"))
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
		w.Write([]byte("error message"))
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
