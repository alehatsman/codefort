package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alehatsman/moongit/internal/storage"
)

// gitCommitMeta reads a commit's subject line and author name from a bare repo,
// best-effort: any failure (unknown sha, git error) yields empty strings so a
// run still enqueues with just its SHA. The subject + author are frozen onto
// the run row at enqueue so the UI can show what a run was for, not just a SHA.
func gitCommitMeta(bareRepo, sha string) (subject, author string) {
	out, err := exec.Command(
		"git", "--git-dir", bareRepo, "show", "-s", "--format=%s%n%an", sha,
	).Output()
	if err != nil {
		return "", ""
	}
	subject, author, _ = strings.Cut(strings.TrimRight(string(out), "\n"), "\n")
	return subject, author
}

// postReceiveHook is the generic hook installed into every bare repo. It
// notifies the local moongitd of each pushed ref so the daemon can enqueue a
// CI run. It is identical across repos: the per-repo identity and the secret
// arrive via environment variables that moongitd injects into the
// `git receive-pack` process at push time (the hook inherits them), so no
// secret is ever written to disk. It soft-fails — a CI notification problem
// must never block a push.
const postReceiveHook = `#!/bin/sh
# moongit CI post-receive hook — managed by moongitd; do not edit.
[ -n "$MOONGIT_CI_URL" ] || exit 0
[ -n "$MOONGIT_CI_SECRET" ] || exit 0
[ -n "$MOONGIT_CI_REPO" ] || exit 0
while read -r old new ref; do
	body=$(printf '{"repo":"%s","old":"%s","new":"%s","ref":"%s","pusher":"%s"}' \
		"$MOONGIT_CI_REPO" "$old" "$new" "$ref" "${MOONGIT_CI_PUSHER:-}")
	curl -fsS -m 5 -X POST "$MOONGIT_CI_URL/internal/ci/events" \
		-H "X-Moongit-CI-Secret: $MOONGIT_CI_SECRET" \
		-H "Content-Type: application/json" \
		-d "$body" >/dev/null 2>&1 || true
done
`

// WritePostReceiveHook installs (or refreshes) the post-receive hook in a bare
// repo. Idempotent — safe to call on every repo creation and from the
// install-hooks backfill.
func WritePostReceiveHook(bareRepo string) error {
	hooksDir := filepath.Join(bareRepo, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hooksDir, "post-receive"), []byte(postReceiveHook), 0o755)
}

// generateCISecret returns a fresh 256-bit hex secret for the loopback CI
// endpoint. The secret is per-process: the same value is injected into the
// push hook's environment and checked by the endpoint, so it never needs to
// be persisted.
func generateCISecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// loopbackURL derives the loopback base URL the hook should POST to from the
// server's listen address. A wildcard/empty host becomes 127.0.0.1.
func loopbackURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = "", strings.TrimPrefix(addr, ":")
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// ciEventRequest is the body the post-receive hook POSTs per pushed ref.
type ciEventRequest struct {
	Repo   string `json:"repo"` // owner/name
	Old    string `json:"old"`
	New    string `json:"new"`
	Ref    string `json:"ref"`
	Pusher string `json:"pusher"`
}

// handleCIEvents receives a pushed-ref notification from the post-receive hook
// and enqueues a CI run. It lives off the /api Bearer surface: it is
// loopback-only and gated by the per-process CI secret. A push to a repo with
// CI disabled is accepted but enqueues nothing, so non-CI repos don't
// accumulate canceled runs. Branch deletes (zero new SHA) are ignored.
func (s *Server) handleCIEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopback(r.RemoteAddr) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.ciSecret == "" ||
		subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Moongit-CI-Secret")), []byte(s.ciSecret)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req ciEventRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// Branch delete — nothing to build.
	if isZeroSHA(req.New) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	owner, name, ok := strings.Cut(req.Repo, "/")
	if !ok || owner == "" || name == "" {
		http.Error(w, "bad repo", http.StatusBadRequest)
		return
	}

	repoID, err := storage.LookupRepo(s.db, owner, name)
	if err != nil {
		http.Error(w, "unknown repo", http.StatusNotFound)
		return
	}

	// Surface the push on the fleet feed before the CI gate, so pushes to
	// CI-disabled repos still notify subscribers.
	s.emit("push", repoID, req.Pusher, map[string]any{"ref": req.Ref, "before": req.Old, "after": req.New})

	enabled, err := storage.RepoCIEnabled(s.db, repoID)
	if err != nil {
		s.logger.Error("ci events: enabled check", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	if !enabled {
		// CI off for this repo: accept the notification, enqueue nothing.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	bareRepo := filepath.Join(s.cfg.ReposDir, owner, name+".git")
	msg, author := gitCommitMeta(bareRepo, req.New)
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		CommitSHA: req.New, CommitMsg: msg, CommitAuthor: author,
		Ref: req.Ref, Event: "push", Trigger: req.Pusher,
	})
	if err != nil {
		s.logger.Error("ci events: enqueue", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.emitRunQueued(repoID, run)
	s.logger.Info("ci run enqueued", "repo", req.Repo, "run", run.Number, "ref", req.Ref)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"run": run.Number})
}

// isZeroSHA reports whether a git SHA is the all-zeros sentinel (a ref delete),
// for either sha1 or sha256 widths.
func isZeroSHA(sha string) bool {
	return sha == "" || strings.Trim(sha, "0") == ""
}

// isLoopback reports whether a request's RemoteAddr is a loopback IP.
func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
