package main

import (
	"testing"
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
