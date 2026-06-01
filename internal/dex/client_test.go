package dex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// slowServer answers /v1 requests after `delay`, so a client whose per-op
// timeout is shorter than the delay fails and a longer one succeeds.
func slowServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case <-r.Context().Done():
		}
	}))
}

// Cheap calls use the short budget and Ask uses the long one (#127): a single
// shared 15s client timeout used to cap them all, 502-ing Explore's Ask even
// when dex was healthy. Here the server is slower than the cheap budget but
// faster than the ask budget, so Search must fail and Ask must succeed.
func TestAskGetsLongerBudgetThanCheapCalls(t *testing.T) {
	srv := slowServer(120 * time.Millisecond)
	defer srv.Close()

	c := New(srv.URL, "")
	c.timeout = 40 * time.Millisecond
	c.askTimeout = 2 * time.Second

	if _, err := c.Search(context.Background(), "proj", "q", 5); err == nil {
		t.Error("Search should have hit the cheap-call timeout, got nil error")
	}
	if _, err := c.Ask(context.Background(), "proj", "q", 5); err != nil {
		t.Errorf("Ask should fit within its longer budget, got %v", err)
	}
}

// Ask carries dex's synthesized prose answer + model attribution through
// (the headline of the new /ask shape) and flattens semantic_hits into the
// common Hit shape so the existing renderer can reuse them.
func TestAskParsesSynthesizedAnswer(t *testing.T) {
	const payload = `{
		"status": "ok",
		"intent": "behavior_search",
		"answer": "The dex client lives in internal/dex/client.go.",
		"answer_model": "qwen2.5-coder:14b",
		"next_action": "Read internal/dex/client.go lines 207-226.",
		"semantic_hits": [
			{"path": "internal/dex/client.go", "start_line": 207, "end_line": 226, "score": 0.6, "kind": "method", "reason": "Ask"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "").Ask(context.Background(), "proj", "where is the client?", 5)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if res.Answer != "The dex client lives in internal/dex/client.go." {
		t.Errorf("Answer not carried through, got %q", res.Answer)
	}
	if res.AnswerModel != "qwen2.5-coder:14b" {
		t.Errorf("AnswerModel not carried through, got %q", res.AnswerModel)
	}
	if res.NextAction == "" {
		t.Error("NextAction should be carried through")
	}
	if len(res.Hits) != 1 || res.Hits[0].Path != "internal/dex/client.go" {
		t.Errorf("semantic_hits not flattened into Hits, got %+v", res.Hits)
	}
	// Intent + per-hit reason fold into the hit's Role for the renderer.
	if res.Hits[0].Role != "behavior_search · Ask" {
		t.Errorf("hit Role = %q, want %q", res.Hits[0].Role, "behavior_search · Ask")
	}
}

// A deadline the caller already set is respected, not overridden by the
// cheap-call default.
func TestRespectsCallerDeadline(t *testing.T) {
	srv := slowServer(200 * time.Millisecond)
	defer srv.Close()

	c := New(srv.URL, "")
	c.timeout = 5 * time.Second // generous default…

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Search(ctx, "proj", "q", 5); err == nil {
		t.Error("caller's tight deadline should win over the default, got nil error")
	}
}
