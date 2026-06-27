package websocket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewUpgrader_RejectsEmptyOrigin_WhenNoAllowSameOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	if up.CheckOrigin(r) {
		t.Error("expected rejection: empty Origin not allowed without AllowSameOrigin")
	}
}

func TestNewUpgrader_AcceptsEmptyOrigin_WhenAllowSameOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowSameOrigin: true})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	if !up.CheckOrigin(r) {
		t.Error("expected acceptance: AllowSameOrigin=true")
	}
}

func TestNewUpgrader_RejectsUnknownOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("Origin", "https://evil.com")
	if up.CheckOrigin(r) {
		t.Error("expected rejection: evil.com not in allowlist")
	}
}

func TestNewUpgrader_AcceptsAllowlistedOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com", "https://admin.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("Origin", "https://APP.EXAMPLE.COM") // case-insensitive
	if !up.CheckOrigin(r) {
		t.Error("expected acceptance: APP.EXAMPLE.COM in allowlist (case-insensitive)")
	}
}

func TestNewUpgrader_RejectsProtocolConfusion(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("Origin", "http://app.example.com") // http vs https
	if up.CheckOrigin(r) {
		t.Error("expected rejection: http vs https scheme mismatch")
	}
}

func TestNewUpgrader_AcceptsSameOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: nil})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Host = "mez.example.com"
	r.TLS = nil // http
	r.Header.Set("Origin", "http://mez.example.com")
	if !up.CheckOrigin(r) {
		t.Error("expected acceptance: same-origin (Origin.host == r.Host)")
	}
}

func TestNewUpgrader_RejectsSameOriginDifferentScheme(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: nil})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Host = "mez.example.com"
	r.TLS = nil
	r.Header.Set("Origin", "https://mez.example.com") // https, mas request é http
	if up.CheckOrigin(r) {
		t.Error("expected rejection: same-origin but scheme mismatch (https vs http)")
	}
}

func TestNewUpgrader_RejectsSubdomainConfusion(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("Origin", "https://app.example.com.evil.com")
	if up.CheckOrigin(r) {
		t.Error("expected rejection: subdomain confusion attempt")
	}
}

func TestNewUpgrader_TrustedProxy_HonorsForwardedOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		TrustedProxy:   true,
	})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("X-Forwarded-Origin", "https://app.example.com")
	r.Header.Set("Origin", "https://internal-lb")
	if !up.CheckOrigin(r) {
		t.Error("expected acceptance: X-Forwarded-Origin in allowlist (TrustedProxy)")
	}
}

func TestNewUpgrader_RejectsMalformedOrigin(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	r.Header.Set("Origin", "not-a-url")
	if up.CheckOrigin(r) {
		t.Error("expected rejection: malformed Origin")
	}
}

func TestNewUpgrader_RejectsNonHTTPSchemes(t *testing.T) {
	up := NewUpgrader(UpgraderConfig{AllowedOrigins: []string{"https://app.example.com"}})
	r := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	for _, scheme := range []string{"javascript:alert(1)", "data:text/html,foo", "file:///etc/passwd", "ws://app.example.com", "wss://app.example.com"} {
		r.Header.Set("Origin", scheme)
		if up.CheckOrigin(r) {
			t.Errorf("expected rejection for non-http scheme: %q", scheme)
		}
	}
}

func TestNormalizeOrigins_StripsScheme(t *testing.T) {
	got := normalizeOrigins([]string{
		"https://app.example.com",
		"http://admin.example.com:8080",
		"  ",
		"",
		"ftp://bad",
		"javascript:alert(1)",
		"ws://should-be-rejected",
	})
	want := map[string]struct{}{
		"https://app.example.com":        {},
		"http://admin.example.com:8080":  {},
	}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (%v)", len(got), len(want), got)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing %q in %v", k, got)
		}
	}
}
