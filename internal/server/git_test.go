package server

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPktLine(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"empty", "", "0004"},
		{"service announce", "# service=git-upload-pack\n", "001e# service=git-upload-pack\n"},
		{"receive-pack announce", "# service=git-receive-pack\n", "001f# service=git-receive-pack\n"},
		{"short", "hi\n", "0007hi\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(pktLine(tt.payload))
			if got != tt.want {
				t.Errorf("pktLine(%q) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}

func TestRepoPath(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name      string
		owner     string
		repo      string
		wantErr   bool
		wantTail  string // expected suffix under root, with OS-correct separators
		errSubstr string // optional substring to check in error
	}{
		{name: "plain", owner: "aleh", repo: "hello", wantTail: filepath.Join("aleh", "hello.git")},
		{name: "with .git suffix", owner: "aleh", repo: "hello.git", wantTail: filepath.Join("aleh", "hello.git")},
		{name: "empty owner", owner: "", repo: "hello", wantErr: true, errSubstr: "missing"},
		{name: "empty repo", owner: "aleh", repo: "", wantErr: true, errSubstr: "missing"},
		{name: "slash in owner", owner: "a/b", repo: "hello", wantErr: true, errSubstr: "invalid"},
		{name: "slash in repo", owner: "aleh", repo: "h/i", wantErr: true, errSubstr: "invalid"},
		{name: "backslash in owner", owner: `a\b`, repo: "hello", wantErr: true, errSubstr: "invalid"},
		{name: "dotdot owner", owner: "..", repo: "hello", wantErr: true, errSubstr: "invalid"},
		{name: "dotdot repo", owner: "aleh", repo: "..", wantErr: true, errSubstr: "invalid"},
		{name: "owner starts with dot", owner: ".hidden", repo: "hello", wantErr: true, errSubstr: "invalid"},
		{name: "repo starts with dot", owner: "aleh", repo: ".git", wantErr: true, errSubstr: "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repoPath(root, tt.owner, tt.repo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q missing substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := filepath.Join(root, tt.wantTail)
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
