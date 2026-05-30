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
			// the caller to fill from MOONGIT_SERVER.
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
