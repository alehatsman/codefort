package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// These tests exist because the first cut of this feature shipped
// CanAccessRepo/CanWriteRepo with zero call sites: repo visibility and
// membership were stored, surfaced in the API and UI, and enforced nowhere.
// Each case below fails loudly if a gate is ever unwired again.

type accessFixture struct {
	s        *Server
	openID   int64 // alice/open     — public
	secretID int64 // alice/secret   — private
	aliceTok string
	bobTok   string
	carolTok string // read-only member of alice/secret
	daveTok  string // write member of alice/secret
	sessTok  string // alice's browser session: token name "alice-session"
}

func newAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "access.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	f := accessFixture{s: &Server{
		cfg:    &config.Config{},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}}

	var err2 error
	if f.openID, err2 = storage.EnsureRepo(db, "alice", "open"); err2 != nil {
		t.Fatalf("EnsureRepo open: %v", err2)
	}
	if f.secretID, err2 = storage.EnsureRepo(db, "alice", "secret"); err2 != nil {
		t.Fatalf("EnsureRepo secret: %v", err2)
	}
	if err := storage.SetRepoVisibility(db, f.secretID, "private"); err != nil {
		t.Fatalf("SetRepoVisibility: %v", err)
	}

	// Membership is an account concept: AddRepoMember resolves a users row, so
	// a token alone is not a member candidate. EnsureUser is what `codefortd
	// repo create` already does for an owner.
	for _, u := range []string{"bob", "carol", "dave"} {
		if _, err := storage.EnsureUser(db, u); err != nil {
			t.Fatalf("EnsureUser %s: %v", u, err)
		}
	}

	mint := func(name string) string {
		t.Helper()
		plain, err := storage.GenerateTokenString()
		if err != nil {
			t.Fatalf("GenerateTokenString: %v", err)
		}
		if _, err := storage.CreateToken(db, name, plain); err != nil {
			t.Fatalf("CreateToken %s: %v", name, err)
		}
		return plain
	}
	f.aliceTok = mint("alice")
	f.bobTok = mint("bob")
	f.carolTok = mint("carol")
	f.daveTok = mint("dave")

	if err := storage.AddRepoMember(db, f.secretID, "carol", "read"); err != nil {
		t.Fatalf("AddRepoMember carol: %v", err)
	}
	if err := storage.AddRepoMember(db, f.secretID, "dave", "write"); err != nil {
		t.Fatalf("AddRepoMember dave: %v", err)
	}

	// A browser session token: named "alice-session" (so it is revocable on its
	// own) but linked to the alice account. The gate must resolve the account.
	f.sessTok = mint("alice-session")
	var aliceUserID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE name = 'alice'`).Scan(&aliceUserID); err != nil {
		t.Fatalf("lookup alice: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE tokens SET user_id = ? WHERE name = 'alice-session'`, aliceUserID,
	); err != nil {
		t.Fatalf("link session token: %v", err)
	}
	return f
}

// api sends an authenticated request through the real middleware stack:
// withAuth puts the token on the context, withRepoAccess gates on it.
func (f accessFixture) api(t *testing.T, method, path, token string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, http.NoBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	f.s.withAuth(f.s.withRepoAccess(f.s.apiHandler())).ServeHTTP(rr, req)
	return rr.Code
}

func TestRepoAccessPublicRepoIsOpen(t *testing.T) {
	f := newAccessFixture(t)
	// The local-trust default: any valid token reads and writes a public repo.
	// If this breaks, the gate has changed the default deployment's behavior.
	if code := f.api(t, http.MethodGet, "/api/repos/alice/open/issues", f.bobTok); code != http.StatusOK {
		t.Errorf("stranger GET public repo = %d, want 200", code)
	}
}

func TestRepoAccessPrivateRepoHidesFromStrangers(t *testing.T) {
	f := newAccessFixture(t)

	if code := f.api(t, http.MethodGet, "/api/repos/alice/secret/issues", f.aliceTok); code != http.StatusOK {
		t.Errorf("owner GET private repo = %d, want 200", code)
	}
	// 404, not 403: a 403 would confirm the repo exists, which is the one
	// thing "private" is supposed to hide.
	if code := f.api(t, http.MethodGet, "/api/repos/alice/secret/issues", f.bobTok); code != http.StatusNotFound {
		t.Errorf("stranger GET private repo = %d, want 404", code)
	}
	if code := f.api(t, http.MethodPost, "/api/repos/alice/secret/issues", f.bobTok); code != http.StatusNotFound {
		t.Errorf("stranger POST private repo = %d, want 404", code)
	}
}

func TestRepoAccessReadMemberCannotWrite(t *testing.T) {
	f := newAccessFixture(t)

	if code := f.api(t, http.MethodGet, "/api/repos/alice/secret/issues", f.carolTok); code != http.StatusOK {
		t.Errorf("read member GET = %d, want 200", code)
	}
	// Read access is already established, so the repo is no secret from carol.
	// 403 is the honest answer and names what to ask the owner for.
	if code := f.api(t, http.MethodPost, "/api/repos/alice/secret/issues", f.carolTok); code != http.StatusForbidden {
		t.Errorf("read member POST = %d, want 403", code)
	}
	if code := f.api(t, http.MethodGet, "/api/repos/alice/secret/issues", f.daveTok); code != http.StatusOK {
		t.Errorf("write member GET = %d, want 200", code)
	}
}

func TestRepoAccessSessionTokenResolvesToItsAccount(t *testing.T) {
	f := newAccessFixture(t)
	// LoginUser names session tokens "<user>-session". Matching on the token
	// name would lock alice out of her own private repo the moment she signed
	// in through the web UI; the gate resolves tokens.user_id instead.
	if code := f.api(t, http.MethodGet, "/api/repos/alice/secret/issues", f.sessTok); code != http.StatusOK {
		t.Errorf("owner session token GET private repo = %d, want 200", code)
	}
}

func TestRepoAccessIgnoresNonRepoPaths(t *testing.T) {
	f := newAccessFixture(t)
	for _, path := range []string{"/api/repos", "/api/tokens", "/api/whoami"} {
		if code := f.api(t, http.MethodGet, path, f.bobTok); code == http.StatusNotFound {
			t.Errorf("%s = 404; the gate should not claim non-repo-scoped paths", path)
		}
	}
}

// git drives the smart-HTTP gate in isolation: a sentinel stands in for the
// real git handler so a 200 means "the gate let it through", not "a bare repo
// happened to exist on disk".
func (f accessFixture) git(t *testing.T, path string, auth func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	if auth != nil {
		auth(req)
	}
	rr := httptest.NewRecorder()
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	f.s.withGitRepoAccess(sentinel).ServeHTTP(rr, req)
	return rr
}

func TestGitAccessPublicRepoStaysOpen(t *testing.T) {
	f := newAccessFixture(t)
	// No credential at all — the open-by-default clone path must not regress.
	rr := f.git(t, "/alice/open/info/refs?service=git-upload-pack", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("anonymous clone of public repo = %d, want 200", rr.Code)
	}
}

func TestGitAccessPrivateRepoChallengesAnonymous(t *testing.T) {
	f := newAccessFixture(t)
	rr := f.git(t, "/alice/secret/info/refs?service=git-upload-pack", nil)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous clone of private repo = %d, want 401", rr.Code)
	}
	// The challenge is what makes `git clone` prompt instead of failing.
	if got := rr.Header().Get("WWW-Authenticate"); got == "" {
		t.Error("401 without a WWW-Authenticate challenge; git will not prompt")
	}
}

func TestGitAccessPrivateRepoAcceptsTokenAsBasicPassword(t *testing.T) {
	f := newAccessFixture(t)
	// Git has no bearer support, so the token rides as the Basic password with
	// an arbitrary username — the same shape as a forge PAT.
	rr := f.git(t, "/alice/secret/info/refs?service=git-upload-pack", func(r *http.Request) {
		r.SetBasicAuth("git", f.aliceTok)
	})
	if rr.Code != http.StatusOK {
		t.Errorf("owner clone of private repo via Basic = %d, want 200", rr.Code)
	}

	rr = f.git(t, "/alice/secret/info/refs?service=git-upload-pack", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+f.aliceTok)
	})
	if rr.Code != http.StatusOK {
		t.Errorf("owner clone of private repo via Bearer = %d, want 200", rr.Code)
	}
}

func TestGitAccessPrivateRepoRejectsStranger(t *testing.T) {
	f := newAccessFixture(t)
	// Credentials were presented and rejected: report missing, not forbidden.
	rr := f.git(t, "/alice/secret/info/refs?service=git-upload-pack", func(r *http.Request) {
		r.SetBasicAuth("git", f.bobTok)
	})
	if rr.Code != http.StatusNotFound {
		t.Errorf("stranger clone of private repo = %d, want 404", rr.Code)
	}
}

func TestGitAccessReadMemberCannotPush(t *testing.T) {
	f := newAccessFixture(t)
	basic := func(tok string) func(*http.Request) {
		return func(r *http.Request) { r.SetBasicAuth("git", tok) }
	}

	// The receive-pack advertisement is refused too, so a read-only
	// collaborator is turned away before uploading a pack.
	rr := f.git(t, "/alice/secret/info/refs?service=git-receive-pack", basic(f.carolTok))
	if rr.Code != http.StatusForbidden {
		t.Errorf("read member push advertisement = %d, want 403", rr.Code)
	}
	rr = f.git(t, "/alice/secret/git-receive-pack", basic(f.carolTok))
	if rr.Code != http.StatusForbidden {
		t.Errorf("read member receive-pack = %d, want 403", rr.Code)
	}
	// The same member still fetches fine.
	rr = f.git(t, "/alice/secret/info/refs?service=git-upload-pack", basic(f.carolTok))
	if rr.Code != http.StatusOK {
		t.Errorf("read member fetch = %d, want 200", rr.Code)
	}
	rr = f.git(t, "/alice/secret/git-receive-pack", basic(f.daveTok))
	if rr.Code != http.StatusOK {
		t.Errorf("write member receive-pack = %d, want 200", rr.Code)
	}
}

func TestRepoFromPath(t *testing.T) {
	tests := []struct {
		name, path, prefix string
		wantOwner, wantRep string
		wantOK             bool
	}{
		{"api repo root", "/api/repos/alice/widgets", "/api/repos", "alice", "widgets", true},
		{"api nested", "/api/repos/alice/widgets/issues/7/comments", "/api/repos", "alice", "widgets", true},
		{"api list endpoint", "/api/repos", "/api/repos", "", "", false},
		{"api owner only", "/api/repos/alice", "/api/repos", "", "", false},
		{"api trailing slash", "/api/repos/alice/", "/api/repos", "", "", false},
		{"git upload pack", "/alice/widgets/git-upload-pack", "", "alice", "widgets", true},
		{"git dot-git suffix", "/alice/widgets.git/info/refs", "", "alice", "widgets", true},
		{"git too short", "/alice", "", "", "", false},
		{"root", "/", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := repoFromPath(tt.path, tt.prefix)
			if ok != tt.wantOK || owner != tt.wantOwner || repo != tt.wantRep {
				t.Errorf("repoFromPath(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.path, tt.prefix, owner, repo, ok, tt.wantOwner, tt.wantRep, tt.wantOK)
			}
		})
	}
}
