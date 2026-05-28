package api

import "testing"

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
		{"open", false},     // pre-cleanup legacy value
		{"OPEN", false},     // case-sensitive
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
