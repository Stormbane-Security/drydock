package interpolate

import (
	"testing"

	"github.com/stormbane-security/drydock/internal/scenario"
)

func TestResolve_BasicSubstitution(t *testing.T) {
	outputs := map[string]string{
		"gar_repo": "us-central1-docker.pkg.dev/proj/repo",
	}
	got, err := resolve("${fixture.gar_repo}/test-image", outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "us-central1-docker.pkg.dev/proj/repo/test-image"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_MultipleRefs(t *testing.T) {
	outputs := map[string]string{
		"project": "my-proj",
		"region":  "us-central1",
	}
	got, err := resolve("${fixture.region}-docker.pkg.dev/${fixture.project}/repo", outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "us-central1-docker.pkg.dev/my-proj/repo"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolve_NoRefs(t *testing.T) {
	got, err := resolve("plain string", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain string" {
		t.Errorf("got %q, want %q", got, "plain string")
	}
}

func TestResolve_UnresolvedError(t *testing.T) {
	outputs := map[string]string{}
	_, err := resolve("${fixture.missing_key}", outputs)
	if err == nil {
		t.Fatal("expected error for unresolved reference")
	}
}

func TestResolve_NonFixtureRefUntouched(t *testing.T) {
	outputs := map[string]string{}
	got, err := resolve("${env.HOME}/path", outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "${env.HOME}/path" {
		t.Errorf("got %q, want %q", got, "${env.HOME}/path")
	}
}

func TestScenario_InterpolatesAllFields(t *testing.T) {
	outputs := map[string]string{
		"repo":     "test-org/test-repo",
		"sa_email": "sa@proj.iam.gserviceaccount.com",
		"gar_repo": "us-central1-docker.pkg.dev/proj/ci-test",
	}

	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type:     "github-actions",
			Repo:     "${fixture.repo}",
			Workflow: "docker.yml",
			Inputs: map[string]string{
				"gcp-service-account": "${fixture.sa_email}",
				"image":              "${fixture.gar_repo}/img",
			},
		},
		Assertions: []scenario.Assertion{
			{
				Name: "check image",
				Type: "command",
				Expect: scenario.AssertionExpect{
					Command: "gcloud artifacts docker images list ${fixture.gar_repo}",
					Stdout:  "img",
				},
			},
		},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Backend.Repo != "test-org/test-repo" {
		t.Errorf("Backend.Repo = %q, want %q", s.Backend.Repo, "test-org/test-repo")
	}
	if s.Backend.Inputs["gcp-service-account"] != "sa@proj.iam.gserviceaccount.com" {
		t.Errorf("Backend.Inputs[gcp-service-account] = %q", s.Backend.Inputs["gcp-service-account"])
	}
	if s.Backend.Inputs["image"] != "us-central1-docker.pkg.dev/proj/ci-test/img" {
		t.Errorf("Backend.Inputs[image] = %q", s.Backend.Inputs["image"])
	}
	if s.Assertions[0].Expect.Command != "gcloud artifacts docker images list us-central1-docker.pkg.dev/proj/ci-test" {
		t.Errorf("Assertion command = %q", s.Assertions[0].Expect.Command)
	}
}

func TestScenario_ErrorOnMissing(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type: "github-actions",
			Repo: "${fixture.nonexistent}",
		},
	}

	err := Scenario(s, map[string]string{})
	if err == nil {
		t.Fatal("expected error for unresolved fixture variable")
	}
}
