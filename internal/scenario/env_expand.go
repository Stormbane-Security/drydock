package scenario

import (
	"os"
	"strings"
)

// ExpandScenarioEnv substitutes $VAR / ${VAR} in selected scenario strings using the
// process environment. When afterFixture is false, strings that still contain
// ${fixture.…} are left unchanged so fixture interpolation can run later.
func ExpandScenarioEnv(s *Scenario, afterFixture bool) {
	expand := func(val string) string {
		if !afterFixture && strings.Contains(val, "${fixture.") {
			return val
		}
		return os.ExpandEnv(val)
	}

	// Backend (type is fixed; do not expand)
	s.Backend.ComposeFile = expand(s.Backend.ComposeFile)
	s.Backend.TerraformDir = expand(s.Backend.TerraformDir)
	s.Backend.Workspace = expand(s.Backend.Workspace)
	s.Backend.Repo = expand(s.Backend.Repo)
	s.Backend.Workflow = expand(s.Backend.Workflow)
	s.Backend.Ref = expand(s.Backend.Ref)
	s.Backend.Trigger = expand(s.Backend.Trigger)
	expandMap(s.Backend.Inputs, expand)
	expandMap(s.Backend.TerraformVars, expand)
	expandMap(s.Backend.TerraformBackend, expand)

	if s.Fixture != nil {
		expandMap(s.Fixture.Vars, expand)
	}

	expandMap(s.Env, expand)

	if s.Ready != nil {
		s.Ready.Cmd = expand(s.Ready.Cmd)
	}

	for i := range s.Run {
		s.Run[i] = expand(s.Run[i])
	}

	for i := range s.Setup {
		s.Setup[i].Name = expand(s.Setup[i].Name)
		s.Setup[i].Run = expand(s.Setup[i].Run)
		s.Setup[i].Dir = expand(s.Setup[i].Dir)
		expandMap(s.Setup[i].Env, expand)
	}

	for i := range s.Commands {
		s.Commands[i].Name = expand(s.Commands[i].Name)
		s.Commands[i].Run = expand(s.Commands[i].Run)
		s.Commands[i].Dir = expand(s.Commands[i].Dir)
		expandMap(s.Commands[i].Env, expand)
	}

	for i := range s.PostExploit {
		s.PostExploit[i].Name = expand(s.PostExploit[i].Name)
		s.PostExploit[i].Run = expand(s.PostExploit[i].Run)
		s.PostExploit[i].Dir = expand(s.PostExploit[i].Dir)
		expandMap(s.PostExploit[i].Env, expand)
	}

	for i := range s.Assertions {
		a := &s.Assertions[i]
		a.Name = expand(a.Name)
		a.Target = expand(a.Target)
		a.File = expand(a.File)
		for j := range a.Args {
			a.Args[j] = expand(a.Args[j])
		}
		e := &a.Expect
		e.Body = expand(e.Body)
		e.NotBody = expand(e.NotBody)
		e.HeaderValue = expand(e.HeaderValue)
		e.Command = expand(e.Command)
		e.Stdout = expand(e.Stdout)
		e.NotStdout = expand(e.NotStdout)
		e.OutputValue = expand(e.OutputValue)
		e.OutputMatch = expand(e.OutputMatch)
		e.Contains = expand(e.Contains)
		e.CheckID = expand(e.CheckID)
		e.NotCheckID = expand(e.NotCheckID)
		e.Severity = expand(e.Severity)
		e.EvidenceKey = expand(e.EvidenceKey)
		e.EvidenceValue = expand(e.EvidenceValue)
		e.EvidenceContains = expand(e.EvidenceContains)
		e.Job = expand(e.Job)
		e.StepName = expand(e.StepName)
		e.ArtifactName = expand(e.ArtifactName)
		e.Output = expand(e.Output)
		e.GhcollectAnalyzers = expand(e.GhcollectAnalyzers)
		e.ProxyType = expand(e.ProxyType)
		e.FrameworkField = expand(e.FrameworkField)
		e.CloudProviderField = expand(e.CloudProviderField)
		e.AuthSystemField = expand(e.AuthSystemField)
		e.InfraLayerField = expand(e.InfraLayerField)
		e.BackendService = expand(e.BackendService)
		e.ServiceVersion = expand(e.ServiceVersion)
		e.ServiceVersionContains = expand(e.ServiceVersionContains)
		e.CookieName = expand(e.CookieName)
		e.PathResponds = expand(e.PathResponds)
		e.MatchedPlaybook = expand(e.MatchedPlaybook)
		e.TitleContains = expand(e.TitleContains)
		e.NotProxyType = expand(e.NotProxyType)
		e.NotFramework = expand(e.NotFramework)
		for j := range a.Expectations {
			ex := &a.Expectations[j]
			ex.Body = expand(ex.Body)
			ex.CheckID = expand(ex.CheckID)
			ex.Stdout = expand(ex.Stdout)
			ex.GhcollectAnalyzers = expand(ex.GhcollectAnalyzers)
		}
	}
}

func expandMap(m map[string]string, expand func(string) string) {
	for k, v := range m {
		m[k] = expand(v)
	}
}

// NeedsDocker reports whether this scenario starts Docker Compose services.
func (s *Scenario) NeedsDocker() bool {
	if s.IsUnifiedFormat() {
		return true
	}
	switch s.Backend.Type {
	case "compose":
		return s.Backend.ComposeFile != ""
	case "hybrid":
		return s.Backend.ComposeFile != ""
	default:
		return false
	}
}

// ExpandEnvBeforeValidate runs environment expansion appropriate for static validation.
// When a fixture is present, ${fixture.…} placeholders are preserved.
func ExpandEnvBeforeValidate(s *Scenario) {
	ExpandScenarioEnv(s, s.Fixture == nil)
}
