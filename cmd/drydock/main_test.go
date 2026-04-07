package main

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stormbane-security/drydock/internal/scenario"
)

func TestMatchesTags_SingleMatch(t *testing.T) {
	if !matchesTags([]string{"redis", "database"}, []string{"redis"}) {
		t.Error("expected match for 'redis'")
	}
}

func TestMatchesTags_MultipleFilterOneMatch(t *testing.T) {
	if !matchesTags([]string{"redis", "database"}, []string{"web", "database"}) {
		t.Error("expected match when any filter tag matches")
	}
}

func TestMatchesTags_NoMatch(t *testing.T) {
	if matchesTags([]string{"redis", "database"}, []string{"web", "cicd"}) {
		t.Error("expected no match when no filter tags match")
	}
}

func TestMatchesTags_EmptyScenarioTags(t *testing.T) {
	if matchesTags(nil, []string{"redis"}) {
		t.Error("expected no match with empty scenario tags")
	}
}

func TestMatchesTags_EmptyFilterTags(t *testing.T) {
	if matchesTags([]string{"redis"}, nil) {
		t.Error("expected no match with empty filter tags")
	}
}

func TestMatchesTags_BothEmpty(t *testing.T) {
	if matchesTags(nil, nil) {
		t.Error("expected no match when both are empty")
	}
}

func TestMatchesTags_TrimWhitespace(t *testing.T) {
	if !matchesTags([]string{"redis"}, []string{" redis "}) {
		t.Error("expected match with trimmed whitespace")
	}
}

func TestMatchesTags_ExactMatch(t *testing.T) {
	if !matchesTags([]string{"cors"}, []string{"cors"}) {
		t.Error("expected exact match")
	}
}

func TestMatchesTags_CaseSensitive(t *testing.T) {
	if matchesTags([]string{"Redis"}, []string{"redis"}) {
		t.Error("tags should be case-sensitive")
	}
}

func TestMatchesTags_DuplicateTags(t *testing.T) {
	if !matchesTags([]string{"a", "b", "a"}, []string{"a"}) {
		t.Error("expected match with duplicate tags")
	}
}

// ── Exclude tags ─────────────────────────────────────────────────────────

func TestExcludeTags_Basic(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{Name: "s1", Tags: []string{"fast"}},
		{Name: "s2", Tags: []string{"flaky"}},
		{Name: "s3", Tags: []string{"fast", "database"}},
	}
	excTags := []string{"flaky"}
	var filtered []*scenario.Scenario
	for _, s := range scenarios {
		if !matchesTags(s.Tags, excTags) {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 scenarios after exclude, got %d", len(filtered))
	}
	if filtered[0].Name != "s1" || filtered[1].Name != "s3" {
		t.Errorf("wrong scenarios after exclude: %s, %s", filtered[0].Name, filtered[1].Name)
	}
}

func TestExcludeTags_ExcludeMultiple(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{Name: "s1", Tags: []string{"fast"}},
		{Name: "s2", Tags: []string{"flaky"}},
		{Name: "s3", Tags: []string{"slow"}},
	}
	excTags := []string{"flaky", "slow"}
	var filtered []*scenario.Scenario
	for _, s := range scenarios {
		if !matchesTags(s.Tags, excTags) {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) != 1 {
		t.Fatalf("expected 1 scenario after exclude, got %d", len(filtered))
	}
	if filtered[0].Name != "s1" {
		t.Errorf("expected s1, got %s", filtered[0].Name)
	}
}

func TestExcludeTags_NoExclude(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{Name: "s1", Tags: []string{"fast"}},
		{Name: "s2", Tags: []string{"database"}},
	}
	excTags := []string{"flaky"}
	var filtered []*scenario.Scenario
	for _, s := range scenarios {
		if !matchesTags(s.Tags, excTags) {
			filtered = append(filtered, s)
		}
	}
	if len(filtered) != 2 {
		t.Fatalf("expected all 2 scenarios when no tags match, got %d", len(filtered))
	}
}

// ── collectImages (pull subcommand) ──────────────────────────────────────

func TestCollectImages_UniqueAndSorted(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{
			Name: "s1",
			Services: map[string]scenario.ComposeService{
				"web":   {Image: "nginx:latest"},
				"cache": {Image: "redis:7"},
			},
		},
		{
			Name: "s2",
			Services: map[string]scenario.ComposeService{
				"web": {Image: "nginx:latest"}, // duplicate
				"db":  {Image: "postgres:16"},
			},
		},
	}
	images := collectImages(scenarios)
	if len(images) != 3 {
		t.Fatalf("expected 3 unique images, got %d: %v", len(images), images)
	}
	// Should be sorted.
	expected := []string{"nginx:latest", "postgres:16", "redis:7"}
	for i, want := range expected {
		if images[i] != want {
			t.Errorf("images[%d] = %q, want %q", i, images[i], want)
		}
	}
}

func TestCollectImages_SkipBuildOnly(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{
			Name: "s1",
			Services: map[string]scenario.ComposeService{
				"app": {Build: &scenario.ComposeBuild{Context: "."}},
				"db":  {Image: "postgres:16"},
			},
		},
	}
	images := collectImages(scenarios)
	if len(images) != 1 {
		t.Fatalf("expected 1 image (skip build-only), got %d: %v", len(images), images)
	}
	if images[0] != "postgres:16" {
		t.Errorf("expected postgres:16, got %s", images[0])
	}
}

func TestCollectImages_Empty(t *testing.T) {
	images := collectImages(nil)
	if len(images) != 0 {
		t.Errorf("expected 0 images for nil scenarios, got %d", len(images))
	}
}

func TestCollectImages_NoServices(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{
			Name:    "s1",
			Backend: scenario.Backend{Type: "terraform", TerraformDir: "tf"},
		},
	}
	images := collectImages(scenarios)
	if len(images) != 0 {
		t.Errorf("expected 0 images for terraform-only, got %d", len(images))
	}
}

// ── runScenarios (parallel execution) ────────────────────────────────────

func TestRunScenarios_Sequential(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	}
	var order []string
	runOne := func(s *scenario.Scenario) scenarioResult {
		order = append(order, s.Name)
		return scenarioResult{scenario: s}
	}
	ctx := context.Background()
	results := runScenarios(ctx, scenarios, 1, runOne)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Sequential means in-order.
	for i, name := range []string{"a", "b", "c"} {
		if order[i] != name {
			t.Errorf("order[%d] = %q, want %q", i, order[i], name)
		}
	}
}

func TestRunScenarios_Parallel(t *testing.T) {
	scenarios := []*scenario.Scenario{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
		{Name: "d"},
	}
	var count atomic.Int32
	runOne := func(s *scenario.Scenario) scenarioResult {
		count.Add(1)
		return scenarioResult{scenario: s}
	}
	ctx := context.Background()
	results := runScenarios(ctx, scenarios, 4, runOne)
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	if count.Load() != 4 {
		t.Errorf("expected 4 executions, got %d", count.Load())
	}
	// Verify results are in correct order (index-keyed).
	for i, s := range scenarios {
		if results[i].scenario.Name != s.Name {
			t.Errorf("results[%d].scenario = %q, want %q", i, results[i].scenario.Name, s.Name)
		}
	}
}

func TestRunScenarios_ParallelResultsOrdered(t *testing.T) {
	// Even with concurrency, results[i] must correspond to scenarios[i].
	scenarios := make([]*scenario.Scenario, 20)
	for i := range scenarios {
		scenarios[i] = &scenario.Scenario{Name: string(rune('A' + i))}
	}
	runOne := func(s *scenario.Scenario) scenarioResult {
		return scenarioResult{scenario: s}
	}
	ctx := context.Background()
	results := runScenarios(ctx, scenarios, 5, runOne)
	for i, s := range scenarios {
		if results[i].scenario.Name != s.Name {
			t.Errorf("results[%d].scenario = %q, want %q", i, results[i].scenario.Name, s.Name)
		}
	}
}

func TestRunScenarios_ParallelClampedToOne(t *testing.T) {
	// parallel=0 should be treated as sequential (clamped in cmdRun, but runScenarios handles <=1).
	scenarios := []*scenario.Scenario{{Name: "a"}}
	called := false
	runOne := func(s *scenario.Scenario) scenarioResult {
		called = true
		return scenarioResult{scenario: s}
	}
	ctx := context.Background()
	results := runScenarios(ctx, scenarios, 0, runOne)
	if !called {
		t.Error("expected runOne to be called")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}
