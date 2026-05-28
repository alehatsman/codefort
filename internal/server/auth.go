package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

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

// withAuth wraps next, requiring a valid Bearer token on every request
// except the public allowlist (/healthz and the git smart-HTTP routes).
// Git transport auth lands in a later iteration alongside SSH support.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		raw, err := bearerToken(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		tok, err := storage.LookupToken(s.db, raw)
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if err != nil {
			s.logger.Error("token lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		ctx := context.WithValue(r.Context(), tokenCtxKey{}, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isPublicPath returns true for paths that bypass auth. Kept explicit
// (rather than prefix-matching) so adding a new public path is a
// conscious change.
func isPublicPath(path string) bool {
	if path == "/healthz" {
		return true
	}
	// Git smart-HTTP — unauthenticated for this iteration. SSH transport
	// + Basic auth for git lands separately.
	if strings.HasSuffix(path, "/info/refs") ||
		strings.HasSuffix(path, "/git-upload-pack") ||
		strings.HasSuffix(path, "/git-receive-pack") {
		return true
	}
	return false
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
