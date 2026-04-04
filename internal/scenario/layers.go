// Package scenario — layer loading, service merging, and matrix expansion.
package scenario

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResolveLayersAndMatrix resolves all layer references in a scenario's services
// and expands matrix variants. Returns one or more resolved scenarios ready
// for execution. If no matrix is defined, returns a single-element slice.
//
// layerDirs is the list of directories to search for layer files (in order).
// If empty, defaults to "layers/" relative to the scenario directory.
func ResolveLayersAndMatrix(s *Scenario, layerDirs []string) ([]*Scenario, error) {
	if len(layerDirs) == 0 {
		layerDirs = defaultLayerDirs(s.Dir)
	}

	// Expand matrix into combinations.
	combos := expandMatrix(s.Matrix)
	if len(combos) == 0 {
		// No matrix — resolve layers once.
		resolved, err := resolveLayers(s, nil, layerDirs)
		if err != nil {
			return nil, err
		}
		return []*Scenario{resolved}, nil
	}

	var scenarios []*Scenario
	for _, combo := range combos {
		resolved, err := resolveLayers(s, combo, layerDirs)
		if err != nil {
			return nil, fmt.Errorf("matrix %v: %w", combo, err)
		}
		// Append matrix values to the scenario name for uniqueness.
		suffix := matrixSuffix(combo)
		resolved.Name = s.Name + "-" + suffix
		scenarios = append(scenarios, resolved)
	}
	return scenarios, nil
}

// resolveLayers deep-copies the scenario and resolves all `from:` references
// in its services. If combo is non-nil, ${matrix.<key>} placeholders are
// replaced before resolving.
func resolveLayers(s *Scenario, combo map[string]string, layerDirs []string) (*Scenario, error) {
	// Deep copy the scenario so we don't mutate the original.
	resolved := shallowCopyScenario(s)

	// Resolve each service.
	resolvedServices := make(map[string]ComposeService, len(s.Services))
	for name, svc := range s.Services {
		from := svc.From
		if from != "" && combo != nil {
			from = substituteMatrix(from, combo)
		}

		if from == "" {
			resolvedServices[name] = svc
			continue
		}

		// Load the layer file.
		layer, layerDir, err := loadLayer(from, layerDirs)
		if err != nil {
			return nil, fmt.Errorf("service %q: %w", name, err)
		}

		// Merge: scenario service overrides layer defaults.
		merged := mergeService(*layer, svc, layerDir, s.Dir)
		resolvedServices[name] = merged
	}

	resolved.Services = resolvedServices
	return resolved, nil
}

// loadLayer finds and parses a layer YAML file. Returns the parsed service
// and the directory containing the layer file (for resolving relative paths).
func loadLayer(name string, layerDirs []string) (*ComposeService, string, error) {
	// Try each layer directory.
	for _, dir := range layerDirs {
		path := filepath.Join(dir, name+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			path = filepath.Join(dir, name+".yml")
			data, err = os.ReadFile(path)
			if err != nil {
				continue
			}
		}

		var svc ComposeService
		if err := yaml.Unmarshal(data, &svc); err != nil {
			return nil, "", fmt.Errorf("parsing layer %s: %w", path, err)
		}
		return &svc, filepath.Dir(path), nil
	}

	return nil, "", fmt.Errorf("layer %q not found in search paths: %v", name, layerDirs)
}

// mergeService merges an overlay on top of a base service. Overlay fields
// take precedence; maps are merged (overlay wins on key conflicts); slices
// are appended. Volume paths in the base are resolved relative to layerDir.
func mergeService(base, overlay ComposeService, layerDir, scenarioDir string) ComposeService {
	result := base

	// Clear the `from` field — it's been resolved.
	result.From = ""

	// Scalar overrides.
	if overlay.Image != "" {
		result.Image = overlay.Image
	}
	if overlay.Build != nil {
		result.Build = overlay.Build
	}
	if overlay.Command != nil {
		result.Command = overlay.Command
	}
	if overlay.Entrypoint != nil {
		result.Entrypoint = overlay.Entrypoint
	}
	if overlay.HealthCheck != nil {
		result.HealthCheck = overlay.HealthCheck
	}
	if overlay.Restart != "" {
		result.Restart = overlay.Restart
	}

	// Map merges (overlay wins on key conflicts).
	result.Environment = mergeMaps(base.Environment, overlay.Environment)
	result.Networks = mergeNetworks(base.Networks, overlay.Networks)

	// Slice appends.
	if len(overlay.Ports) > 0 {
		result.Ports = append(result.Ports, overlay.Ports...)
	}
	if len(overlay.CapAdd) > 0 {
		result.CapAdd = append(result.CapAdd, overlay.CapAdd...)
	}
	if len(overlay.SecurityOpt) > 0 {
		result.SecurityOpt = append(result.SecurityOpt, overlay.SecurityOpt...)
	}

	// Volumes: resolve base volumes relative to layerDir, then append overlay.
	resolvedBaseVols := make([]string, len(base.Volumes))
	for i, v := range base.Volumes {
		resolvedBaseVols[i] = resolveLayerVolume(v, layerDir)
	}
	result.Volumes = append(resolvedBaseVols, overlay.Volumes...)

	// DependsOn: overlay replaces if non-empty.
	if !overlay.DependsOn.IsEmpty() {
		result.DependsOn = overlay.DependsOn
	}

	return result
}

// resolveLayerVolume converts relative host paths in a volume mount to
// absolute paths relative to the layer file's directory.
func resolveLayerVolume(vol string, layerDir string) string {
	parts := strings.SplitN(vol, ":", 3)
	if len(parts) < 2 {
		return vol
	}
	hostPath := parts[0]
	if strings.HasPrefix(hostPath, "./") || strings.HasPrefix(hostPath, "../") {
		abs := filepath.Join(layerDir, hostPath)
		if !filepath.IsAbs(abs) {
			if resolved, err := filepath.Abs(abs); err == nil {
				abs = resolved
			}
		}
		parts[0] = abs
		return strings.Join(parts, ":")
	}
	return vol
}

// expandMatrix returns all combinations of matrix values.
// For {database: [postgres, mysql], cache: [redis, memcached]}, returns:
// [{database: postgres, cache: redis}, {database: postgres, cache: memcached},
//
//	{database: mysql, cache: redis}, {database: mysql, cache: memcached}]
func expandMatrix(matrix map[string][]string) []map[string]string {
	if len(matrix) == 0 {
		return nil
	}

	// Collect keys in deterministic order.
	keys := make([]string, 0, len(matrix))
	for k := range matrix {
		keys = append(keys, k)
	}

	// Cartesian product.
	var combos []map[string]string
	combos = append(combos, map[string]string{})

	for _, key := range keys {
		values := matrix[key]
		var expanded []map[string]string
		for _, combo := range combos {
			for _, val := range values {
				newCombo := make(map[string]string, len(combo)+1)
				for k, v := range combo {
					newCombo[k] = v
				}
				newCombo[key] = val
				expanded = append(expanded, newCombo)
			}
		}
		combos = expanded
	}

	return combos
}

// FilterMatrix filters matrix combinations to only include those matching
// the given filter (e.g. "database=postgres").
func FilterMatrix(combos []map[string]string, filters map[string]string) []map[string]string {
	if len(filters) == 0 {
		return combos
	}

	var filtered []map[string]string
	for _, combo := range combos {
		match := true
		for k, v := range filters {
			if combo[k] != v {
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, combo)
		}
	}
	return filtered
}

// substituteMatrix replaces ${matrix.<key>} placeholders in a string.
func substituteMatrix(s string, combo map[string]string) string {
	for key, val := range combo {
		s = strings.ReplaceAll(s, "${matrix."+key+"}", val)
	}
	return s
}

// matrixSuffix generates a short suffix from matrix values for scenario naming.
func matrixSuffix(combo map[string]string) string {
	parts := make([]string, 0, len(combo))
	for _, v := range combo {
		parts = append(parts, v)
	}
	return strings.Join(parts, "-")
}

// defaultLayerDirs returns the default layer search directories.
func defaultLayerDirs(scenarioDir string) []string {
	dirs := []string{
		filepath.Join(scenarioDir, "layers"),
	}
	// Also search parent directory's layers/ (for tests/scanners/layers/).
	parent := filepath.Dir(scenarioDir)
	if parent != scenarioDir {
		dirs = append(dirs, filepath.Join(parent, "layers"))
	}
	return dirs
}

// shallowCopyScenario creates a copy of a scenario with new slices/maps
// so mutations don't affect the original.
func shallowCopyScenario(s *Scenario) *Scenario {
	cp := *s

	// Deep copy services map.
	if s.Services != nil {
		cp.Services = make(map[string]ComposeService, len(s.Services))
		for k, v := range s.Services {
			cp.Services[k] = v
		}
	}

	// Deep copy networks.
	if s.Networks != nil {
		cp.Networks = make(map[string]ComposeNetwork, len(s.Networks))
		for k, v := range s.Networks {
			cp.Networks[k] = v
		}
	}

	// Deep copy env.
	if s.Env != nil {
		cp.Env = make(map[string]string, len(s.Env))
		for k, v := range s.Env {
			cp.Env[k] = v
		}
	}

	// Copy slices.
	if s.Run != nil {
		cp.Run = make([]string, len(s.Run))
		copy(cp.Run, s.Run)
	}
	if s.Assertions != nil {
		cp.Assertions = make([]Assertion, len(s.Assertions))
		copy(cp.Assertions, s.Assertions)
	}
	if s.Tags != nil {
		cp.Tags = make([]string, len(s.Tags))
		copy(cp.Tags, s.Tags)
	}

	return &cp
}

// mergeMaps merges two string maps. Overlay values win on conflict.
func mergeMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	result := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}

// mergeNetworks merges two network maps. Overlay wins on conflict.
func mergeNetworks(base, overlay map[string]ComposeServiceNetwork) map[string]ComposeServiceNetwork {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	result := make(map[string]ComposeServiceNetwork, len(base)+len(overlay))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overlay {
		result[k] = v
	}
	return result
}
