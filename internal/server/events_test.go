package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// parseSSEEvents pulls the api.Event payloads out of an SSE response body. Only
// data: lines carry the JSON event; id:/event:/comment lines are framing.
func parseSSEEvents(t *testing.T, body string) []api.Event {
	t.Helper()
	var out []api.Event
	for line := range strings.SplitSeq(body, "\n") {
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		var ev api.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &ev); err != nil {
			t.Fatalf("decode SSE data %q: %v", data, err)
		}
		out = append(out, ev)
	}
	return out
}

// jsonReq drives a JSON-body request through the API mux with an authenticated
// identity on the context (the emit calls stamp the actor from it).
func jsonReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, req)
	return rr
}

func TestEventsReplayAndOrder(t *testing.T) {
	s, repoID := newCIReadServer(t)
	if _, err := storage.AppendEvent(s.db, "issue.created", repoID, "alice", `{"number":1}`); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := storage.AppendEvent(s.db, "push", repoID, "bob", `{"ref":"main"}`); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	rr := ciReq(t, s, http.MethodGet, "/api/events?once=true", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	evs := parseSSEEvents(t, rr.Body.String())
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d (%s)", len(evs), rr.Body.String())
	}
	if evs[0].Type != "issue.created" || evs[1].Type != "push" {
		t.Fatalf("unexpected order: %+v", evs)
	}
	if evs[0].Repo != "alice/repo" || evs[0].Actor != "alice" {
		t.Errorf("repo/actor not resolved: %+v", evs[0])
	}
	if n, ok := evs[0].Data["number"]; !ok || n.(float64) != 1 {
		t.Errorf("payload not decoded into Data: %+v", evs[0].Data)
	}
}

func TestEventsResumeFromLastEventID(t *testing.T) {
	s, repoID := newCIReadServer(t)
	seq1, _ := storage.AppendEvent(s.db, "issue.created", repoID, "alice", `{"number":1}`)
	seq2, _ := storage.AppendEvent(s.db, "issue.claimed", repoID, "bob", `{"number":1}`)

	rr := ciReq(t, s, http.MethodGet, "/api/events?once=true", strconv.FormatInt(seq1, 10))
	evs := parseSSEEvents(t, rr.Body.String())
	if len(evs) != 1 || evs[0].Seq != seq2 {
		t.Fatalf("resume want [%d], got %+v", seq2, evs)
	}
}

func TestEventsRepoFilter(t *testing.T) {
	s, repoA := newCIReadServer(t)
	repoB, err := storage.EnsureRepo(s.db, "alice", "other")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if _, err := storage.AppendEvent(s.db, "push", repoA, "alice", `{}`); err != nil {
		t.Fatalf("AppendEvent A: %v", err)
	}
	if _, err := storage.AppendEvent(s.db, "push", repoB, "alice", `{}`); err != nil {
		t.Fatalf("AppendEvent B: %v", err)
	}

	rr := ciReq(t, s, http.MethodGet, "/api/events?once=true&repo=alice/other", "")
	evs := parseSSEEvents(t, rr.Body.String())
	if len(evs) != 1 || evs[0].Repo != "alice/other" {
		t.Fatalf("repo filter leaked: %+v", evs)
	}

	// An unknown repo filter is a clean 404, not an empty stream.
	rr = ciReq(t, s, http.MethodGet, "/api/events?once=true&repo=alice/ghost", "")
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown repo: code = %d, want 404", rr.Code)
	}
}

func TestEventsTypeFilter(t *testing.T) {
	s, repoID := newCIReadServer(t)
	if _, err := storage.AppendEvent(s.db, "push", repoID, "alice", `{}`); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := storage.AppendEvent(s.db, "issue.created", repoID, "alice", `{"number":1}`); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	rr := ciReq(t, s, http.MethodGet, "/api/events?once=true&types=issue.created", "")
	evs := parseSSEEvents(t, rr.Body.String())
	if len(evs) != 1 || evs[0].Type != "issue.created" {
		t.Fatalf("type filter leaked: %+v", evs)
	}
}

// TestEventsEmittedByMutations is the integration check: an issue create+claim
// through the real handlers leaves the matching events on the feed.
func TestEventsEmittedByMutations(t *testing.T) {
	s, _ := newCIReadServer(t)

	if rr := jsonReq(t, s, http.MethodPost, "/api/repos/alice/repo/issues", `{"title":"t"}`); rr.Code != http.StatusCreated {
		t.Fatalf("create issue: code = %d; body=%s", rr.Code, rr.Body.String())
	}
	if rr := jsonReq(t, s, http.MethodPost, "/api/repos/alice/repo/issues/1/claim", `{"state":"in_progress"}`); rr.Code != http.StatusOK {
		t.Fatalf("claim issue: code = %d; body=%s", rr.Code, rr.Body.String())
	}

	rr := ciReq(t, s, http.MethodGet, "/api/events?once=true", "")
	evs := parseSSEEvents(t, rr.Body.String())
	if len(evs) != 2 {
		t.Fatalf("want 2 emitted events, got %d (%s)", len(evs), rr.Body.String())
	}
	if evs[0].Type != "issue.created" || evs[1].Type != "issue.claimed" {
		t.Fatalf("unexpected emitted events: %+v", evs)
	}
	if evs[1].Actor != "agent#17" {
		t.Errorf("claim actor = %q, want agent#17 (token identity)", evs[1].Actor)
	}
}
