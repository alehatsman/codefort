package api

import (
	"encoding/json"
	"testing"
)

func TestIssueStateValid(t *testing.T) {
	tests := []struct {
		state IssueState
		want  bool
	}{
		{IssueTodo, true},
		{IssueInProgress, true},
		{IssueDone, true},
		{IssueClosed, true},
		{"", false},
		{"open", false}, // pre-cleanup legacy value
		{"OPEN", false}, // case-sensitive
		{"in-progress", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.Valid(); got != tt.want {
				t.Errorf("IssueState(%q).Valid() = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

// TestCompareJSON pins the wire contract the web client and mgit depend on:
// the documented snake_case keys are present, and the Commits/Files slices
// render as JSON arrays (the handler initializes them so they are never null).
func TestCompareJSON(t *testing.T) {
	b, err := json.Marshal(Compare{
		Base:    "main",
		Head:    "feature",
		Commits: []Commit{},
		Files:   []DiffFile{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"base", "head", "merge_base", "ahead", "behind", "commits", "files", "additions", "deletions", "truncated"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in %s", key, b)
		}
	}
	if string(m["commits"]) != "[]" || string(m["files"]) != "[]" {
		t.Errorf("commits/files not empty arrays: commits=%s files=%s", m["commits"], m["files"])
	}
}
