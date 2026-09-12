package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

func newTokenTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Single connection serves as both pools in tests.
	return &Server{
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// do routes a request through the bare API mux so {id} path values resolve.
func do(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, req)
	return rr
}

func TestCreateListRevokeToken(t *testing.T) {
	s := newTokenTestServer(t)

	// Create reveals the plaintext exactly once.
	rr := do(t, s, http.MethodPost, "/api/tokens", api.CreateTokenRequest{Name: "ci-bot"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rr.Code, rr.Body.String())
	}
	var created api.CreatedToken
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !strings.HasPrefix(created.Secret, storage.TokenPrefix) {
		t.Errorf("secret %q missing prefix %q", created.Secret, storage.TokenPrefix)
	}
	if created.Name != "ci-bot" || created.ID == 0 {
		t.Errorf("unexpected token metadata: %+v", created.Token)
	}

	// The secret authenticates until revoked.
	if _, err := storage.LookupToken(s.rdb, created.Secret); err != nil {
		t.Fatalf("LookupToken on fresh token: %v", err)
	}

	// List omits the plaintext but includes the token.
	rr = do(t, s, http.MethodGet, "/api/tokens", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	if strings.Contains(rr.Body.String(), created.Secret) {
		t.Error("list leaked plaintext token")
	}
	var list []api.Token
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "ci-bot" {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Revoke by id; the token then stops authenticating.
	rr = do(t, s, http.MethodDelete, "/api/tokens/"+itoa(created.ID), nil)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body %s", rr.Code, rr.Body.String())
	}
	if _, err := storage.LookupToken(s.rdb, created.Secret); err == nil {
		t.Error("revoked token still authenticates")
	}
}

func TestCreateTokenValidation(t *testing.T) {
	s := newTokenTestServer(t)

	// Blank name → 400.
	if rr := do(t, s, http.MethodPost, "/api/tokens", api.CreateTokenRequest{Name: "  "}); rr.Code != http.StatusBadRequest {
		t.Errorf("blank name status = %d, want 400", rr.Code)
	}

	// Duplicate name → 409.
	if rr := do(t, s, http.MethodPost, "/api/tokens", api.CreateTokenRequest{Name: "dup"}); rr.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rr.Code)
	}
	if rr := do(t, s, http.MethodPost, "/api/tokens", api.CreateTokenRequest{Name: "dup"}); rr.Code != http.StatusConflict {
		t.Errorf("duplicate name status = %d, want 409", rr.Code)
	}
}

func TestRevokeUnknownToken(t *testing.T) {
	s := newTokenTestServer(t)
	if rr := do(t, s, http.MethodDelete, "/api/tokens/9999", nil); rr.Code != http.StatusNotFound {
		t.Errorf("revoke unknown status = %d, want 404", rr.Code)
	}
	if rr := do(t, s, http.MethodDelete, "/api/tokens/abc", nil); rr.Code != http.StatusBadRequest {
		t.Errorf("revoke non-numeric status = %d, want 400", rr.Code)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
