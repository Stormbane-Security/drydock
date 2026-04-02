// Beacon-specific assertion types.
//
// These assertion types ("beacon" and "classify") invoke an external security
// scanner binary and parse its structured JSON output. The binary defaults to
// "beacon" but can be overridden with the DRYDOCK_SCANNER_BIN env var so
// drydock can test any scanner that speaks the same JSON output format.
package assertion

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/runner"
	"github.com/stormbane-security/drydock/internal/scenario"
)

// scannerBin returns the scanner binary name. Defaults to "beacon" but can
// be overridden with DRYDOCK_SCANNER_BIN for testing alternative scanners.
func scannerBin() string {
	if bin := os.Getenv("DRYDOCK_SCANNER_BIN"); bin != "" {
		return bin
	}
	return "beacon"
}

// scannerBinLinux returns the scanner binary to mount into Linux containers.
// Falls back to DRYDOCK_SCANNER_BIN → "beacon". On macOS, users must set
// DRYDOCK_SCANNER_BIN_LINUX to a cross-compiled Linux binary.
func scannerBinLinux() string {
	if bin := os.Getenv("DRYDOCK_SCANNER_BIN_LINUX"); bin != "" {
		return bin
	}
	return scannerBin()
}

// ── Finding types ─────────────────────────────────────────────────────────

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

// ── Beacon scan assertion ─────────────────────────────────────────────────

// setupBeaconEnv configures environment variables needed when invoking beacon
// as a subprocess. Creates a per-scenario temp database and sets quiet mode.
func setupBeaconEnv(env map[string]string) func() {
	// Separate DB per scenario avoids SQLITE_BUSY in parallel runs.
	if _, ok := env["BEACON_STORE_PATH"]; !ok {
		tmpDB, err := os.CreateTemp("", "drydock-beacon-*.db")
		if err == nil {
			tmpDB.Close()
			env["BEACON_STORE_PATH"] = tmpDB.Name()
			return func() { os.Remove(tmpDB.Name()) }
		}
	}
	return func() {}
}

func checkBeacon(ctx context.Context, a scenario.Assertion, baseDir string, env map[string]string) artifact.AssertionResult {
	result := artifact.AssertionResult{}

	// Target is the scan target (hostname, IP, or URL).
	if a.Target == "" {
		result.Message = "target is required for beacon assertions (scan target)"
		return result
	}

	bin := scannerBin()

	// Suppress informational stderr in automated runs.
	env["BEACON_QUIET"] = "1"

	// Auto-acknowledge --authorized prompts in CI/drydock — the operator
	// controls which scenarios run; the interactive prompt is for ad-hoc CLI use.
	for _, arg := range a.Args {
		if arg == "--authorized" {
			env["BEACON_AUTHORIZED_ACK"] = "1"
			break
		}
	}

	// Build the scanner command arguments (everything after the binary name).
	scanArgs := []string{"scan", "--domain", a.Target, "--format", "json", "--no-enrich", "--no-tui"}
	scanArgs = append(scanArgs, a.Args...)

	var r runner.Result

	if network := env["DRYDOCK_NETWORK"]; network != "" {
		// Run beacon inside a container on the scenario's Docker network.
		// This isolates the scan to only see test infrastructure, not the host.
		// Use the Linux binary since containers are always Linux.
		r = runScannerInNetwork(ctx, scannerBinLinux(), network, scanArgs, baseDir, env)
	} else {
		// No network isolation — run directly on the host.
		argv := append([]string{bin}, scanArgs...)
		r = runner.RunExec(ctx, bin+"-scan", argv, baseDir, env)
	}

	// Surface scanner stderr so debug lines are visible in test output.
	if r.Stderr != "" {
		fmt.Fprintf(os.Stderr, "drydock: %s stderr: %s\n", bin, r.Stderr)
	}
	if r.ExitCode != 0 && r.Stdout == "" {
		errDetail := r.Stderr
		if r.Error != "" {
			errDetail = r.Error + " | " + r.Stderr
		}
		result.Message = fmt.Sprintf("%s scan failed (exit %d): %s", bin, r.ExitCode, errDetail)
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
			result.Message = fmt.Sprintf("failed to parse %s output: %v (wrapper: %v)\nraw output: %.500s", bin, err, err2, r.Stdout)
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
	detail := fmt.Sprintf("%s scan completed: %d findings", bin, len(findings))
	if a.Expect.CheckID != "" {
		detail += fmt.Sprintf(", %s present", a.Expect.CheckID)
	}
	if a.Expect.NotCheckID != "" {
		detail += fmt.Sprintf(", %s absent", a.Expect.NotCheckID)
	}
	result.Message = detail
	return result
}

// runScannerInNetwork runs the scanner binary inside a Docker container
// attached to the scenario's compose network. This ensures the scan only
// sees test infrastructure, not host services.
//
// Image resolution order:
//  1. DRYDOCK_SCANNER_IMAGE env var (custom image, scanner must be pre-installed)
//  2. "beacon-scanner" default image (built from deploy/Dockerfile)
//
// If DRYDOCK_SCANNER_BIN_LINUX is set, the binary is volume-mounted into the
// container instead of using the image's built-in scanner. This is useful for
// local development where you want to test a freshly compiled binary.
func runScannerInNetwork(ctx context.Context, bin, network string, scanArgs []string, baseDir string, env map[string]string) runner.Result {
	image := "beacon-scanner"
	if img := os.Getenv("DRYDOCK_SCANNER_IMAGE"); img != "" {
		image = img
	}

	// Mount a local binary if DRYDOCK_SCANNER_BIN_LINUX is explicitly set.
	mountBinary := os.Getenv("DRYDOCK_SCANNER_BIN_LINUX") != ""

	argv := []string{
		"docker", "run", "--rm",
		"--network", network,
	}

	if mountBinary {
		binAbs, err := filepath.Abs(bin)
		if err != nil {
			return runner.Result{
				Name:     "scanner-network",
				ExitCode: -1,
				Error:    fmt.Sprintf("cannot resolve scanner binary path: %v", err),
			}
		}
		if _, err := os.Stat(binAbs); err != nil {
			return runner.Result{
				Name:     "scanner-network",
				ExitCode: -1,
				Error:    fmt.Sprintf("scanner binary not found at %s: %v", binAbs, err),
			}
		}
		argv = append(argv, "-v", binAbs+":/usr/local/bin/scanner:ro")
	}

	// Forward environment variables the scanner needs.
	forwardVars := []string{
		"BEACON_QUIET",
		"BEACON_AUTHORIZED_ACK",
		"BEACON_STORE_PATH",
		"SHODAN_API_KEY",
		"CENSYS_API_ID",
		"CENSYS_API_SECRET",
		"WHOISXML_API_KEY",
		"VT_API_KEY",
	}
	for _, k := range forwardVars {
		if v, ok := env[k]; ok && v != "" {
			if k == "BEACON_STORE_PATH" {
				argv = append(argv, "-e", k+"=/tmp/beacon.db")
				continue
			}
			argv = append(argv, "-e", k+"="+v)
		}
	}

	if mountBinary {
		// Override the image entrypoint to use the mounted binary.
		argv = append(argv, "--entrypoint", "scanner")
	}

	// Image name, then scan args. The default image has ENTRYPOINT ["beacon"]
	// so args are passed directly. With mountBinary, entrypoint is overridden.
	argv = append(argv, image)
	argv = append(argv, scanArgs...)

	return runner.RunExec(ctx, "scanner-network", argv, baseDir, env)
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

	bin := scannerBin()

	// Run classify with proper argument separation (no shell injection).
	argv := []string{bin, "classify", a.Target, "--format", "json"}
	r := runner.RunExec(ctx, bin+"-classify", argv, baseDir, env)
	if r.ExitCode != 0 && r.Stdout == "" {
		result.Message = fmt.Sprintf("%s classify failed (exit %d): %s", bin, r.ExitCode, r.Stderr)
		return result
	}

	var out classifyOutput
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		result.Message = fmt.Sprintf("failed to parse %s classify output: %v\nraw output: %.500s", bin, err, r.Stdout)
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
