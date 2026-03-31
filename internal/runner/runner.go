// Package runner executes shell commands and captures their output.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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

	// Merge environment.
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

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

		if result.ExitCode != expectedExit && !cmd.ContinueOnError {
			result.Error = fmt.Sprintf("expected exit code %d, got %d", expectedExit, result.ExitCode)
			break
		}
	}
	return results
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
