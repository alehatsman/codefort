package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

// handleRegister creates a new user account with a password and returns a
// bearer token. This endpoint is intentionally public (no withAuth wrapper)
// so new users can self-register.
//
// POST /api/auth/register
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	_, tok, plain, err := storage.RegisterUser(s.db, req.Username, req.Password)
	if errors.Is(err, storage.ErrUserExists) {
		writeError(w, http.StatusConflict, "username already taken")
		return
	}
	if err != nil {
		s.logger.Error("register user", "err", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api.AuthResponse{Token: tok, Secret: plain})
}

// handleLogin validates a username/password pair and returns a fresh bearer
// token for the session. Public endpoint.
//
// POST /api/auth/login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	tok, plain, err := storage.LoginUser(s.db, req.Username, req.Password)
	if errors.Is(err, storage.ErrBadCredentials) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if errors.Is(err, storage.ErrNoPassword) {
		writeError(w, http.StatusUnauthorized, "user has no password set; use a token instead")
		return
	}
	if err != nil {
		s.logger.Error("login", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, api.AuthResponse{Token: tok, Secret: plain})
}

// handleGetUser returns a user's public profile.
//
// GET /api/users/{username}
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	u, err := storage.GetUser(s.rdb, username)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "user not found: "+username)
		return
	}
	if err != nil {
		s.logger.Error("get user", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}
