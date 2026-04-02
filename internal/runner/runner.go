// Package runner executes shell commands and captures their output.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result holds the output of a single command execution.
type Result struct {
	Name     string        `json:"name"`
	Command  string        `json:"command"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
}

// Run executes a shell command and returns the result.
func Run(ctx context.Context, name, command, dir string, env map[string]string) Result {
	start := time.Now()
	result := Result{
		Name:    name,
		Command: command,
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Env = mergeEnv(env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	}

	return result
}

// RunExec executes a command with explicit argument separation (no shell).
// This prevents shell injection when arguments come from user input.
func RunExec(ctx context.Context, name string, argv []string, dir string, env map[string]string) Result {
	start := time.Now()
	result := Result{
		Name:    name,
		Command: fmt.Sprintf("%v", argv),
	}

	if len(argv) == 0 {
		result.ExitCode = -1
		result.Error = "empty command"
		return result
	}

	// Resolve binary using the caller's custom PATH if provided,
	// since exec.CommandContext uses the current process PATH.
	binary := argv[0]
	merged := mergeEnv(env)
	if customPath, ok := env["PATH"]; ok && !strings.Contains(binary, "/") {
		for _, dir := range strings.Split(customPath, ":") {
			candidate := dir + "/" + binary
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				binary = candidate
				break
			}
		}
	}

	cmd := exec.CommandContext(ctx, binary, argv[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Env = merged

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err.Error()
		}
	}

	return result
}

// RunAll executes a list of commands sequentially, stopping on first failure
// unless ContinueOnError is set.
func RunAll(ctx context.Context, commands []CommandSpec, baseDir string, baseEnv map[string]string) []Result {
	var results []Result
	for _, cmd := range commands {
		// Merge per-command env with base env.
		env := make(map[string]string, len(baseEnv)+len(cmd.Env))
		for k, v := range baseEnv {
			env[k] = v
		}
		for k, v := range cmd.Env {
			env[k] = v
		}

		// Apply per-command timeout if set.
		cmdCtx := ctx
		if cmd.Timeout > 0 {
			var cancel context.CancelFunc
			cmdCtx, cancel = context.WithTimeout(ctx, cmd.Timeout)
			defer cancel()
		}

		dir := baseDir
		if cmd.Dir != "" {
			dir = cmd.Dir
		}

		result := Run(cmdCtx, cmd.Name, cmd.Run, dir, env)
		results = append(results, result)

		// Check expected exit code.
		expectedExit := 0
		if cmd.ExpectExit != nil {
			expectedExit = *cmd.ExpectExit
		}

		if result.ExitCode != expectedExit {
			if result.Error == "" {
				result.Error = fmt.Sprintf("expected exit code %d, got %d", expectedExit, result.ExitCode)
			}
			results[len(results)-1] = result
			if !cmd.ContinueOnError {
				break
			}
		}
	}
	return results
}

// mergeEnv returns os.Environ() with overrides from env applied.
// Unlike simple append, this replaces existing variables so that
// callers can override PATH, HOME, etc.
func mergeEnv(env map[string]string) []string {
	if len(env) == 0 {
		return os.Environ()
	}
	// Build set of override keys (uppercased for case-insensitive match on macOS).
	base := os.Environ()
	overrideKeys := make(map[string]bool, len(env))
	for k := range env {
		overrideKeys[k] = true
	}
	// Copy base env, skipping keys that will be overridden.
	result := make([]string, 0, len(base)+len(env))
	for _, entry := range base {
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			if overrideKeys[entry[:idx]] {
				continue
			}
		}
		result = append(result, entry)
	}
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// CommandSpec is the runner's view of a command to execute.
type CommandSpec struct {
	Name            string
	Run             string
	Dir             string
	Env             map[string]string
	Timeout         time.Duration
	ContinueOnError bool
	ExpectExit      *int
}
