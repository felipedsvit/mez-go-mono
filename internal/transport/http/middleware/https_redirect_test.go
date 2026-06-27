package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPSRedirect_NoForce(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := HTTPSRedirect(false, "")
	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)
	if !called {
		t.Error("next não chamado quando force=false")
	}
	if rr.Code != 200 {
		t.Errorf("status esperado 200, got %d", rr.Code)
	}
}

func TestHTTPSRedirect_Force_HTTP(t *testing.T) {
	mw := HTTPSRedirect(true, "")
	req := httptest.NewRequest("GET", "http://example.com/path?x=1", nil)
	rr := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("next não deveria ser chamado em HTTP quando force=true")
	})).ServeHTTP(rr, req)
	if rr.Code != http.StatusMovedPermanently {
		t.Errorf("status esperado 301, got %d", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "https://example.com/path?x=1" {
		t.Errorf("Location esperado https://example.com/path?x=1, got %q", got)
	}
}

func TestHTTPSRedirect_Force_HTTPS(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	mw := HTTPSRedirect(true, "")
	req := httptest.NewRequest("GET", "https://example.com/x", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)
	if !called {
		t.Error("next não chamado quando já é HTTPS")
	}
	if rr.Code != 200 {
		t.Errorf("status esperado 200, got %d", rr.Code)
	}
}

func TestHTTPSRedirect_CustomHost(t *testing.T) {
	mw := HTTPSRedirect(true, "secure.example.com")
	req := httptest.NewRequest("GET", "http://example.com/x", nil)
	rr := httptest.NewRecorder()
	mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rr, req)
	if got := rr.Header().Get("Location"); got != "https://secure.example.com/x" {
		t.Errorf("Location esperado https://secure.example.com/x, got %q", got)
	}
}
