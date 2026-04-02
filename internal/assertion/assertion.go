// Package assertion implements the test assertion framework for drydock.
// It validates environment state after infrastructure is provisioned,
// checking HTTP endpoints, ports, commands, terraform outputs, and files.
package assertion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/runner"
	"github.com/stormbane-security/drydock/internal/scenario"
)

// Run evaluates all assertions in a scenario against the live environment.
func Run(ctx context.Context, assertions []scenario.Assertion, outputs map[string]string, baseDir string, env map[string]string) []artifact.AssertionResult {
	// Give each scenario its own beacon database to avoid SQLITE_BUSY when
	// multiple beacon scans run in parallel across different scenarios.
	if env == nil {
		env = make(map[string]string)
	}
	if _, ok := env["BEACON_STORE_PATH"]; !ok {
		tmpDB, err := os.CreateTemp("", "drydock-beacon-*.db")
		if err == nil {
			tmpDB.Close()
			env["BEACON_STORE_PATH"] = tmpDB.Name()
			defer os.Remove(tmpDB.Name())
		}
	}

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
		case "beacon":
			result = checkBeacon(ctx, a, baseDir, env)
		case "classify":
			result = checkClassify(ctx, a, baseDir, env)
		case "github-run":
			result = checkGitHubRun(a, outputs)
		case "github-job":
			result = checkGitHubJob(a, outputs)
		case "github-step":
			result = checkGitHubStep(a, outputs)
		case "github-artifact":
			result = checkGitHubArtifact(a, outputs)
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
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		result.Message = fmt.Sprintf("failed to read response body: %v", err)
		return result
	}

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
		_ = conn.Close()
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
	if baseDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, filepath.Clean(path))
		// Prevent path traversal outside baseDir.
		if !strings.HasPrefix(path, filepath.Clean(baseDir)+string(filepath.Separator)) && path != filepath.Clean(baseDir) {
			result.Message = fmt.Sprintf("path %q traverses outside base directory", a.Target)
			return result
		}
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

// ── Beacon assertion ─────────────────────────────────────────────────────

// beaconFinding matches the JSON output structure of beacon scan --format json.
// Beacon wraps findings in an EnrichedFinding envelope: {"finding": {...}, "explanation": "..."}.
// The actual check_id, severity, etc. are nested inside the "finding" key.
type beaconFinding struct {
	// Nested finding — beacon's enriched output format.
	// Severity may be int (beacon's Finding type: 0=info..4=critical) or string in test fixtures.
	Finding struct {
		CheckID  string         `json:"check_id"`
		Severity any            `json:"severity"`
		Title    string         `json:"title"`
		Asset    string         `json:"asset"`
		Evidence map[string]any `json:"evidence"`
	} `json:"finding"`

	// Top-level fallback fields for raw output formats.
	CheckID  string         `json:"check_id"`
	Severity any            `json:"severity"`
	Title    string         `json:"title"`
	Asset    string         `json:"asset"`
	Evidence map[string]any `json:"evidence"`
}

// resolvedCheckID returns the check_id from whichever level it was parsed at.
func (f beaconFinding) resolvedCheckID() string {
	if f.Finding.CheckID != "" {
		return f.Finding.CheckID
	}
	return f.CheckID
}

// parseSeverity converts a severity value (int or string) to a canonical string label.
func parseSeverity(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case float64: // JSON numbers unmarshal to float64 via any
		switch int(s) {
		case 4:
			return "critical"
		case 3:
			return "high"
		case 2:
			return "medium"
		case 1:
			return "low"
		default:
			return "info"
		}
	default:
		return "info"
	}
}

// resolvedSeverity returns severity as a string label from the nested or top-level field.
func (f beaconFinding) resolvedSeverity() string {
	if f.Finding.CheckID != "" {
		return parseSeverity(f.Finding.Severity)
	}
	return parseSeverity(f.Severity)
}

// resolvedEvidence returns evidence from the nested or top-level field.
func (f beaconFinding) resolvedEvidence() map[string]any {
	if f.Finding.Evidence != nil {
		return f.Finding.Evidence
	}
	return f.Evidence
}

// resolvedTitle returns the title from the nested or top-level field.
func (f beaconFinding) resolvedTitle() string {
	if f.Finding.Title != "" {
		return f.Finding.Title
	}
	return f.Title
}

func checkBeacon(ctx context.Context, a scenario.Assertion, baseDir string, env map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	// Target is the scan target (hostname, IP, or URL).
	if a.Target == "" {
		result.Message = "target is required for beacon assertions (scan target)"
		return result
	}

	// Run beacon scan with proper argument separation (no shell injection).
	argv := []string{"beacon", "scan", "--domain", a.Target, "--format", "json", "--no-enrich", "--no-tui"}
	argv = append(argv, a.Args...)
	// Auto-acknowledge --authorized prompts in CI/drydock — the operator
	// controls which scenarios run; the interactive prompt is for ad-hoc CLI use.
	for _, arg := range a.Args {
		if arg == "--authorized" {
			env["BEACON_AUTHORIZED_ACK"] = "1"
			break
		}
	}
	r := runner.RunExec(ctx, "beacon-scan", argv, baseDir, env)
	// Surface beacon stderr so port-restriction debug lines are visible.
	if r.Stderr != "" {
		fmt.Fprintf(os.Stderr, "drydock: beacon stderr: %s\n", r.Stderr)
	}
	if r.ExitCode != 0 && r.Stdout == "" {
		errDetail := r.Stderr
		if r.Error != "" {
			errDetail = r.Error + " | " + r.Stderr
		}
		result.Message = fmt.Sprintf("beacon scan failed (exit %d): %s", r.ExitCode, errDetail)
		return result
	}

	// Parse findings from JSON output.
	var findings []beaconFinding
	if err := json.Unmarshal([]byte(r.Stdout), &findings); err != nil {
		// Try parsing as a wrapper object with a "findings" key.
		var wrapper struct {
			Findings []beaconFinding `json:"findings"`
		}
		if err2 := json.Unmarshal([]byte(r.Stdout), &wrapper); err2 != nil {
			result.Message = fmt.Sprintf("failed to parse beacon output: %v (wrapper: %v)\nraw output: %.500s", err, err2, r.Stdout)
			return result
		}
		findings = wrapper.Findings
	}

	// Check min/max finding counts.
	if a.Expect.MinFindings != nil && len(findings) < *a.Expect.MinFindings {
		result.Message = fmt.Sprintf("expected at least %d findings, got %d", *a.Expect.MinFindings, len(findings))
		return result
	}
	if a.Expect.MaxFindings != nil && len(findings) > *a.Expect.MaxFindings {
		result.Message = fmt.Sprintf("expected at most %d findings, got %d", *a.Expect.MaxFindings, len(findings))
		return result
	}

	// Check that a specific check_id is present.
	if a.Expect.CheckID != "" {
		found := false
		for _, f := range findings {
			if f.resolvedCheckID() != a.Expect.CheckID {
				continue
			}
			// If severity is also specified, verify it matches.
			if a.Expect.Severity != "" && f.resolvedSeverity() != a.Expect.Severity {
				continue
			}
			// If evidence key/value is specified, find a finding that matches.
			if a.Expect.EvidenceKey != "" {
				ev, ok := f.resolvedEvidence()[a.Expect.EvidenceKey]
				if !ok {
					continue
				}
				if a.Expect.EvidenceValue != "" {
					evStr := fmt.Sprintf("%v", ev)
					if evStr != a.Expect.EvidenceValue {
						continue
					}
				}
			}
			found = true
			break
		}
		if !found {
			var foundIDs []string
			for _, f := range findings {
				id := f.resolvedCheckID()
				if a.Expect.EvidenceKey != "" {
					if ev, ok := f.resolvedEvidence()[a.Expect.EvidenceKey]; ok {
						id += fmt.Sprintf("(%s=%v)", a.Expect.EvidenceKey, ev)
					}
				}
				foundIDs = append(foundIDs, id)
			}
			expected := a.Expect.CheckID
			if a.Expect.EvidenceValue != "" {
				expected += fmt.Sprintf(" with %s=%q", a.Expect.EvidenceKey, a.Expect.EvidenceValue)
			}
			result.Message = fmt.Sprintf("expected finding %s not found; got %d findings: [%s]", expected, len(findings), strings.Join(foundIDs, ", "))
			return result
		}
	}

	// Check that a specific check_id is NOT present.
	if a.Expect.NotCheckID != "" {
		for _, f := range findings {
			if f.resolvedCheckID() == a.Expect.NotCheckID {
				result.Message = fmt.Sprintf("finding %s should not be present but was found: %s", a.Expect.NotCheckID, f.resolvedTitle())
				return result
			}
		}
	}

	result.Passed = true
	detail := fmt.Sprintf("beacon scan completed: %d findings", len(findings))
	if a.Expect.CheckID != "" {
		detail += fmt.Sprintf(", %s present", a.Expect.CheckID)
	}
	if a.Expect.NotCheckID != "" {
		detail += fmt.Sprintf(", %s absent", a.Expect.NotCheckID)
	}
	result.Message = detail
	return result
}

// ── Classify assertion ────────────────────────────────────────────────────

// classifyOutput matches the JSON output structure of beacon classify --format json.
type classifyOutput struct {
	ProxyType        string            `json:"proxy_type"`
	Framework        string            `json:"framework"`
	CloudProvider    string            `json:"cloud_provider"`
	AuthSystem       string            `json:"auth_system"`
	InfraLayer       string            `json:"infra_layer"`
	IsKubernetes     bool              `json:"is_kubernetes"`
	IsServerless     bool              `json:"is_serverless"`
	IsReverseProxy   bool              `json:"is_reverse_proxy"`
	HasDMARC         bool              `json:"has_dmarc"`
	BackendServices  []string          `json:"backend_services"`
	ServiceVersions  map[string]string `json:"service_versions"`
	CookieNames      []string          `json:"cookie_names"`
	RespondingPaths  []string          `json:"responding_paths"`
	MatchedPlaybooks []string          `json:"matched_playbooks"`
	StatusCode       int               `json:"status_code"`
	Title            string            `json:"title"`
}

func checkClassify(ctx context.Context, a scenario.Assertion, baseDir string, env map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	if a.Target == "" {
		result.Message = "target is required for classify assertions"
		return result
	}

	// Run beacon classify with proper argument separation (no shell injection).
	argv := []string{"beacon", "classify", a.Target, "--format", "json"}
	r := runner.RunExec(ctx, "beacon-classify", argv, baseDir, env)
	if r.ExitCode != 0 && r.Stdout == "" {
		result.Message = fmt.Sprintf("beacon classify failed (exit %d): %s", r.ExitCode, r.Stderr)
		return result
	}

	var out classifyOutput
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		result.Message = fmt.Sprintf("failed to parse beacon classify output: %v\nraw output: %.500s", err, r.Stdout)
		return result
	}

	// Each expect field is checked independently; all must pass.
	type check struct {
		field   string
		ok      bool
		message string
	}
	var checks []check

	if a.Expect.ProxyType != "" {
		checks = append(checks, check{
			field:   "proxy_type",
			ok:      out.ProxyType == a.Expect.ProxyType,
			message: fmt.Sprintf("proxy_type: expected %q, got %q", a.Expect.ProxyType, out.ProxyType),
		})
	}
	if a.Expect.FrameworkField != "" {
		checks = append(checks, check{
			field:   "framework",
			ok:      out.Framework == a.Expect.FrameworkField,
			message: fmt.Sprintf("framework: expected %q, got %q", a.Expect.FrameworkField, out.Framework),
		})
	}
	if a.Expect.CloudProviderField != "" {
		checks = append(checks, check{
			field:   "cloud_provider",
			ok:      out.CloudProvider == a.Expect.CloudProviderField,
			message: fmt.Sprintf("cloud_provider: expected %q, got %q", a.Expect.CloudProviderField, out.CloudProvider),
		})
	}
	if a.Expect.AuthSystemField != "" {
		checks = append(checks, check{
			field:   "auth_system",
			ok:      out.AuthSystem == a.Expect.AuthSystemField,
			message: fmt.Sprintf("auth_system: expected %q, got %q", a.Expect.AuthSystemField, out.AuthSystem),
		})
	}
	if a.Expect.InfraLayerField != "" {
		checks = append(checks, check{
			field:   "infra_layer",
			ok:      out.InfraLayer == a.Expect.InfraLayerField,
			message: fmt.Sprintf("infra_layer: expected %q, got %q", a.Expect.InfraLayerField, out.InfraLayer),
		})
	}
	if a.Expect.IsKubernetes != nil {
		checks = append(checks, check{
			field:   "is_kubernetes",
			ok:      out.IsKubernetes == *a.Expect.IsKubernetes,
			message: fmt.Sprintf("is_kubernetes: expected %v, got %v", *a.Expect.IsKubernetes, out.IsKubernetes),
		})
	}
	if a.Expect.IsServerless != nil {
		checks = append(checks, check{
			field:   "is_serverless",
			ok:      out.IsServerless == *a.Expect.IsServerless,
			message: fmt.Sprintf("is_serverless: expected %v, got %v", *a.Expect.IsServerless, out.IsServerless),
		})
	}
	if a.Expect.IsReverseProxy != nil {
		checks = append(checks, check{
			field:   "is_reverse_proxy",
			ok:      out.IsReverseProxy == *a.Expect.IsReverseProxy,
			message: fmt.Sprintf("is_reverse_proxy: expected %v, got %v", *a.Expect.IsReverseProxy, out.IsReverseProxy),
		})
	}
	if a.Expect.HasDMARC != nil {
		checks = append(checks, check{
			field:   "has_dmarc",
			ok:      out.HasDMARC == *a.Expect.HasDMARC,
			message: fmt.Sprintf("has_dmarc: expected %v, got %v", *a.Expect.HasDMARC, out.HasDMARC),
		})
	}
	if a.Expect.BackendService != "" {
		found := false
		for _, svc := range out.BackendServices {
			if svc == a.Expect.BackendService {
				found = true
				break
			}
		}
		checks = append(checks, check{
			field:   "backend_service",
			ok:      found,
			message: fmt.Sprintf("backend_service: %q not found in %v", a.Expect.BackendService, out.BackendServices),
		})
	}
	if a.Expect.ServiceVersion != "" {
		_, exists := out.ServiceVersions[a.Expect.ServiceVersion]
		checks = append(checks, check{
			field:   "service_version",
			ok:      exists,
			message: fmt.Sprintf("service_version: key %q not found in service_versions", a.Expect.ServiceVersion),
		})
	}
	if a.Expect.CookieName != "" {
		found := false
		for _, c := range out.CookieNames {
			if c == a.Expect.CookieName {
				found = true
				break
			}
		}
		checks = append(checks, check{
			field:   "cookie_name",
			ok:      found,
			message: fmt.Sprintf("cookie_name: %q not found in %v", a.Expect.CookieName, out.CookieNames),
		})
	}
	if a.Expect.PathResponds != "" {
		found := false
		for _, p := range out.RespondingPaths {
			if p == a.Expect.PathResponds {
				found = true
				break
			}
		}
		checks = append(checks, check{
			field:   "path_responds",
			ok:      found,
			message: fmt.Sprintf("path_responds: %q not found in %v", a.Expect.PathResponds, out.RespondingPaths),
		})
	}
	if a.Expect.MatchedPlaybook != "" {
		found := false
		for _, pb := range out.MatchedPlaybooks {
			if pb == a.Expect.MatchedPlaybook {
				found = true
				break
			}
		}
		checks = append(checks, check{
			field:   "matched_playbook",
			ok:      found,
			message: fmt.Sprintf("matched_playbook: %q not found in %v", a.Expect.MatchedPlaybook, out.MatchedPlaybooks),
		})
	}
	if a.Expect.StatusCodeField != nil {
		checks = append(checks, check{
			field:   "status_code",
			ok:      out.StatusCode == *a.Expect.StatusCodeField,
			message: fmt.Sprintf("status_code: expected %d, got %d", *a.Expect.StatusCodeField, out.StatusCode),
		})
	}
	if a.Expect.TitleContains != "" {
		checks = append(checks, check{
			field:   "title_contains",
			ok:      strings.Contains(out.Title, a.Expect.TitleContains),
			message: fmt.Sprintf("title_contains: %q not found in title %q", a.Expect.TitleContains, out.Title),
		})
	}
	if a.Expect.NotProxyType != "" {
		checks = append(checks, check{
			field:   "not_proxy_type",
			ok:      out.ProxyType != a.Expect.NotProxyType,
			message: fmt.Sprintf("not_proxy_type: proxy_type must not be %q but is", a.Expect.NotProxyType),
		})
	}
	if a.Expect.NotFramework != "" {
		checks = append(checks, check{
			field:   "not_framework",
			ok:      out.Framework != a.Expect.NotFramework,
			message: fmt.Sprintf("not_framework: framework must not be %q but is", a.Expect.NotFramework),
		})
	}

	// All checks must pass.
	for _, c := range checks {
		if !c.ok {
			result.Message = c.message
			return result
		}
	}

	result.Passed = true
	result.Message = fmt.Sprintf("classify check passed (%d fields verified)", len(checks))
	return result
}

// ── GitHub Actions assertions ─────────────────────────────────────────────

func checkGitHubRun(a scenario.Assertion, outputs map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}
	if a.Expect.Conclusion == "" {
		result.Message = "expect.conclusion is required for github-run assertions"
		return result
	}

	actual, ok := outputs["run.conclusion"]
	if !ok {
		result.Message = "run.conclusion not found in outputs (workflow may not have completed)"
		return result
	}

	if actual != a.Expect.Conclusion {
		result.Message = fmt.Sprintf("run conclusion: expected %q, got %q", a.Expect.Conclusion, actual)
		return result
	}

	result.Passed = true
	result.Message = fmt.Sprintf("run conclusion = %s", actual)
	return result
}

func checkGitHubJob(a scenario.Assertion, outputs map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}
	if a.Expect.Job == "" {
		result.Message = "expect.job is required for github-job assertions"
		return result
	}
	if a.Expect.Conclusion == "" {
		result.Message = "expect.conclusion is required for github-job assertions"
		return result
	}

	key := "job." + a.Expect.Job + ".conclusion"
	actual, ok := outputs[key]
	if !ok {
		result.Message = fmt.Sprintf("job %q not found in outputs", a.Expect.Job)
		return result
	}

	if actual != a.Expect.Conclusion {
		result.Message = fmt.Sprintf("job %s conclusion: expected %q, got %q", a.Expect.Job, a.Expect.Conclusion, actual)
		return result
	}

	result.Passed = true
	result.Message = fmt.Sprintf("job %s conclusion = %s", a.Expect.Job, actual)
	return result
}

func checkGitHubStep(a scenario.Assertion, outputs map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}
	if a.Expect.Job == "" {
		result.Message = "expect.job is required for github-step assertions"
		return result
	}
	if a.Expect.StepName == "" {
		result.Message = "expect.step_name is required for github-step assertions"
		return result
	}
	if a.Expect.Conclusion == "" {
		result.Message = "expect.conclusion is required for github-step assertions"
		return result
	}

	key := "job." + a.Expect.Job + ".step." + a.Expect.StepName + ".conclusion"
	actual, ok := outputs[key]
	if !ok {
		result.Message = fmt.Sprintf("step %s in job %s not found in outputs", a.Expect.StepName, a.Expect.Job)
		return result
	}

	if actual != a.Expect.Conclusion {
		result.Message = fmt.Sprintf("step %s conclusion: expected %q, got %q", a.Expect.StepName, a.Expect.Conclusion, actual)
		return result
	}

	result.Passed = true
	result.Message = fmt.Sprintf("step %s conclusion = %s", a.Expect.StepName, actual)
	return result
}

func checkGitHubArtifact(a scenario.Assertion, outputs map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}
	if a.Expect.ArtifactName == "" {
		result.Message = "expect.artifact_name is required for github-artifact assertions"
		return result
	}

	key := "artifact." + a.Expect.ArtifactName
	_, ok := outputs[key]
	if !ok {
		result.Message = fmt.Sprintf("artifact %q not found", a.Expect.ArtifactName)
		return result
	}

	result.Passed = true
	result.Message = fmt.Sprintf("artifact %s present", a.Expect.ArtifactName)
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
