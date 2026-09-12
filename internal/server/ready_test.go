package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

func TestHandleListReadyFilter(t *testing.T) {
	s := newDepServer(t) // 3 issues: 1,2,3 — all plain todo leaves

	// Make #1 depend on #2 (unmet) → #1 blocked, #2/#3 ready.
	if rr := addDep(t, s, 1, 2); rr.Code != http.StatusOK {
		t.Fatalf("add dep: %d", rr.Code)
	}

	got := listFiltered(t, s, "ready=1")
	if got[1] {
		t.Errorf("#1 (blocked) must not be in --ready")
	}
	if !got[2] || !got[3] {
		t.Errorf("--ready should include #2 and #3, got %v", got)
	}

	blocked := listFiltered(t, s, "blocked=1")
	if !blocked[1] || blocked[2] || blocked[3] {
		t.Errorf("--blocked should be {#1}, got %v", blocked)
	}
}

func TestHandleListReadyBlockedMutuallyExclusive(t *testing.T) {
	s := newDepServer(t)
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues?ready=1&blocked=1", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("ready+blocked: status %d, want 400", rr.Code)
	}
}

// listFiltered hits the per-repo issue list with a raw query string and returns
// the set of issue numbers in the response.
func listFiltered(t *testing.T, s *Server, query string) map[int]bool {
	t.Helper()
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues?"+query, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list %q: status %d, body %s", query, rr.Code, rr.Body.String())
	}
	var issues []api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &issues); err != nil {
		t.Fatalf("decode: %v", err)
	}
	out := map[int]bool{}
	for _, iss := range issues {
		out[iss.Number] = true
	}
	return out
}
