// Package scenario defines the declarative scenario format and provides
// YAML loading and validation.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario is the top-level declarative test definition.
type Scenario struct {
	// Name identifies this scenario. Must be unique within a run.
	Name string `yaml:"name"`

	// Description explains what this scenario tests.
	Description string `yaml:"description,omitempty"`

	// Backend configures the environment provider (compose, terraform, or both).
	Backend Backend `yaml:"backend"`

	// Setup runs before the main commands. Use for seeding data, waiting for readiness, etc.
	Setup []Command `yaml:"setup,omitempty"`

	// Commands are the test steps to execute against the environment.
	Commands []Command `yaml:"commands"`

	// Assertions validate the environment state after commands run.
	Assertions []Assertion `yaml:"assertions,omitempty"`

	// Timeout is the maximum wall-clock time for the entire scenario.
	// Defaults to 10m if unset.
	Timeout Duration `yaml:"timeout,omitempty"`

	// Artifacts configures what to collect after the run.
	Artifacts ArtifactConfig `yaml:"artifacts,omitempty"`

	// Env injects environment variables into all commands and backends.
	Env map[string]string `yaml:"env,omitempty"`

	// Tags enable filtering scenarios by category.
	Tags []string `yaml:"tags,omitempty"`

	// Dir is the directory containing the scenario file (set by loader, not YAML).
	Dir string `yaml:"-"`
}

// Backend configures the environment provider.
type Backend struct {
	// Type selects the backend: "compose", "terraform", or "hybrid" (both).
	Type string `yaml:"type"`

	// ComposeFile is the path to the Docker Compose file (relative to scenario dir).
	// Used when Type is "compose" or "hybrid".
	ComposeFile string `yaml:"compose_file,omitempty"`

	// TerraformDir is the path to the Terraform root module (relative to scenario dir).
	// Used when Type is "terraform" or "hybrid".
	TerraformDir string `yaml:"terraform_dir,omitempty"`

	// TerraformVars are variables passed to terraform apply -var.
	TerraformVars map[string]string `yaml:"terraform_vars,omitempty"`

	// TerraformBackend overrides the backend config (e.g. for remote state).
	TerraformBackend map[string]string `yaml:"terraform_backend,omitempty"`

	// AutoApprove skips the interactive approval for terraform apply/destroy.
	// Defaults to true in drydock (sandbox environments are disposable).
	AutoApprove *bool `yaml:"auto_approve,omitempty"`

	// Workspace is the Terraform workspace name. Defaults to "drydock-<scenario-name>".
	Workspace string `yaml:"workspace,omitempty"`
}

// Command is a single step to execute.
type Command struct {
	// Name identifies this command in logs and artifacts.
	Name string `yaml:"name"`

	// Run is the shell command to execute.
	Run string `yaml:"run"`

	// Dir overrides the working directory for this command.
	Dir string `yaml:"dir,omitempty"`

	// Env adds environment variables for this command only.
	Env map[string]string `yaml:"env,omitempty"`

	// Timeout overrides the scenario-level timeout for this command.
	Timeout Duration `yaml:"timeout,omitempty"`

	// ContinueOnError allows subsequent commands to run even if this one fails.
	ContinueOnError bool `yaml:"continue_on_error,omitempty"`

	// Expect configures exit code and output assertions for this specific command.
	Expect *CommandExpect `yaml:"expect,omitempty"`
}

// CommandExpect defines what a command should produce.
type CommandExpect struct {
	ExitCode *int   `yaml:"exit_code,omitempty"` // nil means "must be 0"
	Stdout   string `yaml:"stdout,omitempty"`    // substring match
	Stderr   string `yaml:"stderr,omitempty"`    // substring match
	NotStdout string `yaml:"not_stdout,omitempty"` // must NOT contain
}

// Assertion validates environment state after all commands complete.
type Assertion struct {
	// Name describes what this assertion checks.
	Name string `yaml:"name"`

	// Type selects the assertion engine.
	//   "http"       — HTTP request to a URL, check status/body
	//   "port"       — TCP port is open/closed
	//   "command"    — Run a command, check exit code/output
	//   "terraform"  — Check terraform output values
	//   "file"       — Check file exists/contains
	Type string `yaml:"type"`

	// Target is assertion-type-specific (URL for http, host:port for port, etc.)
	Target string `yaml:"target,omitempty"`

	// Expect defines the expected result.
	Expect AssertionExpect `yaml:"expect"`
}

// AssertionExpect defines expected outcomes for assertions.
type AssertionExpect struct {
	// HTTP assertions
	Status   *int   `yaml:"status,omitempty"`
	Body     string `yaml:"body,omitempty"`      // substring match
	NotBody  string `yaml:"not_body,omitempty"`  // must NOT contain
	Header   string `yaml:"header,omitempty"`    // header name to check
	HeaderValue string `yaml:"header_value,omitempty"` // expected header value

	// Port assertions
	Open   *bool `yaml:"open,omitempty"`

	// Command assertions
	Command  string `yaml:"command,omitempty"`
	ExitCode *int   `yaml:"exit_code,omitempty"`
	Stdout   string `yaml:"stdout,omitempty"`
	NotStdout string `yaml:"not_stdout,omitempty"`

	// Terraform assertions
	Output      string `yaml:"output,omitempty"`       // terraform output name
	OutputValue string `yaml:"output_value,omitempty"` // expected value
	OutputMatch string `yaml:"output_match,omitempty"` // regex match

	// File assertions
	Exists   *bool  `yaml:"exists,omitempty"`
	Contains string `yaml:"contains,omitempty"`
}

// ArtifactConfig controls what gets collected after a run.
type ArtifactConfig struct {
	ContainerLogs bool     `yaml:"container_logs,omitempty"`
	TerraformPlan bool     `yaml:"terraform_plan,omitempty"`
	TerraformState bool    `yaml:"terraform_state,omitempty"`
	Files         []string `yaml:"files,omitempty"` // extra files to collect
}

// Duration wraps time.Duration with YAML marshaling support.
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

// Load reads a scenario from a YAML file.
func Load(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading scenario: %w", err)
	}

	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing scenario %s: %w", path, err)
	}

	s.Dir = filepath.Dir(path)

	if s.Timeout.Duration == 0 {
		s.Timeout.Duration = 10 * time.Minute
	}

	return &s, nil
}

// Validate checks that a scenario is well-formed.
func (s *Scenario) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("scenario name is required")
	}
	if s.Backend.Type == "" {
		return fmt.Errorf("backend.type is required")
	}
	switch s.Backend.Type {
	case "compose":
		if s.Backend.ComposeFile == "" {
			return fmt.Errorf("backend.compose_file is required for compose backend")
		}
	case "terraform":
		if s.Backend.TerraformDir == "" {
			return fmt.Errorf("backend.terraform_dir is required for terraform backend")
		}
	case "hybrid":
		if s.Backend.ComposeFile == "" && s.Backend.TerraformDir == "" {
			return fmt.Errorf("hybrid backend requires at least compose_file or terraform_dir")
		}
	default:
		return fmt.Errorf("unsupported backend type: %q (use compose, terraform, or hybrid)", s.Backend.Type)
	}
	if len(s.Commands) == 0 && len(s.Assertions) == 0 {
		return fmt.Errorf("scenario must have at least one command or assertion")
	}
	for i, c := range s.Commands {
		if c.Name == "" {
			return fmt.Errorf("command[%d].name is required", i)
		}
		if c.Run == "" {
			return fmt.Errorf("command[%d].run is required", i)
		}
	}
	for i, a := range s.Assertions {
		if a.Name == "" {
			return fmt.Errorf("assertion[%d].name is required", i)
		}
		switch a.Type {
		case "http", "port", "command", "terraform", "file":
			// valid
		default:
			return fmt.Errorf("assertion[%d]: unsupported type %q", i, a.Type)
		}
	}
	return nil
}

// LoadDir loads all scenario YAML files from a directory.
func LoadDir(dir string) ([]*Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading scenario directory: %w", err)
	}

	var scenarios []*Scenario
	for _, entry := range entries {
		if entry.IsDir() {
			// Check for scenario.yaml inside subdirectory.
			subPath := filepath.Join(dir, entry.Name(), "scenario.yaml")
			if _, err := os.Stat(subPath); err == nil {
				s, err := Load(subPath)
				if err != nil {
					return nil, err
				}
				scenarios = append(scenarios, s)
			}
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".yaml" || filepath.Ext(name) == ".yml" {
			s, err := Load(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			scenarios = append(scenarios, s)
		}
	}
	return scenarios, nil
}
