package scenario

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateComposeBytes_SingleService(t *testing.T) {
	services := map[string]ComposeService{
		"web": {Image: "nginx:alpine", Ports: []string{"8080:80"}},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing generated YAML: %v", err)
	}

	web, ok := parsed.Services["web"]
	if !ok {
		t.Fatal("expected service 'web' in output")
	}
	if web.Image != "nginx:alpine" {
		t.Errorf("expected image 'nginx:alpine', got %q", web.Image)
	}
	if len(web.Ports) != 1 || web.Ports[0] != "8080:80" {
		t.Errorf("expected ports ['8080:80'], got %v", web.Ports)
	}
}

func TestGenerateComposeBytes_MultipleServicesWithDependsOn(t *testing.T) {
	services := map[string]ComposeService{
		"web": {
			Image:     "nginx:alpine",
			Ports:     []string{"8080:80"},
			DependsOn: DependsOn{Entries: []DependsOnEntry{{Service: "db"}}},
		},
		"db": {
			Image: "postgres:16",
			Environment: map[string]string{
				"POSTGRES_PASSWORD": "secret",
			},
		},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing generated YAML: %v", err)
	}

	if len(parsed.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(parsed.Services))
	}

	web := parsed.Services["web"]
	if len(web.DependsOn.Entries) != 1 || web.DependsOn.Entries[0].Service != "db" {
		t.Errorf("expected depends_on ['db'], got %v", web.DependsOn.Entries)
	}

	db := parsed.Services["db"]
	if db.Environment["POSTGRES_PASSWORD"] != "secret" {
		t.Errorf("expected POSTGRES_PASSWORD=secret, got %q", db.Environment["POSTGRES_PASSWORD"])
	}
}

func TestGenerateComposeBytes_AllFields(t *testing.T) {
	services := map[string]ComposeService{
		"app": {
			Image: "myapp:latest",
			Build: &ComposeBuild{
				Context:    "./app",
				Dockerfile: "Dockerfile.dev",
				Args:       map[string]string{"VERSION": "1.0"},
			},
			Ports:       []string{"3000:3000", "3001:3001"},
			Environment: map[string]string{"NODE_ENV": "test"},
			Volumes:     []string{"./data:/data"},
			DependsOn:   DependsOn{Entries: []DependsOnEntry{{Service: "db"}, {Service: "cache"}}},
			Command:     "npm start",
			Entrypoint:  "/entrypoint.sh",
			HealthCheck: &ComposeHealth{
				Test:     []string{"CMD", "curl", "-f", "http://localhost:3000/health"},
				Interval: "10s",
				Timeout:  "5s",
				Retries:  3,
				Start:    "30s",
			},
			CapAdd:      []string{"NET_ADMIN", "SYS_PTRACE"},
			SecurityOpt: []string{"seccomp:unconfined"},
			Networks:    map[string]ComposeServiceNetwork{"frontend": {}},
			Restart:     "unless-stopped",
		},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing generated YAML: %v", err)
	}

	app := parsed.Services["app"]
	if app.Image != "myapp:latest" {
		t.Errorf("image: got %q", app.Image)
	}
	if app.Build == nil {
		t.Fatal("expected build to be set")
	}
	if app.Build.Context != "./app" {
		t.Errorf("build.context: got %q", app.Build.Context)
	}
	if app.Build.Dockerfile != "Dockerfile.dev" {
		t.Errorf("build.dockerfile: got %q", app.Build.Dockerfile)
	}
	if app.Build.Args["VERSION"] != "1.0" {
		t.Errorf("build.args.VERSION: got %q", app.Build.Args["VERSION"])
	}
	if len(app.Ports) != 2 {
		t.Errorf("expected 2 ports, got %d", len(app.Ports))
	}
	if app.Environment["NODE_ENV"] != "test" {
		t.Errorf("environment.NODE_ENV: got %q", app.Environment["NODE_ENV"])
	}
	if len(app.Volumes) != 1 || app.Volumes[0] != "./data:/data" {
		t.Errorf("volumes: got %v", app.Volumes)
	}
	if len(app.DependsOn.Entries) != 2 {
		t.Errorf("expected 2 depends_on, got %d", len(app.DependsOn.Entries))
	}
	if app.Restart != "unless-stopped" {
		t.Errorf("restart: got %q", app.Restart)
	}
	if app.HealthCheck == nil {
		t.Fatal("expected healthcheck to be set")
	}
	if app.HealthCheck.Retries != 3 {
		t.Errorf("healthcheck.retries: got %d", app.HealthCheck.Retries)
	}
	if app.HealthCheck.Interval != "10s" {
		t.Errorf("healthcheck.interval: got %q", app.HealthCheck.Interval)
	}
	if len(app.CapAdd) != 2 {
		t.Errorf("expected 2 cap_add, got %d", len(app.CapAdd))
	}
	if len(app.SecurityOpt) != 1 || app.SecurityOpt[0] != "seccomp:unconfined" {
		t.Errorf("security_opt: got %v", app.SecurityOpt)
	}
}

func TestGenerateComposeBytes_EmptyServices(t *testing.T) {
	_, err := GenerateComposeBytes(map[string]ComposeService{})
	if err == nil {
		t.Fatal("expected error for empty services")
	}
	if !strings.Contains(err.Error(), "no services defined") {
		t.Errorf("expected 'no services defined' error, got: %v", err)
	}
}

func TestGenerateComposeBytes_NilServices(t *testing.T) {
	_, err := GenerateComposeBytes(nil)
	if err == nil {
		t.Fatal("expected error for nil services")
	}
}

func TestGenerateComposeFile_CreatesFile(t *testing.T) {
	services := map[string]ComposeService{
		"web": {Image: "nginx:alpine"},
	}

	path, _, err := GenerateComposeFile(services, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("GenerateComposeFile: %v", err)
	}
	defer os.RemoveAll(strings.TrimSuffix(path, "/compose.yaml"))

	// Verify file exists on disk.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file to exist at %s: %v", path, err)
	}
	if info.IsDir() {
		t.Error("expected a file, not a directory")
	}
	if info.Size() == 0 {
		t.Error("expected non-empty file")
	}

	// Verify content is valid YAML.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing generated file: %v", err)
	}
	if parsed.Services["web"].Image != "nginx:alpine" {
		t.Errorf("expected image 'nginx:alpine' in file, got %q", parsed.Services["web"].Image)
	}

	// Verify filename is compose.yaml.
	if !strings.HasSuffix(path, "/compose.yaml") {
		t.Errorf("expected path ending with /compose.yaml, got %q", path)
	}
}

func TestGenerateComposeFile_EmptyServices(t *testing.T) {
	_, _, err := GenerateComposeFile(map[string]ComposeService{}, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for empty services")
	}
}

func TestGenerateComposeBytes_RoundTrip(t *testing.T) {
	// Generate bytes, parse them back, and verify the structure is intact.
	services := map[string]ComposeService{
		"api": {
			Image: "api:v2",
			Ports: []string{"9090:9090"},
			Environment: map[string]string{
				"DB_HOST": "db",
				"DB_PORT": "5432",
			},
		},
		"db": {
			Image: "postgres:16",
			Volumes: []string{"pgdata:/var/lib/postgresql/data"},
		},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("round-trip parse: %v", err)
	}

	if len(parsed.Services) != 2 {
		t.Errorf("expected 2 services after round-trip, got %d", len(parsed.Services))
	}

	api := parsed.Services["api"]
	if api.Image != "api:v2" {
		t.Errorf("api image after round-trip: %q", api.Image)
	}
	if api.Environment["DB_HOST"] != "db" {
		t.Errorf("api env DB_HOST after round-trip: %q", api.Environment["DB_HOST"])
	}

	db := parsed.Services["db"]
	if len(db.Volumes) != 1 {
		t.Errorf("db volumes after round-trip: %v", db.Volumes)
	}
}

func TestGenerateComposeBytes_CommandSlice(t *testing.T) {
	services := map[string]ComposeService{
		"app": {
			Image:   "node:20",
			Command: []any{"node", "server.js", "--port", "3000"},
		},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	// Just verify it marshals without error and produces valid YAML.
	var parsed composeFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing generated YAML: %v", err)
	}
	if parsed.Services["app"].Image != "node:20" {
		t.Errorf("expected image 'node:20', got %q", parsed.Services["app"].Image)
	}
}

func TestDependsOn_MapForm(t *testing.T) {
	yamlInput := `
services:
  web:
    image: nginx:alpine
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_started
  db:
    image: postgres:16
  cache:
    image: redis:7
`
	var s struct {
		Services map[string]ComposeService `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(yamlInput), &s); err != nil {
		t.Fatalf("unmarshal map-form depends_on: %v", err)
	}

	web := s.Services["web"]
	if len(web.DependsOn.Entries) != 2 {
		t.Fatalf("expected 2 depends_on entries, got %d", len(web.DependsOn.Entries))
	}

	// Verify entries contain the right services and conditions.
	found := make(map[string]string)
	for _, e := range web.DependsOn.Entries {
		found[e.Service] = e.Condition
	}
	if found["db"] != "service_healthy" {
		t.Errorf("expected db condition 'service_healthy', got %q", found["db"])
	}
	if found["cache"] != "service_started" {
		t.Errorf("expected cache condition 'service_started', got %q", found["cache"])
	}

	// Round-trip: marshal and verify it produces map form (since conditions are present).
	data, err := GenerateComposeBytes(s.Services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "condition:") {
		t.Errorf("expected map-form depends_on with conditions in output, got:\n%s", content)
	}
}

func TestDependsOn_ListForm(t *testing.T) {
	yamlInput := `
services:
  web:
    image: nginx:alpine
    depends_on: [db, cache]
  db:
    image: postgres:16
  cache:
    image: redis:7
`
	var s struct {
		Services map[string]ComposeService `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(yamlInput), &s); err != nil {
		t.Fatalf("unmarshal list-form depends_on: %v", err)
	}

	web := s.Services["web"]
	names := web.DependsOn.ServiceNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 service names, got %d", len(names))
	}

	// Round-trip: marshal and verify it produces list form (no conditions).
	data, err := GenerateComposeBytes(s.Services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "condition:") {
		t.Errorf("expected list-form depends_on without conditions, got:\n%s", content)
	}
}

func TestGenerateComposeFile_EphemeralPortsByDefault(t *testing.T) {
	services := map[string]ComposeService{
		"web": {Image: "nginx:alpine", Ports: []string{"8080:80"}},
		"db":  {Image: "postgres:16", Ports: []string{"5432:5432"}},
	}

	path, plan, err := GenerateComposeFile(services, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("GenerateComposeFile: %v", err)
	}
	defer os.RemoveAll(strings.TrimSuffix(path, "/compose.yaml"))

	// Port plan should record intended ports.
	if len(plan.Mappings) != 2 {
		t.Fatalf("expected 2 port mappings, got %d", len(plan.Mappings))
	}

	// Generated file should have ephemeral (0:) ports.
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "0:80") {
		t.Errorf("expected ephemeral port 0:80 in output, got:\n%s", content)
	}
	if strings.Contains(content, "8080:80") {
		t.Errorf("expected host port 8080 to be rewritten to ephemeral, got:\n%s", content)
	}
}

func TestGenerateComposeFile_FixedPorts(t *testing.T) {
	services := map[string]ComposeService{
		"web": {Image: "nginx:alpine", Ports: []string{"8080:80"}},
		"db":  {Image: "postgres:16", Ports: []string{"5432:5432"}},
	}

	path, plan, err := GenerateComposeFile(services, t.TempDir(), nil, true)
	if err != nil {
		t.Fatalf("GenerateComposeFile: %v", err)
	}
	defer os.RemoveAll(strings.TrimSuffix(path, "/compose.yaml"))

	// Port plan should still record intended ports.
	if len(plan.Mappings) != 2 {
		t.Fatalf("expected 2 port mappings, got %d", len(plan.Mappings))
	}

	// Generated file should preserve original ports.
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "8080:80") {
		t.Errorf("expected fixed port 8080:80 in output, got:\n%s", content)
	}
	if !strings.Contains(content, "5432:5432") {
		t.Errorf("expected fixed port 5432:5432 in output, got:\n%s", content)
	}
	if strings.Contains(content, "- 0:80") || strings.Contains(content, "- \"0:80\"") {
		t.Errorf("expected no ephemeral ports when fixed_ports=true, got:\n%s", content)
	}
}

func TestGenerateComposeBytes_OmitsEmptyFields(t *testing.T) {
	services := map[string]ComposeService{
		"minimal": {Image: "alpine"},
	}

	data, err := GenerateComposeBytes(services)
	if err != nil {
		t.Fatalf("GenerateComposeBytes: %v", err)
	}

	content := string(data)
	// These fields should be omitted since they are empty.
	for _, field := range []string{"ports:", "volumes:", "depends_on:", "healthcheck:", "cap_add:", "security_opt:"} {
		if strings.Contains(content, field) {
			t.Errorf("expected %q to be omitted for minimal service, but found it in:\n%s", field, content)
		}
	}
}
