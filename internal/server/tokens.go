package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleListTokens returns metadata for every token (active and revoked).
// The plaintext is never recoverable, so this is safe to expose to any
// authenticated caller — same posture as the `moongitd token list` CLI.
// Tokens aren't owned by a user, so this is the full set, not a per-caller
// view.
func (s *Server) handleListTokens(w http.ResponseWriter, _ *http.Request) {
	tokens, err := storage.ListTokens(s.rdb)
	if err != nil {
		s.logger.Error("list tokens", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// handleCreateToken mints a new token and returns the plaintext exactly
// once, in the response body. The database only ever stores the hash, so a
// missed/closed response is unrecoverable — the UI must surface the secret
// immediately. Mirrors `moongitd token create`.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req api.CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "token name is required")
		return
	}
	if len(name) > 100 {
		writeError(w, http.StatusBadRequest, "token name too long (max 100)")
		return
	}

	plaintext, err := storage.GenerateTokenString()
	if err != nil {
		s.logger.Error("generate token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	tok, err := storage.CreateToken(s.db, name, plaintext)
	if errors.Is(err, storage.ErrTokenExists) {
		writeError(w, http.StatusConflict, "token name already exists: "+name)
		return
	}
	if err != nil {
		s.logger.Error("create token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, api.CreatedToken{Token: tok, Secret: plaintext})
}

// handleRevokeToken revokes a token by id. Revoked tokens stop
// authenticating immediately (LookupToken treats revoked as not-found).
// Idempotent only in the sense that a second call refreshes revoked_at;
// an unknown id is a 404.
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if err := storage.RevokeTokenByID(s.db, id); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "token not found")
		return
	} else if err != nil {
		s.logger.Error("revoke token", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
