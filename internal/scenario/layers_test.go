package scenario

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestExpandMatrix_Empty(t *testing.T) {
	combos := expandMatrix(nil)
	if combos != nil {
		t.Fatalf("expected nil, got %v", combos)
	}
}

func TestExpandMatrix_Single(t *testing.T) {
	combos := expandMatrix(map[string][]string{
		"database": {"postgres", "mysql"},
	})
	if len(combos) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(combos))
	}
	values := []string{combos[0]["database"], combos[1]["database"]}
	sort.Strings(values)
	if values[0] != "mysql" || values[1] != "postgres" {
		t.Fatalf("unexpected values: %v", values)
	}
}

func TestExpandMatrix_Cartesian(t *testing.T) {
	combos := expandMatrix(map[string][]string{
		"database": {"postgres", "mysql"},
		"cache":    {"redis", "memcached"},
	})
	if len(combos) != 4 {
		t.Fatalf("expected 4 combos, got %d", len(combos))
	}
	// Verify all combinations exist.
	seen := make(map[string]bool)
	for _, c := range combos {
		key := c["database"] + "+" + c["cache"]
		seen[key] = true
	}
	expected := []string{"postgres+redis", "postgres+memcached", "mysql+redis", "mysql+memcached"}
	for _, e := range expected {
		if !seen[e] {
			t.Errorf("missing combination: %s", e)
		}
	}
}

func TestFilterMatrix(t *testing.T) {
	combos := expandMatrix(map[string][]string{
		"database": {"postgres", "mysql"},
		"cache":    {"redis", "memcached"},
	})

	filtered := FilterMatrix(combos, map[string]string{"database": "postgres"})
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered combos, got %d", len(filtered))
	}
	for _, c := range filtered {
		if c["database"] != "postgres" {
			t.Errorf("expected database=postgres, got %s", c["database"])
		}
	}

	filtered = FilterMatrix(combos, map[string]string{"database": "postgres", "cache": "redis"})
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered combo, got %d", len(filtered))
	}
}

func TestFilterMatrix_NoFilter(t *testing.T) {
	combos := expandMatrix(map[string][]string{"db": {"pg", "my"}})
	filtered := FilterMatrix(combos, nil)
	if len(filtered) != len(combos) {
		t.Fatalf("expected %d, got %d", len(combos), len(filtered))
	}
}

func TestSubstituteMatrix(t *testing.T) {
	result := substituteMatrix("databases/${matrix.database}", map[string]string{
		"database": "postgres",
	})
	if result != "databases/postgres" {
		t.Fatalf("expected databases/postgres, got %s", result)
	}
}

func TestLoadLayer(t *testing.T) {
	// Create a temp layer directory with a layer file.
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers", "databases")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}

	layerYAML := `image: postgres:16
environment:
  POSTGRES_DB: testdb
  POSTGRES_USER: testuser
  POSTGRES_PASSWORD: testpass
volumes:
  - ./init.sql:/docker-entrypoint-initdb.d/init.sql
`
	if err := os.WriteFile(filepath.Join(layerDir, "postgres.yaml"), []byte(layerYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	svc, loadedDir, err := loadLayer("databases/postgres", []string{filepath.Join(dir, "layers")})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Image != "postgres:16" {
		t.Errorf("expected postgres:16, got %s", svc.Image)
	}
	if svc.Environment["POSTGRES_DB"] != "testdb" {
		t.Errorf("expected POSTGRES_DB=testdb, got %s", svc.Environment["POSTGRES_DB"])
	}
	if loadedDir != layerDir {
		t.Errorf("expected layerDir=%s, got %s", layerDir, loadedDir)
	}
}

func TestLoadLayer_NotFound(t *testing.T) {
	_, _, err := loadLayer("nonexistent", []string{t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing layer")
	}
}

func TestLoadLayer_YMLExtension(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "redis.yml"), []byte("image: redis:7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, _, err := loadLayer("redis", []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if svc.Image != "redis:7" {
		t.Errorf("expected redis:7, got %s", svc.Image)
	}
}

func TestMergeService_ScalarOverride(t *testing.T) {
	base := ComposeService{Image: "postgres:15", Restart: "always"}
	overlay := ComposeService{Image: "postgres:16"}
	merged := mergeService(base, overlay, "/layers", "/scenario")
	if merged.Image != "postgres:16" {
		t.Errorf("expected postgres:16, got %s", merged.Image)
	}
	if merged.Restart != "always" {
		t.Errorf("expected restart=always, got %s", merged.Restart)
	}
}

func TestMergeService_MapMerge(t *testing.T) {
	base := ComposeService{
		Image:       "postgres:16",
		Environment: map[string]string{"POSTGRES_DB": "default", "POSTGRES_USER": "admin"},
	}
	overlay := ComposeService{
		Environment: map[string]string{"POSTGRES_DB": "mydb", "EXTRA": "val"},
	}
	merged := mergeService(base, overlay, "/layers", "/scenario")
	if merged.Environment["POSTGRES_DB"] != "mydb" {
		t.Errorf("overlay should win: got %s", merged.Environment["POSTGRES_DB"])
	}
	if merged.Environment["POSTGRES_USER"] != "admin" {
		t.Errorf("base should be kept: got %s", merged.Environment["POSTGRES_USER"])
	}
	if merged.Environment["EXTRA"] != "val" {
		t.Errorf("overlay should add: got %s", merged.Environment["EXTRA"])
	}
}

func TestMergeService_SliceAppend(t *testing.T) {
	base := ComposeService{Image: "test", Ports: []string{"5432:5432"}}
	overlay := ComposeService{Ports: []string{"5433:5432"}}
	merged := mergeService(base, overlay, "/layers", "/scenario")
	if len(merged.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(merged.Ports))
	}
}

func TestMergeService_ClearsFrom(t *testing.T) {
	base := ComposeService{Image: "test"}
	overlay := ComposeService{From: "databases/postgres"}
	merged := mergeService(base, overlay, "/layers", "/scenario")
	if merged.From != "" {
		t.Errorf("from should be cleared after merge, got %s", merged.From)
	}
}

func TestResolveLayerVolume_Relative(t *testing.T) {
	result := resolveLayerVolume("./init.sql:/docker-entrypoint-initdb.d/init.sql", "/tmp/layers/databases")
	if result != "/tmp/layers/databases/init.sql:/docker-entrypoint-initdb.d/init.sql" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestResolveLayerVolume_Absolute(t *testing.T) {
	result := resolveLayerVolume("/data:/data", "/tmp/layers")
	if result != "/data:/data" {
		t.Errorf("absolute paths should not change: %s", result)
	}
}

func TestResolveLayerVolume_Named(t *testing.T) {
	result := resolveLayerVolume("pgdata", "/tmp/layers")
	if result != "pgdata" {
		t.Errorf("named volumes should not change: %s", result)
	}
}

func TestResolveLayersAndMatrix_NoMatrix(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerDir, "redis.yaml"), []byte("image: redis:7\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Scenario{
		Name: "test",
		Dir:  dir,
		Services: map[string]ComposeService{
			"cache": {From: "redis"},
			"app":   {Image: "alpine:3.19"},
		},
	}

	resolved, err := ResolveLayersAndMatrix(s, []string{layerDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 scenario, got %d", len(resolved))
	}
	if resolved[0].Services["cache"].Image != "redis:7" {
		t.Errorf("expected redis:7, got %s", resolved[0].Services["cache"].Image)
	}
	if resolved[0].Services["cache"].From != "" {
		t.Errorf("from should be cleared")
	}
	if resolved[0].Services["app"].Image != "alpine:3.19" {
		t.Errorf("non-layer service should be unchanged")
	}
}

func TestResolveLayersAndMatrix_WithMatrix(t *testing.T) {
	dir := t.TempDir()
	layerDir := filepath.Join(dir, "layers")
	if err := os.MkdirAll(layerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerDir, "postgres.yaml"), []byte("image: postgres:16\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layerDir, "mysql.yaml"), []byte("image: mysql:8\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Scenario{
		Name: "grafana",
		Dir:  dir,
		Services: map[string]ComposeService{
			"db":  {From: "${matrix.database}"},
			"app": {Image: "grafana/grafana:10.3.1"},
		},
		Matrix: map[string][]string{
			"database": {"postgres", "mysql"},
		},
	}

	resolved, err := ResolveLayersAndMatrix(s, []string{layerDir})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 scenarios, got %d", len(resolved))
	}

	// Verify each variant has the correct database image.
	images := map[string]bool{}
	for _, r := range resolved {
		images[r.Services["db"].Image] = true
		// Verify name includes the variant.
		if r.Name != "grafana-postgres" && r.Name != "grafana-mysql" {
			t.Errorf("unexpected scenario name: %s", r.Name)
		}
	}
	if !images["postgres:16"] {
		t.Error("missing postgres:16 variant")
	}
	if !images["mysql:8"] {
		t.Error("missing mysql:8 variant")
	}
}

func TestResolveLayersAndMatrix_LayerNotFound(t *testing.T) {
	s := &Scenario{
		Name: "test",
		Dir:  t.TempDir(),
		Services: map[string]ComposeService{
			"db": {From: "nonexistent"},
		},
	}

	_, err := ResolveLayersAndMatrix(s, []string{t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing layer")
	}
}

func TestDefaultLayerDirs(t *testing.T) {
	dirs := defaultLayerDirs("/home/user/tests/scanners/grafana")
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs, got %d", len(dirs))
	}
	if dirs[0] != "/home/user/tests/scanners/grafana/layers" {
		t.Errorf("unexpected first dir: %s", dirs[0])
	}
	if dirs[1] != "/home/user/tests/scanners/layers" {
		t.Errorf("unexpected second dir: %s", dirs[1])
	}
}

func TestShallowCopyScenario_Independence(t *testing.T) {
	original := &Scenario{
		Name: "test",
		Services: map[string]ComposeService{
			"app": {Image: "alpine"},
		},
		Tags: []string{"web"},
	}

	cp := shallowCopyScenario(original)
	cp.Services["app"] = ComposeService{Image: "ubuntu"}
	cp.Tags = append(cp.Tags, "extra")

	if original.Services["app"].Image != "alpine" {
		t.Error("copy mutated original services")
	}
	if len(original.Tags) != 1 {
		t.Error("copy mutated original tags")
	}
}

func TestValidate_ServiceWithFrom(t *testing.T) {
	s := &Scenario{
		Name: "test",
		Services: map[string]ComposeService{
			"db":  {From: "databases/postgres"},
			"app": {Image: "grafana/grafana:10.3.1"},
		},
		Assertions: []Assertion{
			{Name: "test", Type: "command"},
		},
	}
	if err := s.Validate(); err != nil {
		t.Errorf("service with from: should pass validation: %v", err)
	}
}
