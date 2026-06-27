// Package ratelimit provides a simple in-memory per-IP token-bucket rate
// limiter. Used for /admin/login to prevent brute force on credentials at
// the HTTP layer (independent of the per-email lockout at the usecase layer).
package ratelimit

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Limiter is a per-key (typically IP) token-bucket rate limiter.
type Limiter struct {
	rate      float64 // tokens per second
	burst     float64 // bucket capacity
	mu        sync.Mutex
	buckets   map[string]*bucket
	nowFunc   func() time.Time
	gcCounter int
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a Limiter that allows `burst` requests in a sudden spike and
// refills at `rate` tokens/second.
func New(rate, burst float64) *Limiter {
	return &Limiter{
		rate:    rate,
		burst:   burst,
		buckets: make(map[string]*bucket),
		nowFunc: time.Now,
	}
}

// Allow returns true if the request from key is permitted. If false, the
// caller should respond with 429 and a Retry-After header.
func (l *Limiter) Allow(key string) bool {
	if l.rate <= 0 {
		return true // disabled
	}
	now := l.nowFunc()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	// Refill
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	l.maybeGC()
	return true
}

func (l *Limiter) maybeGC() {
	// Cheap opportunistic GC: every 256 Allow calls, sweep stale buckets.
	l.gcCounter++
	if l.gcCounter%256 != 0 {
		return
	}
	now := l.nowFunc()
	cutoff := now.Add(-10 * time.Minute)
	for k, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, k)
		}
	}
}

// RetryAfter returns the suggested retry delay based on current bucket state.
// 1 second is a safe default; clients should back off.
func (l *Limiter) RetryAfter(key string) time.Duration {
	return time.Second
}

// ClientIP returns the best-effort client IP, preferring RemoteAddr (which
// the standard library sets from the actual TCP connection, not from
// X-Forwarded-For which is untrusted). This is intentionally conservative
// for the /admin/login path: we don't want a misconfigured proxy to allow
// one attacker to exhaust another tenant's quota.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		// Some test/loopback paths may not have a port. Fall back to the
		// X-Forwarded-For first hop ONLY if there is no RemoteAddr at all
		// (this is rare in production).
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		return "unknown"
	}
	return host
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
