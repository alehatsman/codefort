package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alehatsman/codefort/internal/config"
)

func TestIsGitRequest(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/aleh/repo/info/refs", true},
		{"/aleh/repo/git-upload-pack", true},
		{"/aleh/repo/git-receive-pack", true},
		{"/aleh/repo", false},
		{"/", false},
		{"/assets/app.js", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, tt.path, nil)
		if got := isGitRequest(r); got != tt.want {
			t.Errorf("isGitRequest(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestWebHandlerSPA(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "index.html"), "<!doctype html>INDEX")
	mustWrite(t, filepath.Join(dir, "assets", "app.js"), "console.log(1)")

	s := &Server{cfg: &config.Config{WebDir: dir}}
	h := s.webHandler()
	if h == nil {
		t.Fatal("webHandler returned nil with WebDir set")
	}

	// Existing asset served as-is.
	if body, _ := serve(h, "/assets/app.js"); body != "console.log(1)" {
		t.Errorf("asset body = %q", body)
	}
	// Unknown route falls back to index.html (client-side routing).
	if body, _ := serve(h, "/aleh/repo/issues/3"); body != "<!doctype html>INDEX" {
		t.Errorf("SPA fallback body = %q", body)
	}
	// Root serves index.html.
	if _, code := serve(h, "/"); code != http.StatusOK {
		t.Errorf("root status = %d", code)
	}
}

func TestWebHandlerDisabled(t *testing.T) {
	s := &Server{cfg: &config.Config{WebDir: ""}}
	if s.webHandler() != nil {
		t.Error("webHandler should be nil when WebDir is empty")
	}
}

func serve(h http.Handler, path string) (string, int) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Body.String(), rec.Code
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
