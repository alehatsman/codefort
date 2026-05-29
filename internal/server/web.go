package server

import (
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

// webHandler serves the built single-page app from cfg.WebDir. Requests
// that map to an existing file (JS, CSS, assets) are served as-is;
// everything else falls back to index.html so the client-side router
// owns the route. Returns nil when WebDir is unset — the caller then
// leaves the default route on the git mux, preserving API+git-only mode.
func (s *Server) webHandler() http.Handler {
	if s.cfg.WebDir == "" {
		return nil
	}
	root := http.Dir(s.cfg.WebDir)
	files := http.FileServer(root)
	index := filepath.Join(s.cfg.WebDir, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path the same way http.FileServer does, then probe
		// for the file. Hit → serve it; miss → SPA fallback. Open also
		// resolves "/" to the directory, which the file server renders
		// as index.html, so the root path works without a special case.
		f, err := root.Open(path.Clean(r.URL.Path))
		if err != nil {
			http.ServeFile(w, r, index)
			return
		}
		f.Close()
		files.ServeHTTP(w, r)
	})
}

// isGitRequest reports whether the request targets a git smart-HTTP
// endpoint. These have fixed suffixes, which lets the dispatcher route
// `git clone http://host/owner/repo` to the git mux while a browser
// hitting /owner/repo falls through to the SPA.
func isGitRequest(r *http.Request) bool {
	p := r.URL.Path
	return strings.HasSuffix(p, "/info/refs") ||
		strings.HasSuffix(p, "/git-upload-pack") ||
		strings.HasSuffix(p, "/git-receive-pack")
}
