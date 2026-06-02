package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

func newCITestServer(t *testing.T, secret string) (*Server, int64) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "ci.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := storage.EnsureRepo(db, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	s := &Server{
		cfg:      &config.Config{},
		db:       db,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		ciSecret: secret,
		ciURL:    "http://127.0.0.1:8080",
	}
	return s, repoID
}

func ciPost(t *testing.T, s *Server, remote, secret string, body ciEventRequest) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/internal/ci/events", bytes.NewReader(b))
	req.RemoteAddr = remote
	if secret != "" {
		req.Header.Set("X-Moongit-CI-Secret", secret)
	}
	rr := httptest.NewRecorder()
	s.handleCIEvents(rr, req)
	return rr
}

func runCount(t *testing.T, s *Server, repoID int64) int {
	t.Helper()
	runs, err := storage.ListRuns(s.db, repoID, storage.RunFilter{Limit: 100})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	return len(runs)
}

func TestHandleCIEventsEnqueues(t *testing.T) {
	s, repoID := newCITestServer(t, "sekret")
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatal(err)
	}
	rr := ciPost(t, s, "127.0.0.1:5555", "sekret", ciEventRequest{
		Repo: "alice/repo", Old: "old", New: "abc123", Ref: "refs/heads/main", Pusher: "alice",
	})
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	runs, _ := storage.ListRuns(s.db, repoID, storage.RunFilter{Limit: 10})
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.CommitSHA != "abc123" || got.Ref != "refs/heads/main" || got.Trigger != "alice" || got.Event != "push" {
		t.Errorf("enqueued run = %+v", got)
	}
	if got.Status != storage.RunQueued {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

func TestHandleCIEventsBadSecret(t *testing.T) {
	s, repoID := newCITestServer(t, "sekret")
	storage.SetRepoCIEnabled(s.db, repoID, true)
	rr := ciPost(t, s, "127.0.0.1:5555", "wrong", ciEventRequest{Repo: "alice/repo", New: "abc"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", rr.Code)
	}
	if n := runCount(t, s, repoID); n != 0 {
		t.Errorf("runs = %d, want 0 (rejected)", n)
	}
}

func TestHandleCIEventsNonLoopbackRejected(t *testing.T) {
	s, repoID := newCITestServer(t, "sekret")
	storage.SetRepoCIEnabled(s.db, repoID, true)
	rr := ciPost(t, s, "192.0.2.1:1234", "sekret", ciEventRequest{Repo: "alice/repo", New: "abc"})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403 (non-loopback)", rr.Code)
	}
	if n := runCount(t, s, repoID); n != 0 {
		t.Errorf("runs = %d, want 0", n)
	}
}

func TestHandleCIEventsZeroSHAIgnored(t *testing.T) {
	s, repoID := newCITestServer(t, "sekret")
	storage.SetRepoCIEnabled(s.db, repoID, true)
	rr := ciPost(t, s, "127.0.0.1:5555", "sekret", ciEventRequest{
		Repo: "alice/repo", New: strings.Repeat("0", 40), Ref: "refs/heads/gone",
	})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204 (branch delete)", rr.Code)
	}
	if n := runCount(t, s, repoID); n != 0 {
		t.Errorf("runs = %d, want 0 (delete ignored)", n)
	}
}

func TestHandleCIEventsDisabledRepoEnqueuesNothing(t *testing.T) {
	s, repoID := newCITestServer(t, "sekret") // CI left disabled
	rr := ciPost(t, s, "127.0.0.1:5555", "sekret", ciEventRequest{Repo: "alice/repo", New: "abc"})
	if rr.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204 (CI disabled)", rr.Code)
	}
	if n := runCount(t, s, repoID); n != 0 {
		t.Errorf("runs = %d, want 0 (disabled repo)", n)
	}
}

func TestHandleCIEventsUnknownRepo(t *testing.T) {
	s, _ := newCITestServer(t, "sekret")
	rr := ciPost(t, s, "127.0.0.1:5555", "sekret", ciEventRequest{Repo: "ghost/x", New: "abc"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}

func TestWritePostReceiveHook(t *testing.T) {
	dir := t.TempDir()
	if err := WritePostReceiveHook(dir); err != nil {
		t.Fatalf("WritePostReceiveHook: %v", err)
	}
	p := filepath.Join(dir, "hooks", "post-receive")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("hook mode = %v, want executable", info.Mode().Perm())
	}
	body, _ := os.ReadFile(p)
	for _, want := range []string{"curl", "/internal/ci/events", "X-Moongit-CI-Secret", "MOONGIT_CI_REPO", "MOONGIT_CI_SECRET"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("hook script missing %q", want)
		}
	}
}

func TestLoopbackURL(t *testing.T) {
	cases := map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		"127.0.0.1:7000": "http://127.0.0.1:7000",
	}
	for in, want := range cases {
		if got := loopbackURL(in); got != want {
			t.Errorf("loopbackURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsZeroSHA(t *testing.T) {
	for _, z := range []string{"", strings.Repeat("0", 40), strings.Repeat("0", 64)} {
		if !isZeroSHA(z) {
			t.Errorf("isZeroSHA(%q) = false, want true", z)
		}
	}
	for _, nz := range []string{"abc123", "0abc", strings.Repeat("0", 39) + "1"} {
		if isZeroSHA(nz) {
			t.Errorf("isZeroSHA(%q) = true, want false", nz)
		}
	}
}
