// Package assertion implements the test assertion framework for drydock.
// It validates environment state after infrastructure is provisioned,
// checking HTTP endpoints, ports, commands, terraform outputs, and files.
package assertion

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/runner"
	"github.com/stormbane-security/drydock/internal/scenario"
)

// Run evaluates all assertions in a scenario against the live environment.
func Run(ctx context.Context, assertions []scenario.Assertion, outputs map[string]string, baseDir string, env map[string]string) []artifact.AssertionResult {
	var results []artifact.AssertionResult
	for _, a := range assertions {
		var result artifact.AssertionResult
		result.Name = a.Name
		result.Type = a.Type

		switch a.Type {
		case "http":
			result = checkHTTP(ctx, a)
		case "port":
			result = checkPort(ctx, a)
		case "command":
			result = checkCommand(ctx, a, baseDir, env)
		case "terraform":
			result = checkTerraformOutput(a, outputs)
		case "file":
			result = checkFile(a, baseDir)
		default:
			result.Message = fmt.Sprintf("unsupported assertion type: %s", a.Type)
		}

		result.Name = a.Name
		result.Type = a.Type
		results = append(results, result)
	}
	return results
}

func checkHTTP(ctx context.Context, a scenario.Assertion) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Target, nil)
	if err != nil {
		result.Message = fmt.Sprintf("invalid URL: %v", err)
		return result
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Message = fmt.Sprintf("HTTP request failed: %v", err)
		return result
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	// Check status code.
	if a.Expect.Status != nil && resp.StatusCode != *a.Expect.Status {
		result.Message = fmt.Sprintf("expected status %d, got %d", *a.Expect.Status, resp.StatusCode)
		return result
	}

	// Check body contains.
	if a.Expect.Body != "" && !strings.Contains(string(body), a.Expect.Body) {
		result.Message = fmt.Sprintf("body does not contain %q", a.Expect.Body)
		return result
	}

	// Check body not contains.
	if a.Expect.NotBody != "" && strings.Contains(string(body), a.Expect.NotBody) {
		result.Message = fmt.Sprintf("body should not contain %q", a.Expect.NotBody)
		return result
	}

	// Check header.
	if a.Expect.Header != "" {
		hv := resp.Header.Get(a.Expect.Header)
		if a.Expect.HeaderValue != "" && hv != a.Expect.HeaderValue {
			result.Message = fmt.Sprintf("expected header %s=%q, got %q", a.Expect.Header, a.Expect.HeaderValue, hv)
			return result
		}
		if hv == "" {
			result.Message = fmt.Sprintf("expected header %s to be present", a.Expect.Header)
			return result
		}
	}

	result.Passed = true
	result.Message = fmt.Sprintf("HTTP %d OK", resp.StatusCode)
	return result
}

func checkPort(ctx context.Context, a scenario.Assertion) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	conn, err := net.DialTimeout("tcp", a.Target, 5*time.Second)
	isOpen := err == nil
	if conn != nil {
		conn.Close()
	}

	expectOpen := true
	if a.Expect.Open != nil {
		expectOpen = *a.Expect.Open
	}

	if isOpen != expectOpen {
		if expectOpen {
			result.Message = fmt.Sprintf("port %s expected open but is closed", a.Target)
		} else {
			result.Message = fmt.Sprintf("port %s expected closed but is open", a.Target)
		}
		return result
	}

	result.Passed = true
	if isOpen {
		result.Message = fmt.Sprintf("port %s is open", a.Target)
	} else {
		result.Message = fmt.Sprintf("port %s is closed", a.Target)
	}
	return result
}

func checkCommand(ctx context.Context, a scenario.Assertion, baseDir string, env map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	if a.Expect.Command == "" {
		result.Message = "expect.command is required for command assertions"
		return result
	}

	r := runner.Run(ctx, a.Name, a.Expect.Command, baseDir, env)

	expectedExit := 0
	if a.Expect.ExitCode != nil {
		expectedExit = *a.Expect.ExitCode
	}
	if r.ExitCode != expectedExit {
		result.Message = fmt.Sprintf("expected exit code %d, got %d\nstdout: %s\nstderr: %s", expectedExit, r.ExitCode, r.Stdout, r.Stderr)
		return result
	}

	if a.Expect.Stdout != "" && !strings.Contains(r.Stdout, a.Expect.Stdout) {
		result.Message = fmt.Sprintf("stdout does not contain %q\nactual: %s", a.Expect.Stdout, r.Stdout)
		return result
	}

	if a.Expect.NotStdout != "" && strings.Contains(r.Stdout, a.Expect.NotStdout) {
		result.Message = fmt.Sprintf("stdout should not contain %q", a.Expect.NotStdout)
		return result
	}

	result.Passed = true
	result.Message = "command passed"
	return result
}

func checkTerraformOutput(a scenario.Assertion, outputs map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	if a.Expect.Output == "" {
		result.Message = "expect.output is required for terraform assertions"
		return result
	}

	val, ok := outputs[a.Expect.Output]
	if !ok {
		result.Message = fmt.Sprintf("terraform output %q not found", a.Expect.Output)
		return result
	}

	if a.Expect.OutputValue != "" && val != a.Expect.OutputValue {
		result.Message = fmt.Sprintf("output %q: expected %q, got %q", a.Expect.Output, a.Expect.OutputValue, val)
		return result
	}

	if a.Expect.OutputMatch != "" {
		re, err := regexp.Compile(a.Expect.OutputMatch)
		if err != nil {
			result.Message = fmt.Sprintf("invalid regex %q: %v", a.Expect.OutputMatch, err)
			return result
		}
		if !re.MatchString(val) {
			result.Message = fmt.Sprintf("output %q value %q does not match pattern %q", a.Expect.Output, val, a.Expect.OutputMatch)
			return result
		}
	}

	result.Passed = true
	result.Message = fmt.Sprintf("output %s = %q", a.Expect.Output, val)
	return result
}

func checkFile(a scenario.Assertion, baseDir string) artifact.AssertionResult {
	result := artifact.AssertionResult{}
	path := a.Target
	if baseDir != "" && !strings.HasPrefix(path, "/") {
		path = baseDir + "/" + path
	}

	_, err := os.Stat(path)
	exists := err == nil

	if a.Expect.Exists != nil {
		if *a.Expect.Exists && !exists {
			result.Message = fmt.Sprintf("file %q expected to exist but does not", a.Target)
			return result
		}
		if !*a.Expect.Exists && exists {
			result.Message = fmt.Sprintf("file %q expected not to exist but does", a.Target)
			return result
		}
	}

	if a.Expect.Contains != "" {
		if !exists {
			result.Message = fmt.Sprintf("file %q does not exist, cannot check contents", a.Target)
			return result
		}
		data, err := os.ReadFile(path)
		if err != nil {
			result.Message = fmt.Sprintf("reading file %q: %v", a.Target, err)
			return result
		}
		if !strings.Contains(string(data), a.Expect.Contains) {
			result.Message = fmt.Sprintf("file %q does not contain %q", a.Target, a.Expect.Contains)
			return result
		}
	}

	result.Passed = true
	result.Message = "file check passed"
	return result
}

// AllPassed returns true if every assertion result passed.
func AllPassed(results []artifact.AssertionResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}
