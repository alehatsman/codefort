package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/alehatsman/codefort/internal/storage"
)

// Access control is enforced in exactly two places — the two middlewares in
// this file — rather than at the ~60 handler call sites that resolve a repo.
// One gate is auditable; sixty are not, and the first version of this feature
// shipped with the check functions written but never called, which is precisely
// the failure mode a per-call-site design invites.
//
// The posture stays the local-trust one described in specs/constitution.md: a
// valid token is still the bar, and every repo is `public` by default (the
// visibility column defaults to 'public', so an existing deployment is
// unaffected). Marking a repo private is an opt-in that makes these gates
// load-bearing for that repo only. This is coarse membership — owner / write /
// read — not the fine-grained RBAC matrix VISION.md rules out.

// principalFromContext returns the *account* identity an access check should
// resolve against, which is not always the same string as the attribution
// identity.
//
// A token minted by LoginUser is named "<user>-session" so a browser session
// can be revoked without touching the account's primary token. Its Name is
// therefore "alice-session" while the account it speaks for is "alice". Access
// decisions must use the account, or a user would lose access to their own
// private repo the moment they signed in through the web UI.
//
// Falls back to the token name for tokens with no linked account: the
// admin-provisioned `codefortd token create alice` path and per-run agent
// tokens. Those are matched by name, which is how repo ownership resolved
// before accounts existed.
func principalFromContext(r *http.Request) string {
	t, ok := TokenFromContext(r.Context())
	if !ok {
		return ""
	}
	if t.UserName != "" {
		return t.UserName
	}
	return t.Name
}

// repoFromPath extracts an owner/repo pair from a path whose first two
// segments name a repository, after an optional prefix. ok is false when the
// path is too short to name one — e.g. "/api/repos" (the list endpoint) or
// "/api/repos/alice" (owner only), neither of which is repo-scoped.
func repoFromPath(path, prefix string) (owner, repo string, ok bool) {
	rest := strings.TrimPrefix(strings.TrimPrefix(path, prefix), "/")
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), true
}

// isReadMethod reports whether a request only reads. Everything else needs
// write access; defaulting unknown methods to "write" keeps the gate
// fail-closed if a new verb is ever routed.
func isReadMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// withRepoAccess enforces repo visibility and membership on every repo-scoped
// /api route. Mount it inside withAuth, which puts the token on the context.
//
// Requests that don't name a repo pass straight through, as do repos that
// don't resolve — the handler emits its own 404 with the message clients
// already parse.
//
// A caller without read access gets the same 404 as a repo that doesn't exist.
// 403 would confirm the repo is there, which is exactly what "private" is
// supposed to hide.
func (s *Server) withRepoAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, repo, ok := repoFromPath(r.URL.Path, "/api/repos")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		repoID, err := storage.LookupRepo(s.rdb, owner, repo)
		if errors.Is(err, storage.ErrNotFound) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			s.logger.Error("access lookup repo", "owner", owner, "repo", repo, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		caller := principalFromContext(r)
		notFound := "repo not registered: " + owner + "/" + repo

		allowed, err := storage.CanAccessRepo(s.rdb, repoID, caller)
		if err != nil {
			s.logger.Error("access check", "repo", owner+"/"+repo, "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if !allowed {
			writeError(w, http.StatusNotFound, notFound)
			return
		}
		if !isReadMethod(r.Method) {
			canWrite, err := storage.CanWriteRepo(s.rdb, repoID, caller)
			if err != nil {
				s.logger.Error("write access check", "repo", owner+"/"+repo, "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			if !canWrite {
				// Read access is already established here, so the repo's
				// existence is not a secret from this caller — 403 is the
				// honest answer and tells them what to ask the owner for.
				writeError(w, http.StatusForbidden, "write access required for "+owner+"/"+repo)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// withGitRepoAccess enforces the same rules on git smart-HTTP.
//
// The git transport has no bearer-token middleware — it is deliberately open
// under local-trust, optionally behind a single shared Basic credential. That
// stays true for public repos: this middleware is a no-op for them, so a
// default deployment clones exactly as it did before.
//
// A private repo needs a per-caller identity, which the shared Basic credential
// cannot provide (it names a deployment, not a person). So identity here comes
// from a codefort token presented either as a Bearer header or — the way git
// clients actually authenticate — as the *password* of a Basic credential, with
// any username. That is the same token-over-git-HTTP pattern as a forge PAT.
func (s *Server) withGitRepoAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, repo, ok := repoFromPath(r.URL.Path, "")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		repoID, err := storage.LookupRepo(s.rdb, owner, repo)
		if errors.Is(err, storage.ErrNotFound) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			s.logger.Error("git access lookup repo", "owner", owner, "repo", repo, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		private, err := storage.RepoIsPrivate(s.rdb, repoID)
		if err != nil {
			s.logger.Error("git visibility lookup", "repo", owner+"/"+repo, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !private {
			next.ServeHTTP(w, r)
			return
		}

		caller, ok := s.gitIdentity(r)
		if !ok {
			// No usable credential at all. 401 + a Basic challenge is what
			// makes `git clone` prompt for a username/password instead of
			// failing outright.
			w.Header().Set("WWW-Authenticate", `Basic realm="codefort"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		allowed, err := storage.CanAccessRepo(s.rdb, repoID, caller)
		if err != nil {
			s.logger.Error("git access check", "repo", owner+"/"+repo, "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !allowed {
			// Credentials were presented and rejected — report the repo as
			// missing rather than forbidden, same no-leak rule as the API gate.
			http.NotFound(w, r)
			return
		}
		if isGitWrite(r) {
			canWrite, err := storage.CanWriteRepo(s.rdb, repoID, caller)
			if err != nil {
				s.logger.Error("git write access check", "repo", owner+"/"+repo, "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !canWrite {
				http.Error(w, "write access required for "+owner+"/"+repo, http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isGitWrite reports whether a smart-HTTP request pushes. Both the
// receive-pack RPC and the ref advertisement that precedes it count, so a
// read-only collaborator is turned away before the client uploads a pack.
func isGitWrite(r *http.Request) bool {
	if strings.HasSuffix(r.URL.Path, "/git-receive-pack") {
		return true
	}
	return strings.HasSuffix(r.URL.Path, "/info/refs") &&
		r.URL.Query().Get("service") == "git-receive-pack"
}

// gitIdentity resolves the principal behind a git request from a codefort
// token, accepted as a Bearer header or as the password half of Basic auth
// (git's only native credential shape). The Basic *username* is ignored: the
// token already names its identity, and git clients send arbitrary usernames.
//
// ok is false when no credential parses as a live token — including when the
// only credential present is the shared CODEFORT_BASIC_USER pair, which
// authenticates the deployment rather than a person.
func (s *Server) gitIdentity(r *http.Request) (string, bool) {
	raw, err := bearerToken(r)
	if err != nil {
		if _, pass, ok := r.BasicAuth(); ok {
			raw = pass
		} else {
			return "", false
		}
	}
	if raw == "" {
		return "", false
	}
	tok, err := storage.LookupToken(s.rdb, raw)
	if err != nil {
		return "", false
	}
	if tok.UserName != "" {
		return tok.UserName, true
	}
	return tok.Name, true
}
