// Package scenario defines the declarative scenario format and provides
// YAML loading and validation.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	// Used in the old multi-file format. Mutually exclusive with Services for
	// compose-based tests — if Services is set, the backend is generated automatically.
	Backend Backend `yaml:"backend"`

	// Services defines compose-compatible service definitions inline.
	// When set, a temporary compose.yaml is generated and the backend is set to "compose".
	// This is the recommended format for new tests.
	Services map[string]ComposeService `yaml:"services,omitempty"`

	// Ready defines a single readiness check that replaces setup wait loops.
	// The engine runs Ready.Cmd repeatedly at Ready.Interval until it succeeds
	// or Ready.Timeout elapses.
	Ready *ReadyCheck `yaml:"ready,omitempty"`

	// Run is a list of shell commands to execute after the environment is ready.
	// These run sequentially before assertions. Optional — assertions may handle
	// their own commands (e.g. beacon assertions run beacon scan internally).
	Run []string `yaml:"run,omitempty"`

	// Setup runs before the main commands. Use for seeding data, waiting for readiness, etc.
	Setup []Command `yaml:"setup,omitempty"`

	// Commands are the test steps to execute against the environment.
	Commands []Command `yaml:"commands"`

	// Assertions validate the environment state after commands run.
	Assertions []Assertion `yaml:"assertions,omitempty"`

	// Timeout is the maximum wall-clock time for the entire scenario.
	// Defaults to 10m if unset.
	Timeout Duration `yaml:"timeout,omitempty"`

	// Fixture optionally provisions prerequisite infrastructure (always Terraform).
	// Its outputs are interpolated into the rest of the scenario as ${fixture.<output>}.
	Fixture *Fixture `yaml:"fixture,omitempty"`

	// Artifacts configures what to collect after the run.
	Artifacts ArtifactConfig `yaml:"artifacts,omitempty"`

	// Env injects environment variables into all commands and backends.
	Env map[string]string `yaml:"env,omitempty"`

	// Tags enable filtering scenarios by category.
	Tags []string `yaml:"tags,omitempty"`

	// Dir is the directory containing the scenario file (set by loader, not YAML).
	Dir string `yaml:"-"`
}

// IsUnifiedFormat returns true if this scenario uses the new unified manifest
// format with inline service definitions.
func (s *Scenario) IsUnifiedFormat() bool {
	return len(s.Services) > 0
}

// ComposeService mirrors Docker Compose service configuration.
// Fields are passed through to the generated compose.yaml as-is.
type ComposeService struct {
	Image       string            `yaml:"image,omitempty"`
	Build       *ComposeBuild     `yaml:"build,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty"`
	Command     any               `yaml:"command,omitempty"`
	Entrypoint  any               `yaml:"entrypoint,omitempty"`
	HealthCheck *ComposeHealth    `yaml:"healthcheck,omitempty"`
	CapAdd      []string          `yaml:"cap_add,omitempty"`
	SecurityOpt []string         `yaml:"security_opt,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
	Restart     string            `yaml:"restart,omitempty"`
}

// ComposeBuild mirrors the Docker Compose build configuration.
type ComposeBuild struct {
	Context    string            `yaml:"context,omitempty"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
}

// ComposeHealth mirrors the Docker Compose healthcheck configuration.
type ComposeHealth struct {
	Test     []string `yaml:"test,omitempty"`
	Interval string   `yaml:"interval,omitempty"`
	Timeout  string   `yaml:"timeout,omitempty"`
	Retries  int      `yaml:"retries,omitempty"`
	Start    string   `yaml:"start_period,omitempty"`
}

// ReadyCheck defines a single readiness probe for the environment.
type ReadyCheck struct {
	// Cmd is the shell command to run as a health probe.
	Cmd string `yaml:"cmd"`

	// Timeout is the maximum time to wait for readiness. Defaults to 60s.
	Timeout Duration `yaml:"timeout,omitempty"`

	// Interval is the time between probe attempts. Defaults to 2s.
	Interval Duration `yaml:"interval,omitempty"`
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

	// ── GitHub Actions backend ──────────────────────────────────────────

	// Repo is the GitHub repository (owner/name) for the workflow.
	// Required when Type is "github-actions".
	Repo string `yaml:"repo,omitempty"`

	// Workflow is the workflow filename or ID to trigger.
	Workflow string `yaml:"workflow,omitempty"`

	// Ref is the git ref (branch/tag) to run against. Defaults to the repo's default branch.
	Ref string `yaml:"ref,omitempty"`

	// Trigger selects how to start the run: "workflow_dispatch" (default) or "push".
	Trigger string `yaml:"trigger,omitempty"`

	// Inputs are key-value pairs passed to the workflow_dispatch event.
	Inputs map[string]string `yaml:"inputs,omitempty"`
}

// Fixture provisions prerequisite cloud infrastructure before the main backend.
// It is always a Terraform module. Its outputs are available for interpolation
// via ${fixture.<output_name>} in the rest of the scenario.
type Fixture struct {
	// Module is the path to the Terraform module (relative to scenario dir).
	Module string `yaml:"module"`

	// Vars are variables passed to terraform apply -var.
	Vars map[string]string `yaml:"vars,omitempty"`

	// Workspace overrides the Terraform workspace. Defaults to "drydock-fixture-<scenario-name>".
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
	//   "beacon"     — Run beacon scan, check for findings
	Type string `yaml:"type"`

	// Target is assertion-type-specific (URL for http, host:port for port, etc.)
	Target string `yaml:"target,omitempty"`

	// Args are extra CLI arguments appended to the tool invocation.
	// Currently used by the "beacon" assertion type (e.g. ["--scanners", "cors"]).
	Args []string `yaml:"args,omitempty"`

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

	// Beacon assertions
	CheckID       string `yaml:"check_id,omitempty"`       // finding check ID to look for
	NotCheckID    string `yaml:"not_check_id,omitempty"`    // finding check ID that must NOT appear
	Severity      string `yaml:"severity,omitempty"`        // expected severity (critical, high, medium, low, info)
	MinFindings   *int   `yaml:"min_findings,omitempty"`    // minimum number of findings expected
	MaxFindings   *int   `yaml:"max_findings,omitempty"`    // maximum number of findings expected
	EvidenceKey   string `yaml:"evidence_key,omitempty"`    // key in evidence map to check
	EvidenceValue string `yaml:"evidence_value,omitempty"`  // expected value for evidence key

	// GitHub Actions assertions
	Conclusion   string `yaml:"conclusion,omitempty"`    // expected conclusion (success, failure, etc.)
	Job          string `yaml:"job,omitempty"`           // job name to assert on
	StepName     string `yaml:"step_name,omitempty"`     // step name within a job
	ArtifactName string `yaml:"artifact_name,omitempty"` // artifact that should be present

	// Classify assertions — beacon classify --format json output fields
	ProxyType       string `yaml:"proxy_type,omitempty"`       // exact match on proxy_type
	FrameworkField  string `yaml:"framework,omitempty"`        // exact match on framework
	CloudProviderField string `yaml:"cloud_provider,omitempty"` // exact match on cloud_provider
	AuthSystemField string `yaml:"auth_system,omitempty"`      // exact match on auth_system
	InfraLayerField string `yaml:"infra_layer,omitempty"`      // exact match on infra_layer
	IsKubernetes    *bool  `yaml:"is_kubernetes,omitempty"`     // boolean match
	IsServerless    *bool  `yaml:"is_serverless,omitempty"`     // boolean match
	IsReverseProxy  *bool  `yaml:"is_reverse_proxy,omitempty"`  // boolean match
	HasDMARC        *bool  `yaml:"has_dmarc,omitempty"`          // boolean match
	BackendService  string `yaml:"backend_service,omitempty"`   // value must appear in backend_services array
	ServiceVersion  string `yaml:"service_version,omitempty"`   // key must exist in service_versions map
	CookieName      string `yaml:"cookie_name,omitempty"`       // value must appear in cookie_names array
	PathResponds    string `yaml:"path_responds,omitempty"`     // path must appear in responding_paths array
	MatchedPlaybook string `yaml:"matched_playbook,omitempty"`  // playbook must appear in matched_playbooks array
	StatusCodeField *int   `yaml:"status_code,omitempty"`       // integer match on status_code
	TitleContains   string `yaml:"title_contains,omitempty"`    // substring match on title
	NotProxyType    string `yaml:"not_proxy_type,omitempty"`    // must NOT be this proxy_type
	NotFramework    string `yaml:"not_framework,omitempty"`     // must NOT be this framework
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
	return d.String(), nil
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

	// Apply defaults for ready check.
	if s.Ready != nil {
		if s.Ready.Timeout.Duration == 0 {
			s.Ready.Timeout.Duration = 60 * time.Second
		}
		if s.Ready.Interval.Duration == 0 {
			s.Ready.Interval.Duration = 2 * time.Second
		}
	}

	return &s, nil
}

// Validate checks that a scenario is well-formed.
func (s *Scenario) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("scenario name is required")
	}
	if s.Fixture != nil {
		if s.Fixture.Module == "" {
			return fmt.Errorf("fixture.module is required when fixture is specified")
		}
	}

	// New unified format: services block replaces backend for compose-based tests.
	if s.IsUnifiedFormat() {
		// Validate services.
		for name, svc := range s.Services {
			if svc.Image == "" && svc.Build == nil {
				return fmt.Errorf("service %q must have either image or build", name)
			}
		}
		// Ready check validation.
		if s.Ready != nil && s.Ready.Cmd == "" {
			return fmt.Errorf("ready.cmd is required when ready is specified")
		}
		// In unified format, run + assertions are sufficient.
		if len(s.Run) == 0 && len(s.Commands) == 0 && len(s.Assertions) == 0 {
			return fmt.Errorf("scenario must have at least one run command, command, or assertion")
		}
	} else {
		// Old format: backend is required.
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
		case "github-actions":
			if s.Backend.Repo == "" {
				return fmt.Errorf("backend.repo is required for github-actions backend")
			}
			if s.Backend.Workflow == "" {
				return fmt.Errorf("backend.workflow is required for github-actions backend")
			}
		default:
			return fmt.Errorf("unsupported backend type: %q (use compose, terraform, hybrid, or github-actions)", s.Backend.Type)
		}
		if len(s.Commands) == 0 && len(s.Assertions) == 0 {
			return fmt.Errorf("scenario must have at least one command or assertion")
		}
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
		case "http", "port", "command", "terraform", "file", "beacon", "classify",
			"github-run", "github-job", "github-step", "github-artifact":
			// valid
		default:
			return fmt.Errorf("assertion[%d]: unsupported type %q", i, a.Type)
		}
	}
	return nil
}

// portPattern matches port numbers preceded by a colon, -p flag, or space (for nc-style commands).
var portPattern = regexp.MustCompile(`([:,\s-][pP]\s*)(\d{2,5})`)

// RemapPorts applies a port offset to all host port bindings in the scenario,
// updating service port mappings, ready checks, assertion targets, and assertion args.
// This enables parallel test execution without port conflicts.
func (s *Scenario) RemapPorts(offset int) {
	if offset == 0 || !s.IsUnifiedFormat() {
		return
	}

	// Collect all host ports and compute their remapped values.
	portMap := make(map[string]string) // "8080" → "8180"
	for name, svc := range s.Services {
		for i, p := range svc.Ports {
			// Handle port ranges like "21100-21110:21100-21110"
			hostContainer := strings.SplitN(p, ":", 2)
			if len(hostContainer) != 2 {
				continue
			}
			hostPart := hostContainer[0]
			containerPart := hostContainer[1]

			// Handle range format "start-end"
			if strings.Contains(hostPart, "-") {
				rangeParts := strings.SplitN(hostPart, "-", 2)
				start, err1 := strconv.Atoi(rangeParts[0])
				end, err2 := strconv.Atoi(rangeParts[1])
				if err1 != nil || err2 != nil {
					continue
				}
				newStart := start + offset
				newEnd := end + offset
				portMap[rangeParts[0]] = strconv.Itoa(newStart)
				portMap[rangeParts[1]] = strconv.Itoa(newEnd)
				svc.Ports[i] = fmt.Sprintf("%d-%d:%s", newStart, newEnd, containerPart)
			} else {
				port, err := strconv.Atoi(hostPart)
				if err != nil {
					continue
				}
				newPort := port + offset
				portMap[hostPart] = strconv.Itoa(newPort)
				svc.Ports[i] = fmt.Sprintf("%d:%s", newPort, containerPart)
			}
		}
		s.Services[name] = svc
	}

	if len(portMap) == 0 {
		return
	}

	// Rewrite ready check command.
	if s.Ready != nil {
		s.Ready.Cmd = remapPortsInString(s.Ready.Cmd, portMap)
	}

	// Rewrite assertion targets and args.
	for i := range s.Assertions {
		s.Assertions[i].Target = remapPortsInString(s.Assertions[i].Target, portMap)
		for j := range s.Assertions[i].Args {
			s.Assertions[i].Args[j] = remapPortsInString(s.Assertions[i].Args[j], portMap)
		}
	}

	// Rewrite run commands.
	for i := range s.Run {
		s.Run[i] = remapPortsInString(s.Run[i], portMap)
	}
}

// remapPortsInString replaces port numbers in a string using the given mapping.
// It handles common patterns: ":PORT", "-p PORT", "-P PORT", "localhost PORT",
// and standalone port numbers that match exactly.
func remapPortsInString(s string, portMap map[string]string) string {
	for old, new := range portMap {
		// Replace :PORT (URLs, host:port targets)
		s = strings.ReplaceAll(s, ":"+old, ":"+new)
		// Replace -p PORT and -P PORT (CLI flags)
		s = strings.ReplaceAll(s, "-p "+old, "-p "+new)
		s = strings.ReplaceAll(s, "-P "+old, "-P "+new)
		// Replace "localhost PORT" (nc-style)
		s = strings.ReplaceAll(s, "localhost "+old, "localhost "+new)
		// Replace "--port PORT" (generic CLI)
		s = strings.ReplaceAll(s, "--port "+old, "--port "+new)
	}
	// Suppress unused portPattern warning — reserved for future use.
	_ = portPattern
	return s
}

// Clone returns a deep copy of the scenario suitable for mutation (e.g. port remapping).
func (s *Scenario) Clone() *Scenario {
	c := *s // shallow copy

	// Deep copy Services map.
	if s.Services != nil {
		c.Services = make(map[string]ComposeService, len(s.Services))
		for k, v := range s.Services {
			// Deep copy Ports slice.
			if v.Ports != nil {
				ports := make([]string, len(v.Ports))
				copy(ports, v.Ports)
				v.Ports = ports
			}
			// Deep copy Volumes slice.
			if v.Volumes != nil {
				vols := make([]string, len(v.Volumes))
				copy(vols, v.Volumes)
				v.Volumes = vols
			}
			c.Services[k] = v
		}
	}

	// Deep copy Ready.
	if s.Ready != nil {
		rc := *s.Ready
		c.Ready = &rc
	}

	// Deep copy Assertions.
	if s.Assertions != nil {
		c.Assertions = make([]Assertion, len(s.Assertions))
		for i, a := range s.Assertions {
			c.Assertions[i] = a
			if a.Args != nil {
				args := make([]string, len(a.Args))
				copy(args, a.Args)
				c.Assertions[i].Args = args
			}
		}
	}

	// Deep copy Run.
	if s.Run != nil {
		run := make([]string, len(s.Run))
		copy(run, s.Run)
		c.Run = run
	}

	return &c
}

// LoadDir loads all scenario YAML files from a directory.
func LoadDir(dir string) ([]*Scenario, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading scenario directory: %w", err)
	}

	// Index YAML files so we can identify support directories (e.g. envoy/
	// sitting next to envoy.yaml is a volume-mount directory, not a scenario dir).
	yamlBases := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext == ".yaml" || ext == ".yml" {
			yamlBases[strings.TrimSuffix(name, ext)] = true
		}
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
				continue
			}
			// Skip support directories that share a name with a sibling
			// YAML file (e.g. envoy/ alongside envoy.yaml contains config
			// files mounted as volumes, not scenarios).
			if yamlBases[entry.Name()] {
				continue
			}
			// Recurse into subdirectories to support nested layouts
			// like tests/databases/redis.yaml.
			sub, err := LoadDir(filepath.Join(dir, entry.Name()))
			if err != nil {
				return nil, err
			}
			scenarios = append(scenarios, sub...)
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
