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
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

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
  drydock debug <path>                 Start infrastructure and wait for manual testing
  drydock destroy <path>               Tear down a scenario's environment
  drydock inspect <run-id>             Show results of a previous run
  drydock list                         List all previous runs
  drydock bootstrap [gcp|aws|all]      Interactive setup for test infrastructure
  drydock version                      Print version

Run flags:
  --tags <tag1,tag2>      Only run scenarios with matching tags
  --artifacts <dir>       Artifact output directory (default: .drydock/runs)
  --json                  Output results as JSON`)
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

func cmdRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	tags := fs.String("tags", "", "comma-separated tags to filter scenarios")
	artifactDir := fs.String("artifacts", ".drydock/runs", "artifact output directory")
	jsonOutput := fs.Bool("json", false, "output results as JSON")
	_ = fs.Parse(args)

	if fs.NArg() == 0 {
		fatalf("usage: drydock run [--tags <tags>] <scenario-path>")
	}

	requireDocker()

	scenarios := loadScenarios(fs.Arg(0))

	// Filter by tags.
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

	if len(scenarios) == 0 {
		fatalf("no matching scenarios found")
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

	// Run scenarios.
	var passed, failed, errored int
	for _, s := range scenarios {
		fmt.Fprintf(os.Stderr, "\n═══ Running: %s ═══\n", s.Name)
		record, err := eng.Run(ctx, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			errored++
			continue
		}

		if *jsonOutput {
			data, _ := json.MarshalIndent(record, "", "  ")
			fmt.Println(string(data))
		}

		switch record.Status {
		case "pass":
			passed++
			fmt.Fprintf(os.Stderr, "✓ %s PASSED (%.1fs)\n", s.Name, record.Duration.Seconds())
		case "fail":
			failed++
			fmt.Fprintf(os.Stderr, "✗ %s FAILED: %s (%.1fs)\n", s.Name, record.Error, record.Duration.Seconds())
			for _, ar := range record.AssertionResults {
				if !ar.Passed {
					fmt.Fprintf(os.Stderr, "  FAIL: %s — %s\n", ar.Name, ar.Message)
				}
			}
		default:
			errored++
			fmt.Fprintf(os.Stderr, "! %s ERROR: %s (%.1fs)\n", s.Name, record.Error, record.Duration.Seconds())
		}
	}

	fmt.Fprintf(os.Stderr, "\n═══ Results: %d passed, %d failed, %d errors ═══\n", passed, failed, errored)
	if failed > 0 || errored > 0 {
		os.Exit(1)
	}
}

func cmdDebug(args []string) {
	if len(args) == 0 {
		fatalf("usage: drydock debug <scenario-path>")
	}

	requireDocker()

	s, err := scenario.Load(args[0])
	if err != nil {
		fatalf("loading scenario: %v", err)
	}
	if err := s.Validate(); err != nil {
		fatalf("validation failed: %v", err)
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

	var scenarios []*scenario.Scenario
	if info.IsDir() {
		scenarios, err = scenario.LoadDir(path)
	} else {
		var s *scenario.Scenario
		s, err = scenario.Load(path)
		if err == nil {
			scenarios = []*scenario.Scenario{s}
		}
	}
	if err != nil {
		fatalf("loading scenarios: %v", err)
	}
	if len(scenarios) == 0 {
		fatalf("no scenarios found at %s", path)
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
