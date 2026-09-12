package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

// drivePull invokes a PR handler with the alice/proj path values, an
// authenticated identity, and (for /{number} routes) the number path value —
// httptest doesn't run the mux pattern matcher, so path values are set by hand.
func drivePull(t *testing.T, s *Server, h http.HandlerFunc, method, target, identity, number string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, rdr)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	if number != "" {
		req.SetPathValue("number", number)
	}
	if identity != "" {
		req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: identity}))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestHandleCreatePull(t *testing.T) {
	s := newCompareTestServer(t)
	rr := drivePull(t, s, s.handleCreatePull, http.MethodPost, "/api/repos/alice/proj/pulls", "agent#7", "",
		api.CreatePullRequest{Base: "main", Head: "feature", Title: "merge feature", Body: "please"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var pr api.PullRequest
	if err := json.Unmarshal(rr.Body.Bytes(), &pr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pr.Number != 1 || pr.State != api.PROpen {
		t.Errorf("pr = #%d %q, want #1 open", pr.Number, pr.State)
	}
	if pr.Author != "agent#7" {
		t.Errorf("author = %q, want agent#7 (token-stamped)", pr.Author)
	}
	if pr.BaseRef != "main" || pr.HeadRef != "feature" {
		t.Errorf("refs = %q/%q", pr.BaseRef, pr.HeadRef)
	}
}

func TestHandleCreatePullValidation(t *testing.T) {
	s := newCompareTestServer(t)
	tests := []struct {
		name string
		body api.CreatePullRequest
		want int
	}{
		{"missing base", api.CreatePullRequest{Head: "feature", Title: "t"}, http.StatusBadRequest},
		{"missing head", api.CreatePullRequest{Base: "main", Title: "t"}, http.StatusBadRequest},
		{"missing title", api.CreatePullRequest{Base: "main", Head: "feature"}, http.StatusBadRequest},
		{"same branch", api.CreatePullRequest{Base: "main", Head: "main", Title: "t"}, http.StatusBadRequest},
		{"unknown base", api.CreatePullRequest{Base: "nope", Head: "feature", Title: "t"}, http.StatusNotFound},
		{"unknown head", api.CreatePullRequest{Base: "main", Head: "nope", Title: "t"}, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := drivePull(t, s, s.handleCreatePull, http.MethodPost, "/api/repos/alice/proj/pulls", "agent#7", "", tt.body)
			if rr.Code != tt.want {
				t.Errorf("status = %d, want %d (body=%s)", rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}

func TestHandleListPulls(t *testing.T) {
	s := newCompareTestServer(t)
	for _, head := range []string{"feature"} {
		drivePull(t, s, s.handleCreatePull, http.MethodPost, "/api/repos/alice/proj/pulls", "agent#7", "",
			api.CreatePullRequest{Base: "main", Head: head, Title: "pr-" + head})
	}
	rr := drivePull(t, s, s.handleListPulls, http.MethodGet, "/api/repos/alice/proj/pulls", "agent#7", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var pulls []api.PullRequest
	if err := json.Unmarshal(rr.Body.Bytes(), &pulls); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pulls) != 1 {
		t.Fatalf("got %d pulls, want 1", len(pulls))
	}

	// ?state=closed filters it out.
	rr = drivePull(t, s, s.handleListPulls, http.MethodGet, "/api/repos/alice/proj/pulls?state=closed", "agent#7", "", nil)
	var closed []api.PullRequest
	json.Unmarshal(rr.Body.Bytes(), &closed)
	if len(closed) != 0 {
		t.Errorf("closed filter returned %d, want 0", len(closed))
	}

	// ?state=bogus is rejected.
	rr = drivePull(t, s, s.handleListPulls, http.MethodGet, "/api/repos/alice/proj/pulls?state=bogus", "agent#7", "", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad state status = %d, want 400", rr.Code)
	}

	// ?q= keyword-matches the title (case-insensitive).
	rr = drivePull(t, s, s.handleListPulls, http.MethodGet, "/api/repos/alice/proj/pulls?q=FEATURE", "agent#7", "", nil)
	var matched []api.PullRequest
	json.Unmarshal(rr.Body.Bytes(), &matched)
	if len(matched) != 1 {
		t.Errorf("q=FEATURE returned %d, want 1", len(matched))
	}

	// A non-matching ?q= returns nothing.
	rr = drivePull(t, s, s.handleListPulls, http.MethodGet, "/api/repos/alice/proj/pulls?q=nomatch", "agent#7", "", nil)
	var none []api.PullRequest
	json.Unmarshal(rr.Body.Bytes(), &none)
	if len(none) != 0 {
		t.Errorf("q=nomatch returned %d, want 0", len(none))
	}
}

func TestHandleGetPullDetailEmbedsCompareAndComments(t *testing.T) {
	s := newCompareTestServer(t)
	drivePull(t, s, s.handleCreatePull, http.MethodPost, "/api/repos/alice/proj/pulls", "agent#7", "",
		api.CreatePullRequest{Base: "main", Head: "feature", Title: "feature pr"})

	// A review comment anchored to the head branch should surface on the PR.
	repoID, err := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	if _, err := storage.CreateCodeComment(s.db, repoID, api.CreateCodeCommentRequest{
		Ref: "feature", Path: "feature.txt", StartLine: 1, EndLine: 1, Author: "agent#9", Body: "nit",
	}); err != nil {
		t.Fatalf("CreateCodeComment: %v", err)
	}

	rr := drivePull(t, s, s.handleGetPull, http.MethodGet, "/api/repos/alice/proj/pulls/1", "agent#7", "1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var detail api.PullRequestDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Embedded compare: feature is 1 ahead / 1 behind, diff is feature.txt.
	if detail.Compare.Ahead != 1 || detail.Compare.Behind != 1 {
		t.Errorf("compare ahead/behind = %d/%d, want 1/1", detail.Compare.Ahead, detail.Compare.Behind)
	}
	if len(detail.Compare.Files) != 1 || detail.Compare.Files[0].NewPath != "feature.txt" {
		t.Errorf("compare files = %+v, want [feature.txt]", detail.Compare.Files)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Body != "nit" {
		t.Errorf("comments = %+v, want one 'nit'", detail.Comments)
	}
}

func TestHandleGetPullNotFound(t *testing.T) {
	s := newCompareTestServer(t)
	rr := drivePull(t, s, s.handleGetPull, http.MethodGet, "/api/repos/alice/proj/pulls/99", "agent#7", "99", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleUpdatePull(t *testing.T) {
	s := newCompareTestServer(t)
	drivePull(t, s, s.handleCreatePull, http.MethodPost, "/api/repos/alice/proj/pulls", "agent#7", "",
		api.CreatePullRequest{Base: "main", Head: "feature", Title: "orig"})

	// Edit title + close.
	closed := api.PRClosed
	newTitle := "edited"
	rr := drivePull(t, s, s.handleUpdatePull, http.MethodPatch, "/api/repos/alice/proj/pulls/1", "agent#7", "1",
		api.UpdatePullRequest{Title: &newTitle, State: &closed})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var pr api.PullRequest
	json.Unmarshal(rr.Body.Bytes(), &pr)
	if pr.Title != "edited" || pr.State != api.PRClosed {
		t.Errorf("pr = %q/%q, want edited/closed", pr.Title, pr.State)
	}

	// state=merged is rejected via PATCH (merge has its own endpoint).
	merged := api.PRMerged
	rr = drivePull(t, s, s.handleUpdatePull, http.MethodPatch, "/api/repos/alice/proj/pulls/1", "agent#7", "1",
		api.UpdatePullRequest{State: &merged})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("merge-via-patch status = %d, want 400", rr.Code)
	}

	// Empty title is rejected.
	empty := "   "
	rr = drivePull(t, s, s.handleUpdatePull, http.MethodPatch, "/api/repos/alice/proj/pulls/1", "agent#7", "1",
		api.UpdatePullRequest{Title: &empty})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty-title status = %d, want 400", rr.Code)
	}

	// Unknown PR.
	rr = drivePull(t, s, s.handleUpdatePull, http.MethodPatch, "/api/repos/alice/proj/pulls/99", "agent#7", "99",
		api.UpdatePullRequest{Title: &newTitle})
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown-pr status = %d, want 404", rr.Code)
	}
}
