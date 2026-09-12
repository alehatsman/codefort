package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

// tokenTouchInterval debounces last_used_at writes: a token used again within
// this window doesn't trigger another writer UPDATE. A read-heavy polling fleet
// would otherwise turn every authed request into a serialized write contending
// with real mutations on the single writer pool, while last_used_at only needs
// coarse "recently used" granularity.
const tokenTouchInterval = 5 * time.Minute

// tokenCtxKey is the context key under which the authenticated token's
// metadata is stored. Use TokenFromContext to read it back from a handler.
type tokenCtxKey struct{}

// TokenFromContext returns the authenticated token's metadata. ok is
// false when no token is on the context (e.g., on public routes that
// didn't go through withAuth, or on tests).
func TokenFromContext(ctx context.Context) (api.Token, bool) {
	t, ok := ctx.Value(tokenCtxKey{}).(api.Token)
	return t, ok
}

// withAuth requires a valid Bearer token on every request. Mount it only
// on the /api/* sub-mux — /healthz and the git smart-HTTP routes live
// on the root mux and skip auth entirely.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := bearerToken(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		tok, err := storage.LookupToken(s.rdb, raw)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if err != nil {
			s.logger.Error("token lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Record usage on the writer; best-effort, off the auth critical path.
		// Debounced (see tokenTouchInterval) so a polling fleet doesn't amplify
		// reads into a per-request writer UPDATE.
		if tok.LastUsedAt == nil || time.Since(*tok.LastUsedAt) >= tokenTouchInterval {
			storage.TouchToken(s.db, tok.ID)
		}

		ctx := context.WithValue(r.Context(), tokenCtxKey{}, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withBasicAuth gates a handler behind a single HTTP Basic credential
// when one is configured (MOONGIT_BASIC_USER). It's a no-op when unset,
// preserving the open-by-default posture. Used to protect the web UI and
// git smart-HTTP on a shared network; the /api surface keeps its own
// Bearer-token auth and is not wrapped by this. Comparisons are
// constant-time to avoid leaking the credential via timing.
func (s *Server) withBasicAuth(next http.Handler) http.Handler {
	if s.cfg.BasicUser == "" {
		return next
	}
	wantUser := []byte(s.cfg.BasicUser)
	wantPass := []byte(s.cfg.BasicPass)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(u), wantUser) == 1
		passOK := subtle.ConstantTimeCompare([]byte(p), wantPass) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="moongit"`)
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleWhoami returns the authenticated token's identity. UI uses this
// to render "you" indicators and decide what mutations to surface.
func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	tok, ok := TokenFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no token on context")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Name string `json:"name"`
	}{Name: tok.Name})
}

// identityFromContext returns the authenticated token's name, used to
// stamp the canonical author/assignee on writes. Returns "anonymous"
// when no token is on the context — this only happens on public
// (unauthenticated) routes, which shouldn't be calling this.
func identityFromContext(r *http.Request) string {
	if t, ok := TokenFromContext(r.Context()); ok {
		return t.Name
	}
	return "anonymous"
}

// bearerToken extracts a token from the Authorization header, supporting
// only the Bearer scheme. Returns an error suitable for the 401 body.
func bearerToken(r *http.Request) (string, error) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", errors.New("missing Authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", errors.New("Authorization header must use Bearer scheme")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if raw == "" {
		return "", errors.New("empty bearer token")
	}
	return raw, nil
}
