package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenBucketBurstThenRefill(t *testing.T) {
	// 10/s with a burst of 3: three immediate requests pass, the fourth does
	// not, and the bucket refills at the stated rate.
	b := newTokenBucket(10, 3)
	for i := range 3 {
		if !b.allow() {
			t.Fatalf("request %d denied inside the burst", i+1)
		}
	}
	if b.allow() {
		t.Error("request 4 allowed; the burst should be spent")
	}

	// Rewind lastTick rather than sleeping — the refill is a pure function of
	// elapsed time, so a real wait would only make the test slow and flaky.
	b.mu.Lock()
	b.lastTick = b.lastTick.Add(-200 * time.Millisecond)
	b.mu.Unlock()
	if !b.allow() {
		t.Error("request denied after 200ms of refill at 10/s (want ~2 tokens back)")
	}
}

func TestTokenBucketRefillIsCappedAtBurst(t *testing.T) {
	// A long idle period must not bank unlimited credit, or a client that goes
	// quiet returns able to flood.
	b := newTokenBucket(10, 3)
	b.mu.Lock()
	b.lastTick = b.lastTick.Add(-time.Hour)
	b.mu.Unlock()

	for i := range 3 {
		if !b.allow() {
			t.Fatalf("request %d denied after a long idle", i+1)
		}
	}
	if b.allow() {
		t.Error("a long idle banked more than one burst of credit")
	}
}

func TestIPLimiterIsolatesClients(t *testing.T) {
	l := newIPLimiter(1, 1)
	if !l.allow("10.0.0.1") {
		t.Fatal("first request from .1 denied")
	}
	if l.allow("10.0.0.1") {
		t.Error("second request from .1 allowed; its burst is 1")
	}
	// A different IP has its own budget — otherwise one noisy client would
	// throttle everyone.
	if !l.allow("10.0.0.2") {
		t.Error("first request from .2 denied; buckets are not per-IP")
	}
}

func TestIPLimiterEvictsIdleBuckets(t *testing.T) {
	l := newIPLimiter(100, 100)
	l.allow("10.0.0.1")
	l.allow("10.0.0.2")

	// Age .1 past the TTL, leave .2 fresh.
	l.mu.Lock()
	old := l.buckets["10.0.0.1"]
	old.mu.Lock()
	old.lastTick = old.lastTick.Add(-2 * bucketIdleTTL)
	old.mu.Unlock()
	l.evictIdleLocked(time.Now(), "")
	_, staleKept := l.buckets["10.0.0.1"]
	_, freshKept := l.buckets["10.0.0.2"]
	l.mu.Unlock()

	if staleKept {
		t.Error("bucket idle past the TTL was kept; the map grows without bound")
	}
	if !freshKept {
		t.Error("a recently-used bucket was evicted; its client loses its rate state")
	}
}

func TestIPLimiterSweepKeepsTheCallerBucket(t *testing.T) {
	// The bucket the in-flight request just took must survive its own sweep,
	// even though the sweep runs before that bucket's lastTick is updated.
	l := newIPLimiter(100, 100)
	l.mu.Lock()
	l.buckets["10.0.0.9"] = newTokenBucket(l.rate, l.burst)
	l.buckets["10.0.0.9"].lastTick = time.Now().Add(-2 * bucketIdleTTL)
	l.evictIdleLocked(time.Now(), "10.0.0.9")
	_, kept := l.buckets["10.0.0.9"]
	l.mu.Unlock()
	if !kept {
		t.Error("the sweep evicted the bucket belonging to the request that triggered it")
	}
}

func TestIPLimiterSweepRunsOnSchedule(t *testing.T) {
	l := newIPLimiter(1e9, 1e9) // effectively unlimited; we are counting sweeps
	l.mu.Lock()
	l.buckets["stale"] = newTokenBucket(l.rate, l.burst)
	l.buckets["stale"].lastTick = time.Now().Add(-2 * bucketIdleTTL)
	l.mu.Unlock()

	for range sweepEvery {
		l.allow("10.0.0.1")
	}

	l.mu.Lock()
	_, kept := l.buckets["stale"]
	l.mu.Unlock()
	if kept {
		t.Errorf("stale bucket survived %d allow() calls; the sweep never fired", sweepEvery)
	}
}

func TestWithRateLimitRejectsOverBudget(t *testing.T) {
	s := &Server{limiter: newIPLimiter(1, 1)}
	h := s.withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	call := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/repos", http.NoBody)
		req.RemoteAddr = "10.0.0.1:4242"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	if got := call(); got != http.StatusOK {
		t.Fatalf("first call = %d, want 200", got)
	}
	if got := call(); got != http.StatusTooManyRequests {
		t.Errorf("second call = %d, want 429", got)
	}
}

func TestWithRateLimitDisabledIsPassThrough(t *testing.T) {
	// A nil limiter is how CODEFORT_RATE_LIMIT=0 disables the feature; it must
	// not merely be generous, it must not wrap at all.
	s := &Server{limiter: nil}
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	for range 50 {
		req := httptest.NewRequest(http.MethodGet, "/api/repos", http.NoBody)
		req.RemoteAddr = "10.0.0.1:4242"
		rr := httptest.NewRecorder()
		s.withRateLimit(inner).ServeHTTP(rr, req)
		if rr.Code != http.StatusTeapot {
			t.Fatalf("code = %d, want the inner handler's 418", rr.Code)
		}
	}
}
