// Package interpolate performs ${fixture.<key>} variable substitution
// on scenario fields after a fixture's Terraform outputs are collected.
package interpolate

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/stormbane-security/drydock/internal/scenario"
)

var pattern = regexp.MustCompile(`\$\{fixture\.([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// Scenario performs in-place substitution of ${fixture.<key>} references
// across all interpolatable fields of the scenario. Returns an error if
// any reference has no matching key in outputs.
func Scenario(s *scenario.Scenario, outputs map[string]string) error {
	var errs []string
	r := func(val string) string {
		result, err := resolve(val, outputs)
		if err != nil {
			errs = append(errs, err.Error())
		}
		return result
	}

	// Backend fields.
	s.Backend.Repo = r(s.Backend.Repo)
	s.Backend.Workflow = r(s.Backend.Workflow)
	s.Backend.Ref = r(s.Backend.Ref)
	s.Backend.ComposeFile = r(s.Backend.ComposeFile)
	s.Backend.TerraformDir = r(s.Backend.TerraformDir)
	resolveMap(s.Backend.Inputs, &errs, outputs)
	resolveMap(s.Backend.TerraformVars, &errs, outputs)

	// Scenario-level env.
	resolveMap(s.Env, &errs, outputs)

	// Setup and command fields.
	for i := range s.Setup {
		s.Setup[i].Run = r(s.Setup[i].Run)
		s.Setup[i].Dir = r(s.Setup[i].Dir)
		resolveMap(s.Setup[i].Env, &errs, outputs)
	}
	for i := range s.Commands {
		s.Commands[i].Run = r(s.Commands[i].Run)
		s.Commands[i].Dir = r(s.Commands[i].Dir)
		resolveMap(s.Commands[i].Env, &errs, outputs)
	}

	// Assertion fields.
	for i := range s.Assertions {
		s.Assertions[i].Target = r(s.Assertions[i].Target)
		e := &s.Assertions[i].Expect
		e.Body = r(e.Body)
		e.NotBody = r(e.NotBody)
		e.HeaderValue = r(e.HeaderValue)
		e.Command = r(e.Command)
		e.Stdout = r(e.Stdout)
		e.NotStdout = r(e.NotStdout)
		e.OutputValue = r(e.OutputValue)
		e.OutputMatch = r(e.OutputMatch)
		e.Contains = r(e.Contains)
	}

	if len(errs) > 0 {
		return fmt.Errorf("unresolved fixture variables: %s", strings.Join(errs, "; "))
	}
	return nil
}

func resolve(s string, outputs map[string]string) (string, error) {
	if !strings.Contains(s, "${fixture.") {
		return s, nil
	}
	var missing []string
	result := pattern.ReplaceAllStringFunc(s, func(match string) string {
		key := pattern.FindStringSubmatch(match)[1]
		val, ok := outputs[key]
		if !ok {
			missing = append(missing, key)
			return match
		}
		return val
	})
	if len(missing) > 0 {
		return result, fmt.Errorf("${fixture.%s}", strings.Join(missing, "}, ${fixture."))
	}
	return result, nil
}

func resolveMap(m map[string]string, errs *[]string, outputs map[string]string) {
	for k, v := range m {
		result, err := resolve(v, outputs)
		if err != nil {
			*errs = append(*errs, err.Error())
		}
		m[k] = result
	}
}
