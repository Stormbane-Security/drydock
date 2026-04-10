package scenario

import (
	"os"
	"testing"
)

func TestExpandScenarioEnv_PreservesFixtureRefsWhenPartial(t *testing.T) {
	s := &Scenario{
		Backend: Backend{
			Repo: "${fixture.repo}",
			Ref:  "${DRYDOCK_REF}",
		},
	}
	_ = os.Setenv("DRYDOCK_REF", "main")
	t.Cleanup(func() { _ = os.Unsetenv("DRYDOCK_REF") })

	ExpandScenarioEnv(s, false)
	if s.Backend.Repo != "${fixture.repo}" {
		t.Fatalf("repo: got %q want ${fixture.repo}", s.Backend.Repo)
	}
	if s.Backend.Ref != "main" {
		t.Fatalf("ref: got %q want main", s.Backend.Ref)
	}
}

func TestExpandScenarioEnv_FullAfterFixture(t *testing.T) {
	s := &Scenario{
		Backend: Backend{
			Repo: "acme/lab",
			Ref:  "${DRYDOCK_REF}",
		},
	}
	_ = os.Setenv("DRYDOCK_REF", "develop")
	t.Cleanup(func() { _ = os.Unsetenv("DRYDOCK_REF") })

	ExpandScenarioEnv(s, true)
	if s.Backend.Ref != "develop" {
		t.Fatalf("ref: got %q", s.Backend.Ref)
	}
}

func TestNeedsDocker(t *testing.T) {
	ga := &Scenario{Backend: Backend{Type: "github-actions", Repo: "a/b", Workflow: "w.yml"}}
	if ga.NeedsDocker() {
		t.Error("github-actions should not need docker")
	}
	hy := &Scenario{Backend: Backend{Type: "hybrid", ComposeFile: "c.yaml", Repo: "a/b", Workflow: "w.yml"}}
	if !hy.NeedsDocker() {
		t.Error("hybrid with compose should need docker")
	}
	hy2 := &Scenario{Backend: Backend{Type: "hybrid", TerraformDir: "./tf", Repo: "a/b", Workflow: "w.yml"}}
	if hy2.NeedsDocker() {
		t.Error("hybrid without compose should not need docker")
	}
}
