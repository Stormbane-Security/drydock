// Package scenario — compose file generation for unified manifests.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// composeFile is the top-level Docker Compose v3 structure.
type composeFile struct {
	Services map[string]composeServiceOut `yaml:"services"`
}

// composeServiceOut is the output representation of a service for the generated
// compose.yaml. We use a separate struct from ComposeService so that we
// control exactly what gets marshaled (e.g. omitempty on all fields).
type composeServiceOut struct {
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

// GenerateComposeFile marshals the services map into a valid Docker Compose v3
// YAML file, writes it to a temp directory, and returns the file path.
// The caller is responsible for removing the temp directory when done.
func GenerateComposeFile(services map[string]ComposeService) (string, error) {
	if len(services) == 0 {
		return "", fmt.Errorf("no services defined")
	}

	out := composeFile{
		Services: make(map[string]composeServiceOut, len(services)),
	}

	for name, svc := range services {
		out.Services[name] = composeServiceOut{
			Image:       svc.Image,
			Build:       svc.Build,
			Ports:       svc.Ports,
			Environment: svc.Environment,
			Volumes:     svc.Volumes,
			DependsOn:   svc.DependsOn,
			Command:     svc.Command,
			Entrypoint:  svc.Entrypoint,
			HealthCheck: svc.HealthCheck,
			CapAdd:      svc.CapAdd,
			SecurityOpt: svc.SecurityOpt,
			Networks:    svc.Networks,
			Restart:     svc.Restart,
		}
	}

	data, err := yaml.Marshal(&out)
	if err != nil {
		return "", fmt.Errorf("marshaling compose file: %w", err)
	}

	dir, err := os.MkdirTemp("", "drydock-compose-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("writing compose file: %w", err)
	}

	return path, nil
}

// GenerateComposeBytes marshals the services map into Docker Compose v3 YAML bytes.
func GenerateComposeBytes(services map[string]ComposeService) ([]byte, error) {
	if len(services) == 0 {
		return nil, fmt.Errorf("no services defined")
	}

	out := composeFile{
		Services: make(map[string]composeServiceOut, len(services)),
	}

	for name, svc := range services {
		out.Services[name] = composeServiceOut{
			Image:       svc.Image,
			Build:       svc.Build,
			Ports:       svc.Ports,
			Environment: svc.Environment,
			Volumes:     svc.Volumes,
			DependsOn:   svc.DependsOn,
			Command:     svc.Command,
			Entrypoint:  svc.Entrypoint,
			HealthCheck: svc.HealthCheck,
			CapAdd:      svc.CapAdd,
			SecurityOpt: svc.SecurityOpt,
			Networks:    svc.Networks,
			Restart:     svc.Restart,
		}
	}

	return yaml.Marshal(&out)
}
