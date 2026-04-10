// drydock is a sandbox infrastructure builder and test runner.
// It spins up disposable environments, runs tests against them,
// validates assertions, collects artifacts, and tears everything down.
//
// Usage:
//
//	drydock validate <scenario-path>     Validate scenario YAML
//	drydock run <scenario-path>          Run a scenario end-to-end
//	drydock run --tags <tag> <dir>       Run scenarios matching tags
//	drydock debug <scenario-path>        Start infra and wait for manual testing
//	drydock destroy <scenario-path>      Force-destroy a scenario's environment
//	drydock inspect <run-id>             Show results of a previous run
//	drydock list                         List all previous runs
//	drydock bootstrap [provider]         Interactive setup for test infrastructure
package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stormbane-security/drydock/internal/artifact"
	"github.com/stormbane-security/drydock/internal/engine"
	"github.com/stormbane-security/drydock/internal/scenario"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "validate":
		cmdValidate(args)
	case "run":
		cmdRun(args)
	case "pull":
		cmdPull(args)
	case "debug":
		cmdDebug(args)
	case "destroy":
		cmdDestroy(args)
	case "inspect":
		cmdInspect(args)
	case "list":
		cmdList()
	case "bootstrap":
		cmdBootstrap(args)
	case "version":
		fmt.Printf("drydock v%s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `drydock — sandbox infrastructure builder and test runner

Usage:
  drydock validate <path>              Validate a scenario file or directory
  drydock run [flags] <path>           Run a scenario or all scenarios in a directory
  drydock pull <path>                  Pre-pull all Docker images referenced in scenarios
  drydock debug <path>                 Start infrastructure and wait for manual testing
  drydock destroy <path>               Tear down a scenario's environment
  drydock inspect <run-id>             Show results of a previous run
  drydock list                         List all previous runs
  drydock bootstrap [gcp|aws|all]      Interactive setup for test infrastructure
  drydock version                      Print version

Run flags:
  --tags <tag1,tag2>        Only run scenarios with matching tags
  --exclude-tags <t1,t2>    Skip scenarios with matching tags
  --matrix <k=v,...>        Filter matrix variants (e.g. database=postgres)
  --parallel <N>            Run N scenarios concurrently (default: 1, sequential)
  --retry <N>               Retry failed scenarios up to N times (default: 0)
  --artifacts <dir>         Artifact output directory (default: .drydock/runs)
  --json                    Output results as JSON
  --ci                      CI mode: plain-text output, writes JUnit XML to artifacts dir
  --skip-teardown           Leave backends and fixtures running after the run (or set DRYDOCK_SKIP_TEARDOWN=1)
  --keep                    Alias for --skip-teardown
  --beacon-args <args>      Extra args injected into every beacon assertion (e.g. '--log-level debug')`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "drydock: "+format+"\n", args...)
	os.Exit(1)
}

func cmdValidate(args []string) {
	if len(args) == 0 {
		fatalf("usage: drydock validate <scenario-path>")
	}

	scenarios := loadScenarios(args[0])
	for _, s := range scenarios {
		scenario.ExpandEnvBeforeValidate(s)
		if err := s.Validate(); err != nil {
			fatalf("validation failed for %q: %v", s.Name, err)
		}
		fmt.Fprintf(os.Stderr, "✓ %s\n", s.Name)
	}
	fmt.Fprintf(os.Stderr, "drydock: %d scenario(s) validated\n", len(scenarios))
}

func requireDocker() {
	if _, err := exec.LookPath("docker"); err != nil {
		fatalf("docker not found in PATH — install Docker to run scenarios")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		fatalf("docker daemon is not running — start Docker Desktop and try again")
	}
}

// scenarioResult holds the outcome of running a single scenario, including retry info.
type scenarioResult struct {
	scenario *scenario.Scenario
	record   *artifact.RunRecord
	err      error // non-nil when eng.Run itself errors (no record)
	retries  int   // number of retries it took (0 = first attempt succeeded)
}

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	tags := fs.String("tags", "", "comma-separated tags to filter scenarios")
	excludeTags := fs.String("exclude-tags", "", "comma-separated tags to exclude scenarios")
	matrixFilter := fs.String("matrix", "", "filter matrix variants (e.g. database=postgres,cache=redis)")
	parallel := fs.Int("parallel", 1, "number of scenarios to run concurrently")
	retry := fs.Int("retry", 0, "retry failed scenarios up to N times")
	artifactDir := fs.String("artifacts", ".drydock/runs", "artifact output directory")
	jsonOutput := fs.Bool("json", false, "output results as JSON")
	fixedPorts := fs.Bool("fixed-ports", false, "use fixed host ports from YAML instead of random ephemeral ports")
	ciMode := fs.Bool("ci", false, "CI mode: plain-text output, writes JUnit XML to artifacts dir")
	skipTeardown := fs.Bool("skip-teardown", false, "do not destroy backends/fixtures after the run")
	keep := fs.Bool("keep", false, "alias for --skip-teardown")
	beaconArgs := fs.String("beacon-args", "", "extra args to inject into every beacon assertion (e.g. '--log-level debug')")

	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fatalf("usage: drydock run [--tags <tags>] [--matrix <key=val,...>] <scenario-path> [<path>...]")
	}

	// Support multiple positional arguments: each can be a file or directory.
	var scenarios []*scenario.Scenario
	for i := 0; i < fs.NArg(); i++ {
		scenarios = append(scenarios, loadScenarios(fs.Arg(i))...)
	}

	// Filter by tags (include).
	if *tags != "" {
		filterTags := strings.Split(*tags, ",")
		var filtered []*scenario.Scenario
		for _, s := range scenarios {
			if matchesTags(s.Tags, filterTags) {
				filtered = append(filtered, s)
			}
		}
		scenarios = filtered
	}

	// Filter by tags (exclude).
	if *excludeTags != "" {
		excTags := strings.Split(*excludeTags, ",")
		var filtered []*scenario.Scenario
		for _, s := range scenarios {
			if !matchesTags(s.Tags, excTags) {
				filtered = append(filtered, s)
			}
		}
		scenarios = filtered
	}

	// Filter by matrix values.
	if *matrixFilter != "" {
		filters := parseMatrixFilter(*matrixFilter)
		var filtered []*scenario.Scenario
		for _, s := range scenarios {
			if matchesMatrixFilter(s.Name, filters) {
				filtered = append(filtered, s)
			}
		}
		scenarios = filtered
	}

	if len(scenarios) == 0 {
		fatalf("no matching scenarios found")
	}

	if *skipTeardown || *keep {
		for _, s := range scenarios {
			s.SkipTeardown = true
		}
	}

	needsDocker := false
	for _, s := range scenarios {
		scenario.ExpandEnvBeforeValidate(s)
		if s.NeedsDocker() {
			needsDocker = true
		}
	}
	if needsDocker {
		requireDocker()
	}

	// Sort by weight descending so heavy/slow tests start first.
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Weight > scenarios[j].Weight
	})

	// Apply --fixed-ports CLI override to all scenarios.
	if *fixedPorts {
		for _, s := range scenarios {
			s.FixedPorts = true
		}
	}

	// Inject --beacon-args into every scenario's env for the assertion runner.
	if *beaconArgs != "" {
		for _, s := range scenarios {
			if s.Env == nil {
				s.Env = make(map[string]string)
			}
			s.Env["DRYDOCK_BEACON_EXTRA_ARGS"] = *beaconArgs
		}
	}

	// Clamp parallel to valid range.
	if *parallel < 1 {
		*parallel = 1
	}

	// Set up signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\ndrydock: interrupted, tearing down...")
		cancel()
	}()

	eng := engine.New(*artifactDir)

	// Validate all scenarios first.
	for _, s := range scenarios {
		if err := s.Validate(); err != nil {
			fatalf("validation failed for %q: %v", s.Name, err)
		}
	}

	// runOneScenario executes a single scenario with retry logic.
	runOne := func(s *scenario.Scenario) scenarioResult {
		maxAttempts := 1 + *retry
		for attempt := 0; attempt < maxAttempts; attempt++ {
			record, err := eng.Run(ctx, s)
			if err != nil {
				// Engine-level error (no record). Retry if attempts remain.
				if attempt < maxAttempts-1 {
					continue
				}
				return scenarioResult{scenario: s, err: err, retries: attempt}
			}
			if record.Status == "pass" || attempt >= maxAttempts-1 {
				return scenarioResult{scenario: s, record: record, retries: attempt}
			}
			// Status is fail or error — retry.
		}
		// Should not reach here, but be safe.
		return scenarioResult{scenario: s, err: fmt.Errorf("exhausted retries")}
	}

	// Run scenarios (parallel or sequential).
	results := runScenarios(ctx, scenarios, *parallel, runOne)

	// Report results.
	var passed, failed, errored int
	var records []*artifact.RunRecord
	for _, r := range results {
		if r.err != nil {
			errored++
			if *ciMode {
				fmt.Fprintf(os.Stderr, "--- ERROR %s: %v\n", r.scenario.Name, r.err)
			} else {
				fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", r.scenario.Name, r.err)
			}
			continue
		}
		records = append(records, r.record)

		if *jsonOutput {
			data, _ := json.MarshalIndent(r.record, "", "  ")
			fmt.Println(string(data))
		}

		retryNote := ""
		if r.retries > 0 {
			retryNote = fmt.Sprintf(" (retry %d)", r.retries)
		}

		switch r.record.Status {
		case "pass":
			passed++
			if *ciMode {
				fmt.Fprintf(os.Stderr, "--- PASS %s%s (%.1fs)\n", r.scenario.Name, retryNote, r.record.Duration.Seconds())
			} else {
				fmt.Fprintf(os.Stderr, "✓ %s PASSED%s (%.1fs)\n", r.scenario.Name, retryNote, r.record.Duration.Seconds())
			}
		case "fail":
			failed++
			if *ciMode {
				fmt.Fprintf(os.Stderr, "--- FAIL %s: %s (%.1fs)\n", r.scenario.Name, r.record.Error, r.record.Duration.Seconds())
				for _, ar := range r.record.AssertionResults {
					if !ar.Passed {
						fmt.Fprintf(os.Stderr, "    %s: %s\n", ar.Name, ar.Message)
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "✗ %s FAILED: %s (%.1fs)\n", r.scenario.Name, r.record.Error, r.record.Duration.Seconds())
				for _, ar := range r.record.AssertionResults {
					if !ar.Passed {
						fmt.Fprintf(os.Stderr, "  FAIL: %s — %s\n", ar.Name, ar.Message)
					}
				}
			}
		default:
			errored++
			if *ciMode {
				fmt.Fprintf(os.Stderr, "--- ERROR %s: %s (%.1fs)\n", r.scenario.Name, r.record.Error, r.record.Duration.Seconds())
			} else {
				fmt.Fprintf(os.Stderr, "! %s ERROR: %s (%.1fs)\n", r.scenario.Name, r.record.Error, r.record.Duration.Seconds())
			}
		}
	}

	if *ciMode {
		fmt.Fprintf(os.Stderr, "\nRESULTS: %d passed, %d failed, %d errors\n", passed, failed, errored)
		writeJUnitXML(*artifactDir, records)
	} else {
		fmt.Fprintf(os.Stderr, "\n═══ Results: %d passed, %d failed, %d errors ═══\n", passed, failed, errored)
	}
	if failed > 0 || errored > 0 {
		os.Exit(1)
	}
}

// runScenarios executes scenarios with the given concurrency. When parallel is
// 1, scenarios run sequentially preserving order. When parallel > 1, a worker
// pool processes scenarios concurrently. Results are returned in the same order
// as the input scenarios slice.
func runScenarios(_ context.Context, scenarios []*scenario.Scenario, parallel int, runOne func(*scenario.Scenario) scenarioResult) []scenarioResult {
	results := make([]scenarioResult, len(scenarios))

	if parallel <= 1 {
		// Sequential execution.
		for i, s := range scenarios {
			fmt.Fprintf(os.Stderr, "\n═══ Running: %s ═══\n", s.Name)
			results[i] = runOne(s)
		}
		return results
	}

	// Parallel execution with semaphore pattern.
	type indexedScenario struct {
		index    int
		scenario *scenario.Scenario
	}

	work := make(chan indexedScenario, len(scenarios))
	for i, s := range scenarios {
		work <- indexedScenario{index: i, scenario: s}
	}
	close(work)

	var wg sync.WaitGroup
	wg.Add(parallel)
	for w := 0; w < parallel; w++ {
		go func() {
			defer wg.Done()
			for item := range work {
				fmt.Fprintf(os.Stderr, "\n═══ Running: %s ═══\n", item.scenario.Name)
				results[item.index] = runOne(item.scenario)
			}
		}()
	}
	wg.Wait()

	return results
}

func cmdPull(args []string) {
	if len(args) == 0 {
		fatalf("usage: drydock pull <scenario-path>")
	}

	scenarios := loadScenarios(args[0])

	// Collect unique images from all scenarios.
	images := collectImages(scenarios)
	if len(images) == 0 {
		fmt.Fprintln(os.Stderr, "drydock: no images to pull")
		return
	}

	requireDocker()

	fmt.Fprintf(os.Stderr, "drydock: pulling %d unique images...\n", len(images))
	var failed int
	for i, img := range images {
		fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", i+1, len(images), img)
		cmd := exec.Command("docker", "pull", img)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  WARN: failed to pull %s: %v\n", img, err)
			failed++
		}
	}
	fmt.Fprintf(os.Stderr, "drydock: pulled %d/%d images\n", len(images)-failed, len(images))
	if failed > 0 {
		os.Exit(1)
	}
}

// collectImages extracts unique Docker image names from all scenarios' services
// and old-format backends. Images with build-only configs (no image field) are skipped.
func collectImages(scenarios []*scenario.Scenario) []string {
	seen := make(map[string]bool)
	var images []string
	for _, s := range scenarios {
		for _, svc := range s.Services {
			if svc.Image != "" && !seen[svc.Image] {
				seen[svc.Image] = true
				images = append(images, svc.Image)
			}
		}
	}
	sort.Strings(images)
	return images
}

func cmdDebug(args []string) {
	fs := flag.NewFlagSet("debug", flag.ExitOnError)
	matrixFilter := fs.String("matrix", "", "select matrix variant (e.g. database=postgres)")
	fixedPorts := fs.Bool("fixed-ports", true, "use fixed host ports from YAML (default true for debug)")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fatalf("usage: drydock debug [--matrix <key=val>] <scenario-path>")
	}

	s, err := scenario.Load(fs.Arg(0))
	if err != nil {
		fatalf("loading scenario: %v", err)
	}

	// Resolve layers and expand matrix.
	resolved, err := scenario.ResolveLayersAndMatrix(s, nil)
	if err != nil {
		fatalf("resolving layers: %v", err)
	}

	// If matrix produced multiple variants, filter or pick the first.
	if len(resolved) > 1 && *matrixFilter != "" {
		filters := parseMatrixFilter(*matrixFilter)
		var filtered []*scenario.Scenario
		for _, r := range resolved {
			if matchesMatrixFilter(r.Name, filters) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) == 0 {
			fatalf("no matrix variant matches filter %q", *matrixFilter)
		}
		resolved = filtered
	}
	if len(resolved) > 1 {
		fmt.Fprintf(os.Stderr, "drydock: %d matrix variants available, using first: %s\n", len(resolved), resolved[0].Name)
		fmt.Fprintf(os.Stderr, "  use --matrix to select a specific variant\n")
	}
	s = resolved[0]

	// Apply --fixed-ports flag (defaults to true for debug mode).
	s.FixedPorts = *fixedPorts

	scenario.ExpandEnvBeforeValidate(s)
	if err := s.Validate(); err != nil {
		fatalf("validation failed: %v", err)
	}

	if s.NeedsDocker() {
		requireDocker()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng := engine.New(".drydock/runs")
	_, cleanup, err := eng.SetupDebug(ctx, s)
	if err != nil {
		fatalf("setup failed: %v", err)
	}

	// Print service information.
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Infrastructure is up.")
	if s.IsUnifiedFormat() {
		endpoints := engine.ServiceEndpoints(s)
		fmt.Fprintf(os.Stderr, "Services:\n")
		for _, ep := range endpoints {
			fmt.Fprintf(os.Stderr, "  %s\n", ep)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Backend: %s\n", s.Backend.Type)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Press Ctrl+C to tear down")
	fmt.Fprintln(os.Stderr)

	// Wait for interrupt.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	fmt.Fprintln(os.Stderr)
	cleanup()
	fmt.Fprintln(os.Stderr, "drydock: environment destroyed")
}

func cmdDestroy(args []string) {
	if len(args) == 0 {
		fatalf("usage: drydock destroy <scenario-path>")
	}

	scenarios := loadScenarios(args[0])
	eng := engine.New(".drydock/runs")
	ctx := context.Background()

	for _, s := range scenarios {
		scenario.ExpandEnvBeforeValidate(s)
		fmt.Fprintf(os.Stderr, "destroying: %s\n", s.Name)
		if err := eng.Destroy(ctx, s); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: %v\n", err)
		}
	}
}

func cmdInspect(args []string) {
	if len(args) == 0 {
		fatalf("usage: drydock inspect <run-id>")
	}
	eng := engine.New(".drydock/runs")
	record, err := eng.Inspect(args[0])
	if err != nil {
		fatalf("inspect: %v", err)
	}
	data, _ := json.MarshalIndent(record, "", "  ")
	fmt.Println(string(data))
}

func cmdList() {
	eng := engine.New(".drydock/runs")
	runs, err := eng.ListRuns()
	if err != nil {
		fatalf("list: %v", err)
	}
	if len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "no runs found")
		return
	}
	for _, id := range runs {
		fmt.Println(id)
	}
}

func loadScenarios(path string) []*scenario.Scenario {
	info, err := os.Stat(path)
	if err != nil {
		fatalf("path not found: %s", path)
	}

	var raw []*scenario.Scenario
	if info.IsDir() {
		raw, err = scenario.LoadDir(path)
	} else {
		var s *scenario.Scenario
		s, err = scenario.Load(path)
		if err == nil {
			raw = []*scenario.Scenario{s}
		}
	}
	if err != nil {
		fatalf("loading scenarios: %v", err)
	}
	if len(raw) == 0 {
		fatalf("no scenarios found at %s", path)
	}

	// Resolve layers and expand matrix variants.
	var scenarios []*scenario.Scenario
	for _, s := range raw {
		resolved, err := scenario.ResolveLayersAndMatrix(s, nil)
		if err != nil {
			fatalf("resolving layers for %q: %v", s.Name, err)
		}
		scenarios = append(scenarios, resolved...)
	}
	return scenarios
}

func matchesTags(scenarioTags, filterTags []string) bool {
	tagSet := make(map[string]bool, len(scenarioTags))
	for _, t := range scenarioTags {
		tagSet[t] = true
	}
	for _, t := range filterTags {
		if tagSet[strings.TrimSpace(t)] {
			return true
		}
	}
	return false
}

// parseMatrixFilter parses "database=postgres,cache=redis" into a map.
func parseMatrixFilter(s string) map[string]string {
	filters := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			filters[kv[0]] = kv[1]
		}
	}
	return filters
}

// matchesMatrixFilter checks if a scenario name contains all matrix filter
// values. Matrix-expanded scenarios are named like "base-postgres-redis".
func matchesMatrixFilter(name string, filters map[string]string) bool {
	for _, v := range filters {
		if !strings.Contains(name, v) {
			return false
		}
	}
	return true
}

// ── JUnit XML output for CI ──────────────────────────────────────────────────

type junitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Errors   int             `xml:"errors,attr"`
	Time     float64         `xml:"time,attr"`
	Cases    []junitTestCase `xml:"testcase"`
}

type junitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitError   `xml:"error,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

type junitError struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func writeJUnitXML(artifactDir string, records []*artifact.RunRecord) {
	var totalTests, totalFail, totalErr int
	var totalDur time.Duration
	var cases []junitTestCase

	for _, r := range records {
		totalTests++
		tc := junitTestCase{
			Name:      r.Scenario,
			Classname: "drydock",
			Time:      r.Duration.Seconds(),
		}
		totalDur += r.Duration

		switch r.Status {
		case "fail":
			totalFail++
			var details []string
			for _, ar := range r.AssertionResults {
				if !ar.Passed {
					details = append(details, ar.Name+": "+ar.Message)
				}
			}
			tc.Failure = &junitFailure{
				Message: r.Error,
				Body:    strings.Join(details, "\n"),
			}
		case "error":
			totalErr++
			tc.Error = &junitError{Message: r.Error}
		}
		cases = append(cases, tc)
	}

	suites := junitTestSuites{
		Suites: []junitTestSuite{{
			Name:     "drydock",
			Tests:    totalTests,
			Failures: totalFail,
			Errors:   totalErr,
			Time:     totalDur.Seconds(),
			Cases:    cases,
		}},
	}

	data, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "drydock: failed to generate JUnit XML: %v\n", err)
		return
	}

	_ = os.MkdirAll(artifactDir, 0o750)
	path := filepath.Join(artifactDir, "junit.xml")
	if err := os.WriteFile(path, append([]byte(xml.Header), data...), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "drydock: failed to write %s: %v\n", path, err)
		return
	}
	fmt.Fprintf(os.Stderr, "JUnit XML: %s\n", path)
}
