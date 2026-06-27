package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_AllowsBurstThenBlocks(t *testing.T) {
	limiter := New(1.0, 3.0) // 3 burst, 1/sec refill

	for i := 0; i < 3; i++ {
		if !limiter.Allow("1.2.3.4") {
			t.Errorf("attempt %d should be allowed (burst=3)", i+1)
		}
	}
	if limiter.Allow("1.2.3.4") {
		t.Error("4th request should be blocked (burst exhausted)")
	}
}

func TestRateLimiter_RefillsOverTime(t *testing.T) {
	limiter := New(10.0, 2.0) // 2 burst, fast refill
	if !limiter.Allow("ip") {
		t.Fatal("first should pass")
	}
	if !limiter.Allow("ip") {
		t.Fatal("second should pass")
	}
	if limiter.Allow("ip") {
		t.Fatal("third should be blocked")
	}
	time.Sleep(200 * time.Millisecond) // 2 tokens refilled at 10/s
	if !limiter.Allow("ip") {
		t.Error("after refill, request should pass")
	}
}

func TestRateLimiter_PerKeyIsolation(t *testing.T) {
	limiter := New(0.1, 1.0) // 1 burst, slow refill
	if !limiter.Allow("a") {
		t.Fatal("a first should pass")
	}
	if !limiter.Allow("b") {
		t.Fatal("b first should pass (different key)")
	}
	if limiter.Allow("a") {
		t.Error("a second should be blocked")
	}
	if limiter.Allow("b") {
		t.Error("b second should be blocked")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	limiter := New(0, 0) // rate=0 disables
	for i := 0; i < 100; i++ {
		if !limiter.Allow("ip") {
			t.Errorf("disabled limiter should allow request %d", i)
		}
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		remote, fwd, want string
	}{
		{"1.2.3.4:5000", "", "1.2.3.4"},
		{"1.2.3.4:5000", "5.6.7.8", "1.2.3.4"}, // X-Forwarded-For ignored when RemoteAddr present
		{"[::1]:5000", "", "::1"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = tt.remote
		if tt.fwd != "" {
			req.Header.Set("X-Forwarded-For", tt.fwd)
		}
		if got := ClientIP(req); got != tt.want {
			t.Errorf("ClientIP(remote=%q, fwd=%q) = %q, want %q", tt.remote, tt.fwd, got, tt.want)
		}
	}
}
