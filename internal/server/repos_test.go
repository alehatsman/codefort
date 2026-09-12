package server

import "testing"

func TestValidRepoComponent(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "simple", in: "codefort", want: true},
		{name: "with dash", in: "my-repo", want: true},
		{name: "with underscore", in: "my_repo", want: true},
		{name: "with dot", in: "repo.v2", want: true},
		{name: "digits", in: "user123", want: true},
		{name: "empty rejected", in: "", want: false},
		{name: "dot rejected", in: ".", want: false},
		{name: "dotdot rejected", in: "..", want: false},
		{name: "slash rejected", in: "owner/repo", want: false},
		{name: "space rejected", in: "my repo", want: false},
		{name: "traversal rejected", in: "../etc", want: false},
		{name: "too long rejected", in: string(make([]byte, 101)), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validRepoComponent(tt.in); got != tt.want {
				t.Errorf("validRepoComponent(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
