package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// tokenBucket is a minimal per-IP rate limiter backed by stdlib only.
// It uses the leaky-bucket algorithm: tokens refill at rate r/s up to a
// burst of b tokens. Concurrent-safe.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	rate     float64 // tokens per nanosecond
	lastTick time.Time
}

func newTokenBucket(perSecond float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rate:     perSecond / 1e9, // convert to per-nanosecond
		lastTick: time.Now(),
	}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := float64(now.Sub(b.lastTick))
	b.tokens = min(b.maxBurst, b.tokens+elapsed*b.rate)
	b.lastTick = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// bucketIdleTTL is how long an IP's bucket is kept after its last request.
// It must exceed the time a bucket needs to refill to full, or eviction would
// hand a heavy client a fresh full burst — comfortably true here, since a
// bucket refills in burst/rate seconds (3s at the defaults) and this is an
// hour.
const bucketIdleTTL = time.Hour

// sweepEvery is how many allow() calls pass between eviction sweeps. Sweeping
// on a counter rather than a background goroutine keeps the limiter a plain
// value with no lifecycle to manage — it costs nothing when the server is idle,
// which is exactly when there is nothing to reclaim.
const sweepEvery = 1024

// ipLimiter holds per-IP token buckets. Keys are the client IP string.
//
// The map is swept, not unbounded: one entry per distinct client IP, never
// reclaimed, is fine on a trusted LAN and a slow memory leak on any interface
// that sees churn (a NAT range, a scanner, or simply a long uptime).
type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64
	burst   int
	// calls counts allow() invocations since the last sweep.
	calls int
}

func newIPLimiter(perSecond float64, burst int) *ipLimiter {
	return &ipLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    perSecond,
		burst:   burst,
	}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	b, ok := l.buckets[ip]
	if !ok {
		b = newTokenBucket(l.rate, l.burst)
		l.buckets[ip] = b
	}
	l.calls++
	if l.calls >= sweepEvery {
		l.calls = 0
		l.evictIdleLocked(time.Now(), ip)
	}
	l.mu.Unlock()
	return b.allow()
}

// evictIdleLocked drops buckets untouched for bucketIdleTTL. Caller holds l.mu.
//
// keep is the bucket the current request just took, which must survive its own
// sweep: a brand-new bucket has lastTick set to now, but reading any bucket's
// lastTick here would race with the concurrent allow() this function is called
// from the middle of, so it is excluded by key instead of by timestamp.
func (l *ipLimiter) evictIdleLocked(now time.Time, keep string) {
	for ip, b := range l.buckets {
		if ip == keep {
			continue
		}
		b.mu.Lock()
		idle := now.Sub(b.lastTick)
		b.mu.Unlock()
		if idle >= bucketIdleTTL {
			delete(l.buckets, ip)
		}
	}
}

// withRateLimit rejects requests from IPs that exceed the configured rate.
// Applied before withAuth so even authentication attempts are throttled.
// No-op when s.limiter is nil (rate limiting disabled).
func (s *Server) withRateLimit(next http.Handler) http.Handler {
	if s.limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !s.limiter.allow(ip) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}
