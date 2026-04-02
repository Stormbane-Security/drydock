package interpolate

import (
	"strings"
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

// ── Additional resolve tests ──────────────────────────────────────────────

func TestResolve_EmptyString(t *testing.T) {
	got, err := resolve("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestResolve_PartialMatch(t *testing.T) {
	outputs := map[string]string{
		"host": "db.example.com",
	}
	// One ref is present, one is missing.
	_, err := resolve("${fixture.host}:${fixture.port}", outputs)
	if err == nil {
		t.Fatal("expected error for partially unresolved string")
	}
	if !strings.Contains(err.Error(), "port") {
		t.Errorf("expected error to mention 'port', got: %v", err)
	}
}

func TestResolve_AdjacentRefs(t *testing.T) {
	outputs := map[string]string{
		"a": "hello",
		"b": "world",
	}
	got, err := resolve("${fixture.a}${fixture.b}", outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "helloworld" {
		t.Errorf("expected 'helloworld', got %q", got)
	}
}

func TestResolve_UnderscoresInKey(t *testing.T) {
	outputs := map[string]string{
		"my_long_key_name": "value123",
	}
	got, err := resolve("prefix_${fixture.my_long_key_name}_suffix", outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "prefix_value123_suffix" {
		t.Errorf("got %q, want %q", got, "prefix_value123_suffix")
	}
}

// ── Additional Scenario interpolation tests ───────────────────────────────

func TestScenario_InterpolatesCommandFields(t *testing.T) {
	outputs := map[string]string{
		"endpoint": "https://api.example.com",
		"dir":      "/opt/test",
	}

	s := &scenario.Scenario{
		Name: "test",
		Commands: []scenario.Command{
			{
				Name: "curl-test",
				Run:  "curl ${fixture.endpoint}/health",
				Dir:  "${fixture.dir}",
				Env:  map[string]string{"API": "${fixture.endpoint}"},
			},
		},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Commands[0].Run != "curl https://api.example.com/health" {
		t.Errorf("Run = %q", s.Commands[0].Run)
	}
	if s.Commands[0].Dir != "/opt/test" {
		t.Errorf("Dir = %q", s.Commands[0].Dir)
	}
	if s.Commands[0].Env["API"] != "https://api.example.com" {
		t.Errorf("Env[API] = %q", s.Commands[0].Env["API"])
	}
}

func TestScenario_InterpolatesSetupFields(t *testing.T) {
	outputs := map[string]string{
		"db_url": "postgres://localhost:5432/test",
	}

	s := &scenario.Scenario{
		Name: "test",
		Setup: []scenario.Command{
			{
				Name: "seed",
				Run:  "psql ${fixture.db_url} < seed.sql",
				Env:  map[string]string{"DATABASE_URL": "${fixture.db_url}"},
			},
		},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Setup[0].Run != "psql postgres://localhost:5432/test < seed.sql" {
		t.Errorf("Setup.Run = %q", s.Setup[0].Run)
	}
	if s.Setup[0].Env["DATABASE_URL"] != "postgres://localhost:5432/test" {
		t.Errorf("Setup.Env[DATABASE_URL] = %q", s.Setup[0].Env["DATABASE_URL"])
	}
}

func TestScenario_InterpolatesAssertionFields(t *testing.T) {
	outputs := map[string]string{
		"url":    "https://example.com",
		"secret": "expected-token-value",
	}

	s := &scenario.Scenario{
		Name: "test",
		Assertions: []scenario.Assertion{
			{
				Name:   "check",
				Type:   "http",
				Target: "${fixture.url}/status",
				Expect: scenario.AssertionExpect{
					Body:        "ok",
					HeaderValue: "${fixture.secret}",
				},
			},
		},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Assertions[0].Target != "https://example.com/status" {
		t.Errorf("Target = %q", s.Assertions[0].Target)
	}
	if s.Assertions[0].Expect.HeaderValue != "expected-token-value" {
		t.Errorf("HeaderValue = %q", s.Assertions[0].Expect.HeaderValue)
	}
}

func TestScenario_InterpolatesBackendTerraformVars(t *testing.T) {
	outputs := map[string]string{
		"project_id": "my-project-123",
	}

	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type:          "terraform",
			TerraformDir:  "./infra",
			TerraformVars: map[string]string{"project": "${fixture.project_id}"},
		},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Backend.TerraformVars["project"] != "my-project-123" {
		t.Errorf("TerraformVars[project] = %q", s.Backend.TerraformVars["project"])
	}
}

func TestScenario_InterpolatesEnv(t *testing.T) {
	outputs := map[string]string{
		"api_key": "sk-test-12345",
	}

	s := &scenario.Scenario{
		Name: "test",
		Env:  map[string]string{"API_KEY": "${fixture.api_key}"},
	}

	if err := Scenario(s, outputs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.Env["API_KEY"] != "sk-test-12345" {
		t.Errorf("Env[API_KEY] = %q", s.Env["API_KEY"])
	}
}

func TestScenario_ReportsAllMissingReferences(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type: "github-actions",
			Repo: "${fixture.repo}",
			Ref:  "${fixture.branch}",
		},
		Commands: []scenario.Command{
			{
				Name: "cmd",
				Run:  "${fixture.script}",
			},
		},
	}

	err := Scenario(s, map[string]string{})
	if err == nil {
		t.Fatal("expected error for multiple unresolved variables")
	}
	errMsg := err.Error()
	// All three should be reported.
	for _, key := range []string{"repo", "branch", "script"} {
		if !strings.Contains(errMsg, key) {
			t.Errorf("expected error to mention %q, got: %s", key, errMsg)
		}
	}
}

func TestScenario_NoFixtureRefsPassthrough(t *testing.T) {
	s := &scenario.Scenario{
		Name: "test",
		Backend: scenario.Backend{
			Type:     "compose",
			ComposeFile: "compose.yaml",
		},
		Commands: []scenario.Command{
			{Name: "test", Run: "echo hello world"},
		},
	}

	if err := Scenario(s, map[string]string{}); err != nil {
		t.Fatalf("expected no error for scenario without fixture refs, got: %v", err)
	}

	if s.Commands[0].Run != "echo hello world" {
		t.Errorf("expected passthrough, got %q", s.Commands[0].Run)
	}
}
