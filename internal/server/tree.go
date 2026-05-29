package server

import (
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
)

// maxBlobBytes caps the size of a file we'll return inline for the code
// viewer. Larger files are reported as TooLarge with no content — clone the
// repo to read them.
const maxBlobBytes = 2 << 20 // 2 MiB

// handleTree lists a directory at ?path= on the repo's default branch. The
// repo root is the empty path. An unborn repo (no commits yet) returns an
// empty listing rather than an error so the UI can render a clone hint.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	p, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ref := headRef(r.Context(), repoDir)
	out := api.Tree{Ref: ref, Path: p, Entries: []api.TreeEntry{}}

	if !hasCommits(r.Context(), repoDir) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// "HEAD:" addresses the root tree; "HEAD:dir" the tree at dir. ls-tree
	// then lists that tree's immediate children (names are basenames).
	treeish := "HEAD:" + p
	raw, err := gitOutput(r.Context(), repoDir, "ls-tree", "--long", "-z", treeish)
	if err != nil {
		// A bad path resolves to a missing tree object: 404, not 500.
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}

	for rec := range strings.SplitSeq(string(raw), "\x00") {
		if rec == "" {
			continue
		}
		entry, ok := parseTreeRecord(rec, p)
		if !ok {
			continue
		}
		out.Entries = append(out.Entries, entry)
	}

	sortEntries(out.Entries)
	writeJSON(w, http.StatusOK, out)
}

// handleBlob returns the contents of the file at ?path= on the default
// branch. Binary and oversized files come back with empty content and the
// matching flag set.
func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	p, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	ref := headRef(r.Context(), repoDir)
	treeish := "HEAD:" + p

	typ, err := gitOutput(r.Context(), repoDir, "cat-file", "-t", treeish)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}
	switch strings.TrimSpace(string(typ)) {
	case "blob":
		// ok
	case "tree":
		writeError(w, http.StatusBadRequest, "path is a directory: "+p)
		return
	default:
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}

	blob := api.Blob{Ref: ref, Path: p}

	sizeOut, err := gitOutput(r.Context(), repoDir, "cat-file", "-s", treeish)
	if err != nil {
		s.logger.Error("blob size", "repo", repoDir, "path", p, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	blob.Size, _ = strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)

	if blob.Size > maxBlobBytes {
		blob.TooLarge = true
		writeJSON(w, http.StatusOK, blob)
		return
	}

	content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", treeish)
	if err != nil {
		s.logger.Error("blob content", "repo", repoDir, "path", p, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if isBinary(content) {
		blob.Binary = true
		writeJSON(w, http.StatusOK, blob)
		return
	}
	blob.Content = string(content)
	writeJSON(w, http.StatusOK, blob)
}

// handleRaw streams a file's raw bytes from the default branch with a
// best-effort Content-Type. Unlike handleBlob (which returns JSON and drops
// binary content), this serves the bytes directly so the web UI can load
// images referenced from rendered markdown. Bearer-authed like its siblings.
//
// Because a repo can contain hand-crafted HTML/SVG, a direct navigation to
// this endpoint must not execute script in our origin: the response carries a
// `sandbox` CSP and nosniff, which neutralise scripts on navigation while
// leaving <img> subresource loads (the only way the UI uses this) unaffected.
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	p, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	treeish := "HEAD:" + p
	typ, err := gitOutput(r.Context(), repoDir, "cat-file", "-t", treeish)
	if err != nil {
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}
	switch strings.TrimSpace(string(typ)) {
	case "blob":
		// ok
	case "tree":
		writeError(w, http.StatusBadRequest, "path is a directory: "+p)
		return
	default:
		writeError(w, http.StatusNotFound, "path not found: "+p)
		return
	}

	sizeOut, err := gitOutput(r.Context(), repoDir, "cat-file", "-s", treeish)
	if err != nil {
		s.logger.Error("raw size", "repo", repoDir, "path", p, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if size, _ := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64); size > maxBlobBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", treeish)
	if err != nil {
		s.logger.Error("raw content", "repo", repoDir, "path", p, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", rawContentType(p, content))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// rawContentType picks a Content-Type for a raw blob: file extension first
// (so .svg, .css and friends keep their real type), then content sniffing as
// a fallback for extensionless files.
func rawContentType(p string, content []byte) string {
	if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
		return ct
	}
	return http.DetectContentType(content)
}

// repoDirOrFail resolves the on-disk bare repo path, writing a 4xx and
// returning ok=false if the path is invalid or the repo isn't on disk. The
// caller should run lookupRepoOrFail first for DB-registration semantics.
func (s *Server) repoDirOrFail(w http.ResponseWriter, r *http.Request) (string, bool) {
	repoDir, err := repoPath(s.cfg.ReposDir, r.PathValue("owner"), r.PathValue("repo"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	if _, err := os.Stat(repoDir); err != nil {
		writeError(w, http.StatusNotFound, "repository not found on disk")
		return "", false
	}
	return repoDir, true
}

// parseTreeRecord parses one NUL-terminated `ls-tree --long` record:
//
//	<mode> SP <type> SP <object> SP* <size> TAB <name>
//
// dir is the parent path, used to build the entry's full path.
func parseTreeRecord(rec, dir string) (api.TreeEntry, bool) {
	meta, name, found := strings.Cut(rec, "\t")
	if !found {
		return api.TreeEntry{}, false
	}
	fields := strings.Fields(meta)
	if len(fields) < 4 {
		return api.TreeEntry{}, false
	}
	typ := fields[1] // "blob" | "tree"
	if typ != "blob" && typ != "tree" {
		return api.TreeEntry{}, false // skip commit (submodule) entries
	}

	full := name
	if dir != "" {
		full = dir + "/" + name
	}
	entry := api.TreeEntry{Name: name, Path: full, Type: typ}
	if typ == "blob" {
		entry.Size, _ = strconv.ParseInt(fields[3], 10, 64)
	}
	return entry, true
}

// sortEntries orders a listing directories-first, then case-insensitively
// by name — the conventional file-browser ordering.
func sortEntries(entries []api.TreeEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Type != b.Type {
			return a.Type == "tree"
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// cleanTreePath validates and normalizes a repo-relative path. It rejects
// absolute paths and any traversal outside the repo root. The empty path
// (repo root) is allowed.
func cleanTreePath(p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return "", nil
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid path")
	}
	return cleaned, nil
}

// headRef returns the default branch's short name (e.g. "main"). For an
// unborn repo it still reports the configured branch. Falls back to "HEAD".
func headRef(ctx context.Context, repoDir string) string {
	out, err := gitOutput(ctx, repoDir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

// hasCommits reports whether HEAD resolves to a commit (false for a freshly
// created, never-pushed repo).
func hasCommits(ctx context.Context, repoDir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "-q", "HEAD")
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

// gitOutput runs a read-only git plumbing command in the bare repo dir and
// returns stdout. Stderr is folded into the error for diagnostics.
func gitOutput(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("git " + args[0] + ": " + err.Error() + ": " + stderr.String())
	}
	return out, nil
}

// isBinary applies git's heuristic: a NUL byte in the first 8000 bytes means
// the content is treated as binary.
func isBinary(content []byte) bool {
	head := content
	if len(head) > 8000 {
		head = head[:8000]
	}
	return bytes.IndexByte(head, 0) >= 0
}
