package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRF_GETPassesWithoutToken(t *testing.T) {
	cfg := DefaultCSRFConfig()
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	// GET should set the cookie so the next POST can validate
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "XSRF-TOKEN" {
			found = true
			if c.Value == "" {
				t.Error("XSRF-TOKEN cookie should have a value")
			}
		}
	}
	if !found {
		t.Error("expected XSRF-TOKEN cookie to be set on GET")
	}
}

func TestCSRF_PostRequiresToken(t *testing.T) {
	cfg := DefaultCSRFConfig()
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST without any cookie/header
	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without token expected 403, got %d", rec.Code)
	}
}

func TestCSRF_PostWithCookieButNoHeader(t *testing.T) {
	cfg := DefaultCSRFConfig()
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: "token123"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with cookie but no header expected 403, got %d", rec.Code)
	}
}

func TestCSRF_PostWithMatchingToken_Passes(t *testing.T) {
	cfg := DefaultCSRFConfig()
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: "matching-token"})
	req.Header.Set("X-CSRF-Token", "matching-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("POST with matching token expected 200, got %d", rec.Code)
	}
}

func TestCSRF_PostWithMismatchedToken(t *testing.T) {
	cfg := DefaultCSRFConfig()
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/users", nil)
	req.AddCookie(&http.Cookie{Name: "XSRF-TOKEN", Value: "cookie-value"})
	req.Header.Set("X-CSRF-Token", "different-value")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST with mismatched token expected 403, got %d", rec.Code)
	}
}

func TestCSRF_ExemptPath(t *testing.T) {
	cfg := DefaultCSRFConfig()
	cfg.Exempt = []string{"/admin/auth/oidc/callback"}
	handler := CSRF(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/admin/auth/oidc/callback", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("exempt path POST expected 200, got %d", rec.Code)
	}
}

func TestSecurityHeaders_AllSet(t *testing.T) {
	handler := SecurityHeaders(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headers := rec.Result().Header
	expected := []string{
		"X-Content-Type-Options",
		"X-Frame-Options",
		"Referrer-Policy",
		"Content-Security-Policy",
		"Strict-Transport-Security",
	}
	for _, h := range expected {
		if v := headers.Get(h); v == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if !strings.Contains(headers.Get("X-Frame-Options"), "DENY") {
		t.Errorf("X-Frame-Options should be DENY, got %q", headers.Get("X-Frame-Options"))
	}
}

func TestSecurityHeaders_NoHSTS_WhenInsecure(t *testing.T) {
	handler := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Result().Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS should not be set when secure=false (dev mode)")
	}
}
