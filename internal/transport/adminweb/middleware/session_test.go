package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type stubResolver struct {
	session admin.Session
	err     error
}

func (s stubResolver) Resolve(ctx context.Context, id admin.SessionID) (admin.Session, error) {
	if s.err != nil {
		return admin.Session{}, s.err
	}
	return s.session, nil
}

func TestSession_NoCookie_PassesThrough(t *testing.T) {
	resolver := stubResolver{}
	cfg := SessionConfig{Resolver: resolver, Cookie: "mez_session", TTL: time.Hour}

	called := false
	handler := Session(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, ok := PrincipalFromContext(r.Context())
		if ok {
			t.Error("expected no principal when no cookie is sent")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler not called")
	}
}

func TestSession_ValidCookie_InjectsPrincipal(t *testing.T) {
	resolver := stubResolver{
		session: admin.Session{
			ID:     "sid",
			UserID: "u1",
			Email:  "u@e.com",
		},
	}
	cfg := SessionConfig{Resolver: resolver, Cookie: "mez_session", TTL: time.Hour}

	var gotPrincipal *admin.Principal
	handler := Session(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			t.Fatal("expected principal in context")
		}
		gotPrincipal = &p
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "mez_session", Value: "sid"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotPrincipal == nil || gotPrincipal.UserID != "u1" || gotPrincipal.Email != "u@e.com" {
		t.Errorf("unexpected principal: %+v", gotPrincipal)
	}
}

func TestSession_InvalidCookie_PassesThroughWithoutPrincipal(t *testing.T) {
	resolver := stubResolver{err: admin.ErrSessionExpired}
	cfg := SessionConfig{Resolver: resolver, Cookie: "mez_session", TTL: time.Hour}

	handler := Session(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFromContext(r.Context()); ok {
			t.Error("invalid cookie should not inject principal")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	req.AddCookie(&http.Cookie{Name: "mez_session", Value: "expired"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}

func TestRequireAuth_NoPrincipal_Redirects(t *testing.T) {
	handler := RequireAuth("/admin/login")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when no principal")
	}))

	req := httptest.NewRequest(http.MethodGet, "/admin/tenants", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login?next=/admin/tenants" {
		t.Errorf("unexpected location: %q", loc)
	}
}

func TestRequireAuth_WithPrincipal_PassesThrough(t *testing.T) {
	handler := RequireAuth("/admin/login")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Inject principal via session middleware
	resolver := stubResolver{session: admin.Session{UserID: "u1", Email: "u@e.com"}}
	cfg := SessionConfig{Resolver: resolver, Cookie: "mez_session", TTL: time.Hour}
	wrapped := Session(cfg)(handler)

	req := httptest.NewRequest(http.MethodGet, "/admin/tenants", nil)
	req.AddCookie(&http.Cookie{Name: "mez_session", Value: "sid"})
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
