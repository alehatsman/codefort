package server

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alehatsman/codefort/internal/storage"
)

// validServices lists the git smart-HTTP services we accept on /info/refs.
var validServices = map[string]struct{}{
	"git-upload-pack":  {}, // fetch / clone
	"git-receive-pack": {}, // push
}

// repoPath resolves {owner}/{repo}.git under reposDir, rejecting traversal.
// The router pattern matches /{owner}/{repo}/... but real git clients hit
// /{owner}/{repo}.git/..., so {repo} arrives as "name.git". We accept either.
func repoPath(reposDir, owner, repo string) (string, error) {
	if owner == "" || repo == "" {
		return "", fmt.Errorf("missing owner or repo")
	}
	if strings.ContainsAny(owner, "/\\") || strings.ContainsAny(repo, "/\\") {
		return "", fmt.Errorf("invalid owner or repo")
	}
	if owner == ".." || repo == ".." || strings.HasPrefix(owner, ".") || strings.HasPrefix(repo, ".") {
		return "", fmt.Errorf("invalid owner or repo")
	}
	if !strings.HasSuffix(repo, ".git") {
		repo += ".git"
	}
	full := filepath.Join(reposDir, owner, repo)

	rel, err := filepath.Rel(reposDir, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes repos dir")
	}
	return full, nil
}

func (s *Server) handleInfoRefs(w http.ResponseWriter, r *http.Request) {
	service := r.URL.Query().Get("service")
	if _, ok := validServices[service]; !ok {
		http.Error(w, "service not supported", http.StatusForbidden)
		return
	}

	repoDir, err := repoPath(s.cfg.ReposDir, r.PathValue("owner"), r.PathValue("repo"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(repoDir); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-advertisement", service))
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(pktLine("# service=" + service + "\n")); err != nil {
		return
	}
	if _, err := w.Write([]byte("0000")); err != nil {
		return
	}

	cmd := exec.CommandContext(r.Context(), "git",
		strings.TrimPrefix(service, "git-"),
		"--stateless-rpc",
		"--advertise-refs",
		repoDir,
	)
	cmd.Stdout = w
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		s.logger.Error("info/refs git failed", "service", service, "repo", repoDir, "err", err)
	}
}

func (s *Server) handleServiceRPC(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/x-"+service+"-request" {
			http.Error(w, "unexpected content-type", http.StatusUnsupportedMediaType)
			return
		}

		repoDir, err := repoPath(s.cfg.ReposDir, r.PathValue("owner"), r.PathValue("repo"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := os.Stat(repoDir); err != nil {
			http.NotFound(w, r)
			return
		}

		body, err := decodeBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer body.Close()

		w.Header().Set("Content-Type", fmt.Sprintf("application/x-%s-result", service))
		w.Header().Set("Cache-Control", "no-cache")

		cmd := exec.CommandContext(r.Context(), "git",
			strings.TrimPrefix(service, "git-"),
			"--stateless-rpc",
			repoDir,
		)
		cmd.Stdin = body
		cmd.Stdout = w
		cmd.Stderr = os.Stderr
		if service == "git-receive-pack" {
			pusher, _, _ := r.BasicAuth()
			env, err := s.pushEnv(r.PathValue("owner"), strings.TrimSuffix(r.PathValue("repo"), ".git"), pusher)
			if err != nil {
				s.logger.Error("push env", "repo", repoDir, "err", err)
				http.Error(w, "cannot verify this repo's branch protection; push refused", http.StatusInternalServerError)
				return
			}
			cmd.Env = append(os.Environ(), env...)
		}
		if err := cmd.Run(); err != nil {
			s.logger.Error("service rpc git failed", "service", service, "repo", repoDir, "err", err)
		}
	}
}

// decodeBody handles gzip-encoded request bodies that git clients sometimes send.
func decodeBody(r *http.Request) (io.ReadCloser, error) {
	if r.Header.Get("Content-Encoding") != "gzip" {
		return r.Body, nil
	}
	gr, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, err
	}
	return &gzipBody{Reader: gr, src: r.Body}, nil
}

type gzipBody struct {
	*gzip.Reader
	src io.ReadCloser
}

func (g *gzipBody) Close() error {
	_ = g.Reader.Close()
	return g.src.Close()
}

// pktLine wraps payload in git's pkt-line framing: 4-byte hex length prefix
// (length includes the 4 bytes themselves) followed by payload.
func pktLine(payload string) []byte {
	n := len(payload) + 4
	return fmt.Appendf(nil, "%04x%s", n, payload)
}

// pushEnv builds the environment codefortd injects into `git receive-pack`.
// Two managed hooks read it: post-receive needs the loopback URL, the
// per-process CI secret, the repo identity, and the pusher; pre-receive needs
// the repo's branch-protection patterns. Passing the patterns in rather than
// letting the hook query keeps enforcement free of a network round-trip on
// every push.
//
// A repo with no row (on disk but never registered) has no patterns to read
// and therefore nothing to protect, so that is an empty list rather than an
// error. A real read failure is an error: failing open would silently
// unprotect a branch, and a refused push is recoverable where a rewritten
// main is not.
func (s *Server) pushEnv(owner, name, pusher string) ([]string, error) {
	patterns, err := storage.RepoProtectedRefs(s.rdb, owner, name)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	return []string{
		"CODEFORT_CI_URL=" + s.ciURL,
		"CODEFORT_CI_SECRET=" + s.ciSecret,
		"CODEFORT_CI_REPO=" + owner + "/" + name,
		"CODEFORT_CI_PUSHER=" + pusher,
		"CODEFORT_PROTECTED_REFS=" + patterns,
	}, nil
}
