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

// ipLimiter holds per-IP token buckets. Keys are the client IP string.
type ipLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64
	burst    int
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
	l.mu.Unlock()
	return b.allow()
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
