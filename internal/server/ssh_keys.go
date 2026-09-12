package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

// handleListSSHKeys returns the requesting token's registered SSH keys. Keys
// are scoped to their owning token, so a caller only ever sees its own — the
// token authenticates and also defines whose keys these are.
func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
	tok, ok := TokenFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no token on context")
		return
	}
	keys, err := storage.ListSSHKeys(s.rdb, tok.ID)
	if err != nil {
		s.logger.Error("list ssh keys", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// handleCreateSSHKey registers a public key against the requesting token. The
// key's token name becomes the push/pull identity over SSH. Unlike tokens
// there's no secret to surface once — the public half is returned in full.
func (s *Server) handleCreateSSHKey(w http.ResponseWriter, r *http.Request) {
	tok, ok := TokenFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no token on context")
		return
	}
	var req api.CreateSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	key, err := storage.AddSSHKey(s.db, tok.ID, req.PublicKey, req.Comment)
	if errors.Is(err, storage.ErrInvalidSSHKey) {
		writeError(w, http.StatusBadRequest, "invalid ssh public key")
		return
	}
	if errors.Is(err, storage.ErrSSHKeyExists) {
		writeError(w, http.StatusConflict, "ssh key already registered")
		return
	}
	if err != nil {
		s.logger.Error("create ssh key", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

// handleDeleteSSHKey removes one of the requesting token's keys by id. Deleting
// another token's key 404s — the delete is scoped to the owner, so an id the
// caller doesn't own is indistinguishable from a missing one.
func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	tok, ok := TokenFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no token on context")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid ssh key id")
		return
	}
	if err := storage.DeleteSSHKey(s.db, id, tok.ID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "ssh key not found")
		return
	} else if err != nil {
		s.logger.Error("delete ssh key", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
