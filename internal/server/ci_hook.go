package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alehatsman/codefort/internal/ci"
	"github.com/alehatsman/codefort/internal/storage"
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

// readPipelineAt reads mgitci.yml at sha from a bare repo, best-effort. ok=false
// means the file is absent at that commit, or git failed for any reason; the
// caller then falls through to enqueue and lets the runner gate/report. Mirrors
// gitReadPipeline (the runner's reader) but collapses every failure to "no
// pipeline to enforce here" since this is only the branch-filter pre-check.
func readPipelineAt(bareRepo, sha string) (raw []byte, ok bool) {
	out, err := exec.Command(
		"git", "--git-dir", bareRepo, "show", sha+":mgitci.yml",
	).Output()
	if err != nil {
		return nil, false
	}
	return out, true
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
[ -n "$CODEFORT_CI_URL" ] || exit 0
[ -n "$CODEFORT_CI_SECRET" ] || exit 0
[ -n "$CODEFORT_CI_REPO" ] || exit 0
# Escape a value for embedding in a JSON string: backslash first, then quote.
# Git ref names may contain " (and a token name is arbitrary), so interpolating
# raw would break the JSON or let a crafted ref inject fields.
je() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
while read -r old new ref; do
	body=$(printf '{"repo":"%s","old":"%s","new":"%s","ref":"%s","pusher":"%s"}' \
		"$(je "$CODEFORT_CI_REPO")" "$(je "$old")" "$(je "$new")" "$(je "$ref")" "$(je "${CODEFORT_CI_PUSHER:-}")")
	curl -fsS -m 5 -X POST "$CODEFORT_CI_URL/internal/ci/events" \
		-H "X-Moongit-CI-Secret: $CODEFORT_CI_SECRET" \
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

// preReceiveHook enforces branch protection at push time. Like the
// post-receive hook it is identical across repos and carries no state: the
// repo's patterns arrive as CODEFORT_PROTECTED_REFS, which moongitd injects
// into the receive-pack process it spawns. That keeps the hook free of any
// network call or database read, so a push neither waits on the daemon nor
// slips past protection when the daemon is unwell.
//
// Unlike post-receive it hard-fails: a non-zero exit rejects the whole push
// before any ref moves, which is the point.
const preReceiveHook = `#!/bin/sh
# moongit branch-protection hook — managed by moongitd; do not edit.
[ -n "$CODEFORT_PROTECTED_REFS" ] || exit 0

# A ref's "null" value is all-zeros, 40 hex digits under sha1 and 64 under
# sha256; testing for a non-zero character covers both without pinning a width.
is_null() { case "$1" in *[!0]*) return 1 ;; *) return 0 ;; esac; }

rc=0
while read -r old new ref; do
	# Only branches are protected; tags and other refs are out of scope.
	case "$ref" in refs/heads/*) ;; *) continue ;; esac
	branch=${ref#refs/heads/}

	# Patterns are newline-separated shell globs over the branch name. $pat is
	# deliberately unquoted in the case arm — that is what makes it a glob.
	protected=0
	oldifs=$IFS
	IFS='
'
	for pat in $CODEFORT_PROTECTED_REFS; do
		[ -n "$pat" ] || continue
		case "$branch" in
		$pat) protected=1; break ;;
		esac
	done
	IFS=$oldifs
	[ "$protected" = 1 ] || continue

	if is_null "$new"; then
		echo "moongit: '$branch' is protected — refusing to delete it" >&2
		rc=1
		continue
	fi
	# A branch that does not exist yet is being created, not rewritten.
	is_null "$old" && continue
	# Fast-forward: the old tip is still reachable from the new one.
	git merge-base --is-ancestor "$old" "$new" 2>/dev/null && continue

	echo "moongit: '$branch' is protected — refusing a non-fast-forward push" >&2
	echo "moongit: clear the protection pattern in repo settings to rewrite it" >&2
	rc=1
done
exit $rc
`

// WritePreReceiveHook installs (or refreshes) the branch-protection hook in a
// bare repo. Idempotent, same as WritePostReceiveHook.
func WritePreReceiveHook(bareRepo string) error {
	hooksDir := filepath.Join(bareRepo, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(hooksDir, "pre-receive"), []byte(preReceiveHook), 0o755)
}

// pinHooksPath sets the repo's own `core.hooksPath`, which is not redundant:
// git resolves that setting from the global config too, so a server whose git
// user has `core.hooksPath` set in ~/.gitconfig silently runs *those* hooks
// and none of moongit's — no CI on push, no branch protection, no error
// anywhere. Writing it per-repo pins the lookup to the directory moongitd
// manages.
func pinHooksPath(bareRepo string) error {
	hooksDir := filepath.Join(bareRepo, "hooks")
	out, err := exec.Command("git", "--git-dir", bareRepo, "config", "core.hooksPath", hooksDir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pin core.hooksPath: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// WriteManagedHooks installs every hook moongitd owns and pins the repo's hook
// path at them. Idempotent — called on every repo creation and by the
// install-hooks backfill, so an existing repo picks up new or changed hooks.
func WriteManagedHooks(bareRepo string) error {
	if err := WritePostReceiveHook(bareRepo); err != nil {
		return err
	}
	if err := WritePreReceiveHook(bareRepo); err != nil {
		return err
	}
	return pinHooksPath(bareRepo)
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

	// Auto-close open PRs whose head branch was merged into base via a direct
	// push (bypassing the PR merge endpoint). Best-effort: failures are logged
	// but never block CI enqueue or the push response.
	if branch, ok := strings.CutPrefix(req.Ref, "refs/heads/"); ok {
		bareRepo := filepath.Join(s.cfg.ReposDir, owner, name+".git")
		s.autoCloseMergedPRs(r.Context(), repoID, bareRepo, branch, req.Old, req.New)
	}

	run, queued, err := s.enqueueRefRun(repoID, owner, name, req.Ref, req.New, "push", req.Pusher)
	if err != nil {
		s.logger.Error("ci events: enqueue", "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	if !queued {
		// CI off for this repo, or the branch is filtered out: accept the
		// notification, enqueue nothing.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"run": run.Number})
}

// enqueueRefRun queues a CI run for ref, now pointing at sha, applying every
// gate a push applies. queued is false when CI is off for the repo or the
// pipeline's branch filter excludes the ref — both are ordinary outcomes, not
// errors, so callers report "nothing to build" rather than failing.
//
// It is shared by the push hook and the pull-request merge endpoint. A
// server-side merge advances the base ref with update-ref rather than
// receive-pack, so the post-receive hook never fires; without this the
// canonical branch would land merges with no CI at all, which is the one place
// it matters most.
func (s *Server) enqueueRefRun(repoID int64, owner, name, ref, sha, event, trigger string) (storage.CIRun, bool, error) {
	enabled, err := storage.RepoCIEnabled(s.db, repoID)
	if err != nil {
		return storage.CIRun{}, false, fmt.Errorf("ci enabled check: %w", err)
	}
	if !enabled {
		return storage.CIRun{}, false, nil
	}

	bareRepo := filepath.Join(s.cfg.ReposDir, owner, name+".git")

	// Honor the pipeline's on.push.branches filter at enqueue, mirroring the
	// CI-disabled path above: a ref the pipeline doesn't list enqueues nothing,
	// so filtered branches don't accumulate canceled runs. We only skip when
	// the pipeline parses cleanly AND its filter excludes the ref; an
	// unreadable / unparseable / absent pipeline still enqueues so the runner
	// surfaces the real outcome (parse error → errored run, no pipeline →
	// gated/canceled).
	if raw, ok := readPipelineAt(bareRepo, sha); ok {
		if p, perr := ci.Parse(raw); perr == nil && !p.On.Matches(ref) {
			s.logger.Info("ci run skipped (branch filter)", "repo", owner+"/"+name, "ref", ref)
			return storage.CIRun{}, false, nil
		}
	}

	// Supersede any active CI runs for this ref: a new commit makes them
	// pointless. Best-effort — a query failure or nil canceler doesn't block
	// the new enqueue.
	if s.agentCanceler != nil {
		if stale, qerr := storage.ActiveCIRunIDsForRef(s.db, repoID, ref); qerr != nil {
			s.logger.Error("ci: supersede query", "err", qerr)
		} else {
			for _, id := range stale {
				s.agentCanceler.CancelCIRun(id)
				s.logger.Info("ci run superseded", "run_id", id, "ref", ref, "new_sha", sha)
			}
		}
	}

	msg, author := gitCommitMeta(bareRepo, sha)
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		CommitSHA: sha, CommitMsg: msg, CommitAuthor: author,
		Ref: ref, Event: event, Trigger: trigger,
	})
	if err != nil {
		return storage.CIRun{}, false, fmt.Errorf("enqueue: %w", err)
	}
	s.emitRunQueued(repoID, run)
	s.logger.Info("ci run enqueued", "repo", owner+"/"+name, "run", run.Number, "ref", ref, "event", event)
	return run, true, nil
}

// isZeroSHA reports whether a git SHA is the all-zeros sentinel (a ref delete),
// for either sha1 or sha256 widths.
func isZeroSHA(sha string) bool {
	return sha == "" || strings.Trim(sha, "0") == ""
}

// autoCloseMergedPRs scans open pull requests that target baseRef and marks any
// whose head branch tip is now an ancestor of newBaseSHA as merged. This covers
// the direct-push path where the PR merge endpoint is bypassed. oldBaseSHA is
// the pre-push base tip; it is frozen alongside headSHA so the detail endpoint
// can reproduce the pre-merge diff (computeCompare(oldBaseSHA, headSHA)).
// Best-effort: each PR is attempted independently; a failure on one does not
// block others.
func (s *Server) autoCloseMergedPRs(ctx context.Context, repoID int64, bareRepo, baseRef, oldBaseSHA, newBaseSHA string) {
	prs, err := storage.OpenPRsForBase(s.db, repoID, baseRef)
	if err != nil {
		s.logger.Error("auto-close: list open PRs", "base", baseRef, "err", err)
		return
	}
	for _, pr := range prs {
		headSHA, err := revParse(ctx, bareRepo, "refs/heads/"+pr.HeadRef)
		if err != nil {
			// Head branch deleted or not yet pushed — skip.
			continue
		}
		if !isAncestor(ctx, bareRepo, headSHA, newBaseSHA) {
			continue
		}
		if _, err := storage.MarkMerged(s.db, repoID, pr.Number, oldBaseSHA, headSHA); err != nil {
			s.logger.Error("auto-close: mark merged", "pr", pr.Number, "err", err)
			continue
		}
		s.logger.Info("auto-closed PR (head merged via push)", "pr", pr.Number, "head", pr.HeadRef, "base", baseRef)
	}
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
