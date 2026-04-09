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
	Networks map[string]ComposeNetwork    `yaml:"networks,omitempty"`
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
	DependsOn   DependsOn         `yaml:"depends_on,omitempty"`
	Command     any               `yaml:"command,omitempty"`
	Entrypoint  any               `yaml:"entrypoint,omitempty"`
	HealthCheck *ComposeHealth    `yaml:"healthcheck,omitempty"`
	CapAdd      []string                          `yaml:"cap_add,omitempty"`
	SecurityOpt []string                         `yaml:"security_opt,omitempty"`
	Privileged  bool                              `yaml:"privileged,omitempty"`
	Networks    ServiceNetworks                    `yaml:"networks,omitempty"`
	Restart     string                            `yaml:"restart,omitempty"`
	Expose      []string                          `yaml:"expose,omitempty"`
}

// PortMapping records the intended host port for a container port on a service.
type PortMapping struct {
	Service       string
	IntendedHost  string // Original host port from YAML (e.g. "2379")
	ContainerPort string // Container port (e.g. "2379")
}

// PortPlan holds all port mappings from the original YAML before ephemeral
// port rewriting. After compose up, these are used to query actual ports
// and substitute throughout ready checks, commands, and assertions.
type PortPlan struct {
	Mappings []PortMapping
}

// parsePortSpec splits a Docker Compose port string like "8080:80",
// "8080:80/udp", or "8080" into (hostPort, containerPort). If only one part,
// both are the same. The protocol suffix (/tcp, /udp) is preserved on
// containerPort but stripped from hostPort (host ports are just numbers).
func parsePortSpec(spec string) (host, container string) {
	// Handle quoted specs
	spec = strings.Trim(spec, `"'`)
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	// Single-port spec like "53/udp" — strip protocol from host side.
	container = parts[0]
	host = strings.TrimSuffix(strings.TrimSuffix(container, "/udp"), "/tcp")
	return host, container
}

// GenerateComposeFile marshals the services map into a valid Docker Compose v3
// YAML file, writes it to a temp directory, and returns the file path and
// port plan. When fixedPorts is false (default), host ports are rewritten to
// ephemeral (0) so Docker assigns random ports — this forces tests to prove
// service identification by protocol fingerprinting. When fixedPorts is true,
// the original host ports from the YAML are preserved as-is.
// Volume paths starting with "./" are resolved to absolute paths relative to baseDir.
// The caller is responsible for removing the temp directory when done.
func GenerateComposeFile(services map[string]ComposeService, baseDir string, networks map[string]ComposeNetwork, fixedPorts ...bool) (string, *PortPlan, error) {
	if len(services) == 0 {
		return "", nil, fmt.Errorf("no services defined")
	}

	plan := &PortPlan{}
	out := composeFile{
		Services: make(map[string]composeServiceOut, len(services)),
		Networks: networks,
	}

	useFixed := len(fixedPorts) > 0 && fixedPorts[0]

	for name, svc := range services {
		// Build port mappings. When useFixed is false (default), rewrite
		// host ports to 0 so Docker assigns random ports. When useFixed is
		// true, preserve the original host ports from the YAML.
		outputPorts := make([]string, len(svc.Ports))
		for i, p := range svc.Ports {
			host, container := parsePortSpec(p)
			plan.Mappings = append(plan.Mappings, PortMapping{
				Service:       name,
				IntendedHost:  host,
				ContainerPort: container,
			})
			if useFixed {
				outputPorts[i] = host + ":" + container
			} else {
				outputPorts[i] = "0:" + container
			}
		}

		// Resolve relative volume paths to absolute paths.
		resolvedVolumes := make([]string, len(svc.Volumes))
		for i, v := range svc.Volumes {
			resolvedVolumes[i] = resolveVolumePath(v, baseDir)
		}

		// Resolve relative build context paths.
		var resolvedBuild *ComposeBuild
		if svc.Build != nil {
			resolvedBuild = &ComposeBuild{
				Context:    resolveRelativePath(svc.Build.Context, baseDir),
				Dockerfile: svc.Build.Dockerfile,
				Args:       svc.Build.Args,
			}
		}

		out.Services[name] = composeServiceOut{
			Image:       svc.Image,
			Build:       resolvedBuild,
			Ports:       outputPorts,
			Environment: svc.Environment,
			Volumes:     resolvedVolumes,
			DependsOn:   svc.DependsOn,
			Command:     svc.Command,
			Entrypoint:  svc.Entrypoint,
			HealthCheck: svc.HealthCheck,
			CapAdd:      svc.CapAdd,
			SecurityOpt: svc.SecurityOpt,
			Privileged:  svc.Privileged,
			Networks:    svc.Networks,
			Restart:     svc.Restart,
			Expose:      svc.Expose,
		}
	}

	data, err := yaml.Marshal(&out)
	if err != nil {
		return "", nil, fmt.Errorf("marshaling compose file: %w", err)
	}

	dir, err := os.MkdirTemp("", "drydock-compose-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	path := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("writing compose file: %w", err)
	}

	return path, plan, nil
}

// resolveVolumePath converts relative host paths in volume mounts to absolute
// paths relative to baseDir. Format: "HOST:CONTAINER[:OPTIONS]".
func resolveVolumePath(vol string, baseDir string) string {
	if baseDir == "" {
		return vol
	}
	parts := strings.SplitN(vol, ":", 3)
	if len(parts) < 2 {
		return vol
	}
	hostPath := parts[0]
	hostPath = resolveRelativePath(hostPath, baseDir)
	parts[0] = hostPath
	return strings.Join(parts, ":")
}

// resolveRelativePath converts a relative path to absolute relative to baseDir.
func resolveRelativePath(p string, baseDir string) string {
	if baseDir == "" || filepath.IsAbs(p) {
		return p
	}
	if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || (!strings.HasPrefix(p, "/") && !strings.Contains(p, ":")) {
		joined := filepath.Join(baseDir, p)
		// Ensure the result is absolute — if baseDir was relative, resolve from cwd.
		if !filepath.IsAbs(joined) {
			if abs, err := filepath.Abs(joined); err == nil {
				return abs
			}
		}
		return joined
	}
	return p
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
			Privileged:  svc.Privileged,
			Networks:    svc.Networks,
			Restart:     svc.Restart,
			Expose:      svc.Expose,
		}
	}

	return yaml.Marshal(&out)
}
