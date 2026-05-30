package server

import (
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
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

func TestParseListFilter(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantErr   bool
		errSubstr string
		want      storage.ListFilter
	}{
		{
			name: "empty",
			raw:  "",
			want: storage.ListFilter{},
		},
		{
			name: "single state",
			raw:  "state=todo",
			want: storage.ListFilter{States: []api.IssueState{api.IssueTodo}},
		},
		{
			name: "multiple state comma-separated",
			raw:  "state=todo,in_progress",
			want: storage.ListFilter{States: []api.IssueState{api.IssueTodo, api.IssueInProgress}},
		},
		{
			name: "multiple state via repeated param",
			raw:  "state=todo&state=done",
			want: storage.ListFilter{States: []api.IssueState{api.IssueTodo, api.IssueDone}},
		},
		{
			name:      "invalid state rejected",
			raw:       "state=bogus",
			wantErr:   true,
			errSubstr: "invalid state",
		},
		{
			name: "assignee plain",
			raw:  "assignee=claude-code",
			want: storage.ListFilter{Assignee: "claude-code"},
		},
		{
			name: "assignee null literal",
			raw:  "assignee=null",
			want: storage.ListFilter{Assignee: "null"},
		},
		{
			name: "limit",
			raw:  "limit=25",
			want: storage.ListFilter{Limit: 25},
		},
		{
			name:      "negative limit rejected",
			raw:       "limit=-1",
			wantErr:   true,
			errSubstr: "invalid limit",
		},
		{
			name:      "non-numeric limit rejected",
			raw:       "limit=abc",
			wantErr:   true,
			errSubstr: "invalid limit",
		},
		{
			name: "author plain",
			raw:  "author=alice",
			want: storage.ListFilter{Author: "alice"},
		},
		{
			name: "sort oldest",
			raw:  "sort=oldest",
			want: storage.ListFilter{Sort: api.IssueSortOldest},
		},
		{
			name: "sort recently-updated",
			raw:  "sort=recently-updated",
			want: storage.ListFilter{Sort: api.IssueSortRecentlyUpdated},
		},
		{
			name: "empty sort is the unset default",
			raw:  "sort=",
			want: storage.ListFilter{},
		},
		{
			name:      "invalid sort rejected",
			raw:       "sort=bogus",
			wantErr:   true,
			errSubstr: "invalid sort",
		},
		{
			name: "combined",
			raw:  "state=todo,in_progress&assignee=null&author=alice&sort=oldest&limit=10",
			want: storage.ListFilter{
				States:   []api.IssueState{api.IssueTodo, api.IssueInProgress},
				Assignee: "null",
				Author:   "alice",
				Sort:     api.IssueSortOldest,
				Limit:    10,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.raw)
			if err != nil {
				t.Fatalf("parse query: %v", err)
			}
			got, err := parseListFilter(q)
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
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
