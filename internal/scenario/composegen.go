// Package scenario — compose file generation for unified manifests.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
// scenarioDir is the directory containing the scenario YAML — relative paths
// in build contexts and volume mounts are resolved against it so Docker Compose
// can find them even though the generated file lives in a temp directory.
// The caller is responsible for removing the temp directory when done.
func GenerateComposeFile(services map[string]ComposeService, scenarioDir string) (string, error) {
	if len(services) == 0 {
		return "", fmt.Errorf("no services defined")
	}

	absScenarioDir, _ := filepath.Abs(scenarioDir)

	out := composeFile{
		Services: make(map[string]composeServiceOut, len(services)),
	}

	for name, svc := range services {
		s := composeServiceOut{
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

		// Resolve relative build context to absolute path so docker compose
		// can find it from the temp directory.
		if s.Build != nil && s.Build.Context != "" && !filepath.IsAbs(s.Build.Context) {
			resolved := filepath.Join(absScenarioDir, s.Build.Context)
			s.Build = &ComposeBuild{
				Context:    resolved,
				Dockerfile: s.Build.Dockerfile,
				Args:       s.Build.Args,
			}
		}

		// Resolve relative volume mount sources to absolute paths.
		if len(s.Volumes) > 0 {
			resolved := make([]string, len(s.Volumes))
			for i, v := range s.Volumes {
				resolved[i] = resolveVolumePath(v, absScenarioDir)
			}
			s.Volumes = resolved
		}

		out.Services[name] = s
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

// resolveVolumePath resolves relative host paths in a volume mount spec.
// Docker Compose volume format: "host_path:container_path[:options]"
// Named volumes (no path separator) and absolute paths are left unchanged.
func resolveVolumePath(vol string, baseDir string) string {
	parts := strings.SplitN(vol, ":", 2)
	if len(parts) < 2 {
		return vol // named volume or bare path
	}
	hostPath := parts[0]
	// Skip named volumes (no slash or dot prefix) and absolute paths.
	if filepath.IsAbs(hostPath) || (!strings.HasPrefix(hostPath, ".") && !strings.Contains(hostPath, "/")) {
		return vol
	}
	// Resolve relative host path against scenario directory.
	absHost := filepath.Join(baseDir, hostPath)
	return absHost + ":" + strings.Join(parts[1:], ":")
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
