package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

func newSSHKeyTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "ssh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return &Server{
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// asToken runs req through the handler with tok mounted on the context, the
// same way withAuth would for an authenticated /api request.
func asToken(s *Server, tok api.Token, h http.HandlerFunc, method, path string, body any) *httptest.ResponseRecorder {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, tok))
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

// genPubKeyLine returns a fresh ed25519 authorized_keys line.
func genPubKeyLine(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

func TestSSHKeyAPICreateListDelete(t *testing.T) {
	s := newSSHKeyTestServer(t)
	tok, err := storage.CreateToken(s.db, "alice", "cf_alice")
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	line := genPubKeyLine(t, "laptop")

	// Create.
	rr := asToken(s, tok, s.handleCreateSSHKey, http.MethodPost, "/api/ssh-keys",
		api.CreateSSHKeyRequest{PublicKey: line})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rr.Code, rr.Body.String())
	}
	var created api.SSHKey
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(created.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q, want SHA256: prefix", created.Fingerprint)
	}
	if created.TokenName != "alice" {
		t.Errorf("token name = %q, want alice", created.TokenName)
	}

	// List shows it.
	rr = asToken(s, tok, s.handleListSSHKeys, http.MethodGet, "/api/ssh-keys", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d", rr.Code)
	}
	var keys []api.SSHKey
	if err := json.Unmarshal(rr.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(keys))
	}

	// Delete. PathValue isn't populated when calling the handler directly, so
	// route it through the mux for the {id} to resolve.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/ssh-keys/"+strconv.FormatInt(created.ID, 10), nil)
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, tok))
	s.apiHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rr.Code, rr.Body.String())
	}
}

func TestSSHKeyAPIInvalidKey(t *testing.T) {
	s := newSSHKeyTestServer(t)
	tok, _ := storage.CreateToken(s.db, "alice", "cf_alice")
	rr := asToken(s, tok, s.handleCreateSSHKey, http.MethodPost, "/api/ssh-keys",
		api.CreateSSHKeyRequest{PublicKey: "garbage"})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body %s", rr.Code, rr.Body.String())
	}
}

func TestSSHKeyAPIScopedToCaller(t *testing.T) {
	s := newSSHKeyTestServer(t)
	alice, _ := storage.CreateToken(s.db, "alice", "cf_alice")
	bob, _ := storage.CreateToken(s.db, "bob", "cf_bob")

	// Alice registers a key.
	rr := asToken(s, alice, s.handleCreateSSHKey, http.MethodPost, "/api/ssh-keys",
		api.CreateSSHKeyRequest{PublicKey: genPubKeyLine(t, "")})
	if rr.Code != http.StatusCreated {
		t.Fatalf("alice create status = %d", rr.Code)
	}
	var aliceKey api.SSHKey
	_ = json.Unmarshal(rr.Body.Bytes(), &aliceKey)

	// Bob's list doesn't include it.
	rr = asToken(s, bob, s.handleListSSHKeys, http.MethodGet, "/api/ssh-keys", nil)
	var bobKeys []api.SSHKey
	_ = json.Unmarshal(rr.Body.Bytes(), &bobKeys)
	if len(bobKeys) != 0 {
		t.Errorf("bob sees %d keys, want 0", len(bobKeys))
	}

	// Bob can't delete it — 404, not 204.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/ssh-keys/"+strconv.FormatInt(aliceKey.ID, 10), nil)
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, bob))
	s.apiHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("bob delete status = %d, want 404", rr.Code)
	}
}
