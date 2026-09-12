package main

import (
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name       string
		remote     string
		wantErr    bool
		errSubstr  string
		wantServer string
		wantOwner  string
		wantRepo   string
	}{
		{
			name:       "http with .git",
			remote:     "http://localhost:18080/aleh/hello.git",
			wantServer: "http://localhost:18080",
			wantOwner:  "aleh",
			wantRepo:   "hello",
		},
		{
			name:       "http without .git",
			remote:     "http://localhost:18080/aleh/hello",
			wantServer: "http://localhost:18080",
			wantOwner:  "aleh",
			wantRepo:   "hello",
		},
		{
			name:       "https plain host",
			remote:     "https://git.example.com/team/proj.git",
			wantServer: "https://git.example.com",
			wantOwner:  "team",
			wantRepo:   "proj",
		},
		{
			name:       "trailing slash on path",
			remote:     "http://h/aleh/hello/",
			wantServer: "http://h",
			wantOwner:  "aleh",
			wantRepo:   "hello",
		},
		{
			// scp-style ssh resolves owner/repo; server is left empty for
			// the caller to fill from CODEFORT_SERVER.
			name:       "ssh scp-style resolves owner/repo",
			remote:     "git@example.com:aleh/hello.git",
			wantServer: "",
			wantOwner:  "aleh",
			wantRepo:   "hello",
		},
		{
			name:       "ssh:// resolves owner/repo",
			remote:     "ssh://git@example.com/aleh/hello.git",
			wantServer: "",
			wantOwner:  "aleh",
			wantRepo:   "hello",
		},
		{
			name:      "file scheme rejected",
			remote:    "file:///tmp/repos/aleh/hello.git",
			wantErr:   true,
			errSubstr: "unsupported remote scheme",
		},
		{
			name:      "missing repo segment",
			remote:    "http://host/aleh",
			wantErr:   true,
			errSubstr: "is not <owner>/<repo>",
		},
		{
			name:      "empty path",
			remote:    "http://host/",
			wantErr:   true,
			errSubstr: "is not <owner>/<repo>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRemote(tt.remote)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q missing substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.server != tt.wantServer {
				t.Errorf("server = %q, want %q", got.server, tt.wantServer)
			}
			if got.owner != tt.wantOwner {
				t.Errorf("owner = %q, want %q", got.owner, tt.wantOwner)
			}
			if got.repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", got.repo, tt.wantRepo)
			}
		})
	}
}

// TestRunPRDispatch covers the `pr` argument-parsing and validation branches
// that fire before any server round-trip (so no remote/token is needed).
func TestRunPRDispatch(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		errSubstr string
	}{
		{"no subcommand", []string{}, "usage: moongit pr"},
		{"unknown subcommand", []string{"frobnicate"}, "unknown pr subcommand"},
		{"create missing flags", []string{"create", "--title", "t"}, "usage: moongit pr create"},
		{"create missing title", []string{"create", "--base", "main", "--head", "f"}, "usage: moongit pr create"},
		{"list invalid state", []string{"list", "--state", "bogus"}, "invalid --state"},
		{"show no arg", []string{"show"}, "usage: moongit pr show"},
		{"show bad number", []string{"show", "abc"}, "invalid pull request number"},
		{"merge no arg", []string{"merge"}, "usage: moongit pr merge"},
		{"merge bad number", []string{"merge", "0"}, "invalid pull request number"},
		{"merge extra args", []string{"merge", "1", "extra"}, "unexpected extra args"},
		{"close no arg", []string{"close"}, "usage: moongit pr close"},
		{"close bad number", []string{"close", "abc"}, "invalid pull request number"},
		{"close extra args", []string{"close", "1", "extra"}, "unexpected extra args"},
		{"reopen no arg", []string{"reopen"}, "usage: moongit pr reopen"},
		{"reopen bad number", []string{"reopen", "0"}, "invalid pull request number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runPR(tt.args)
			if err == nil {
				t.Fatalf("runPR(%v) = nil, want error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q missing substring %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

// TestRunRepoDispatch covers the `repo` argument-parsing and validation
// branches that fire before any server round-trip (so no remote/token is
// needed). A well-formed `delete <owner>/<name>` is omitted because it
// proceeds to discoverTarget + an HTTP call.
func TestRunRepoDispatch(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		errSubstr string
	}{
		{"no subcommand", []string{}, "usage: moongit repo"},
		{"unknown subcommand", []string{"frobnicate"}, "unknown repo subcommand"},
		{"delete no arg", []string{"delete"}, "usage: moongit repo delete"},
		{"delete extra args", []string{"delete", "a/b", "c"}, "unexpected extra args"},
		{"delete not owner/repo", []string{"delete", "justname"}, "is not <owner>/<repo>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRepo(tt.args)
			if err == nil {
				t.Fatalf("runRepo(%v) = nil, want error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.errSubstr) {
				t.Errorf("error %q missing substring %q", err.Error(), tt.errSubstr)
			}
		})
	}
}

func TestParseLineSpec(t *testing.T) {
	tests := []struct {
		spec      string
		wantStart int
		wantEnd   int
		wantErr   bool
	}{
		{spec: "5", wantStart: 5, wantEnd: 5},
		{spec: "5-12", wantStart: 5, wantEnd: 12},
		{spec: "7-7", wantStart: 7, wantEnd: 7},
		{spec: " 5 - 12 ", wantStart: 5, wantEnd: 12}, // tolerate surrounding spaces
		{spec: "0", wantErr: true},                    // lines are 1-based
		{spec: "8-3", wantErr: true},                  // reversed range
		{spec: "5-0", wantErr: true},                  // end below start
		{spec: "abc", wantErr: true},
		{spec: "5-x", wantErr: true},
		{spec: "", wantErr: true},
		{spec: "-5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			start, end, err := parseLineSpec(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLineSpec(%q) = (%d, %d, nil), want error", tt.spec, start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLineSpec(%q): unexpected error: %v", tt.spec, err)
			}
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("parseLineSpec(%q) = (%d, %d), want (%d, %d)", tt.spec, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}
