package server

import (
	"reflect"
	"strings"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

func TestCleanTreePath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "root empty", in: "", want: ""},
		{name: "root slash", in: "/", want: ""},
		{name: "plain file", in: "go.mod", want: "go.mod"},
		{name: "nested", in: "internal/server/tree.go", want: "internal/server/tree.go"},
		{name: "leading slash stripped", in: "/internal/server", want: "internal/server"},
		{name: "trailing slash stripped", in: "internal/server/", want: "internal/server"},
		{name: "interior dot collapsed", in: "internal/./server", want: "internal/server"},
		{name: "traversal rejected", in: "../etc/passwd", wantErr: true},
		{name: "dotdot only rejected", in: "..", wantErr: true},
		{name: "embedded traversal rejected", in: "internal/../../etc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanTreePath(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("cleanTreePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseTreeRecord(t *testing.T) {
	tests := []struct {
		name string
		rec  string
		dir  string
		want api.TreeEntry
		ok   bool
	}{
		{
			name: "blob at root",
			rec:  "100644 blob e057fc170d8932806ec9db4188da78a4faa441fa     527\t.gitignore",
			dir:  "",
			want: api.TreeEntry{Name: ".gitignore", Path: ".gitignore", Type: "blob", Size: 527},
			ok:   true,
		},
		{
			name: "tree at root has zero size",
			rec:  "040000 tree 916574b9c61edc2df460158b5b6003a44192e58d       -\tcmd",
			dir:  "",
			want: api.TreeEntry{Name: "cmd", Path: "cmd", Type: "tree"},
			ok:   true,
		},
		{
			name: "blob in subdir gets full path",
			rec:  "100644 blob abc123       42\tmain.go",
			dir:  "internal/server",
			want: api.TreeEntry{Name: "main.go", Path: "internal/server/main.go", Type: "blob", Size: 42},
			ok:   true,
		},
		{
			name: "submodule commit entry skipped",
			rec:  "160000 commit deadbeef\tvendor/lib",
			dir:  "",
			ok:   false,
		},
		{
			name: "malformed record skipped",
			rec:  "garbage-without-tab",
			dir:  "",
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseTreeRecord(tt.rec, tt.dir)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSortEntries(t *testing.T) {
	entries := []api.TreeEntry{
		{Name: "zebra.go", Type: "blob"},
		{Name: "src", Type: "tree"},
		{Name: "Makefile", Type: "blob"},
		{Name: "Docs", Type: "tree"},
	}
	sortEntries(entries)
	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Name
	}
	// Directories first (case-insensitive), then files (case-insensitive).
	want := []string{"Docs", "src", "Makefile", "zebra.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sortEntries order = %v, want %v", got, want)
	}
}

func TestRawContentType(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content []byte
		wantPfx string // Content-Type prefix (charset params vary by platform)
	}{
		{name: "png by extension", path: "docs/logo.png", wantPfx: "image/png"},
		{name: "svg by extension", path: "icon.svg", wantPfx: "image/svg+xml"},
		{name: "uppercase extension", path: "PHOTO.JPG", wantPfx: "image/jpeg"},
		{
			name:    "extensionless falls back to sniff",
			path:    "LICENSE",
			content: []byte("PNG fake? no — plain text\n"),
			wantPfx: "text/plain",
		},
		{
			name:    "extensionless png sniffed",
			path:    "blob",
			content: []byte("\x89PNG\r\n\x1a\n"),
			wantPfx: "image/png",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rawContentType(tt.path, tt.content)
			if !strings.HasPrefix(got, tt.wantPfx) {
				t.Errorf("rawContentType(%q) = %q, want prefix %q", tt.path, got, tt.wantPfx)
			}
		})
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want bool
	}{
		{name: "plain text", in: []byte("package main\n"), want: false},
		{name: "empty", in: []byte{}, want: false},
		{name: "has NUL", in: []byte("foo\x00bar"), want: true},
		{name: "NUL past 8000 ignored", in: append([]byte(strings.Repeat("a", 8000)), 0), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinary(tt.in); got != tt.want {
				t.Errorf("isBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}
