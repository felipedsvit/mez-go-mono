package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
)

// fakeSessionResolver implementa auth.SessionUseCase mínimo para teste.
type fakeSessionResolver struct {
	auth.SessionUseCase
	users map[admin.SessionID]admin.Session
}

func (f *fakeSessionResolver) Resolve(ctx context.Context, sid admin.SessionID) (admin.Session, error) {
	s, ok := f.users[sid]
	if !ok {
		return admin.Session{}, errSessionNotFound
	}
	return s, nil
}

var errSessionNotFound = &fakeError{"session not found"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

// fakeAudit implementa admin.AuditRepo para teste (só Record importa).
type fakeAudit struct {
	admin.AuditRepo
	entries []admin.AuditEntry
}

func (f *fakeAudit) Record(ctx context.Context, e *admin.AuditEntry) error {
	f.entries = append(f.entries, *e)
	return nil
}
func (f *fakeAudit) List(ctx context.Context, _ admin.AuditFilter) ([]admin.AuditEntry, error) {
	return f.entries, nil
}

func TestSession_NoCookie(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// Principal não deve estar no context
		_, ok := PrincipalFromContext(r.Context())
		if ok {
			t.Error("Principal presente sem cookie")
		}
	})

	mw := Session(SessionConfig{
		Resolver: &fakeSessionResolver{},
		Cookie:   "test",
		Secure:   true,
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)

	if !called {
		t.Fatal("next não foi chamado")
	}
}

func TestSession_WithCookie_NoHydrator(t *testing.T) {
	resolver := &fakeSessionResolver{
		users: map[admin.SessionID]admin.Session{
			"sess1": {UserID: "u1", Email: "a@b.c"},
		},
	}

	var gotPrincipal admin.Principal
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, gotOK = PrincipalFromContext(r.Context())
	})

	mw := Session(SessionConfig{
		Resolver: resolver,
		Cookie:   "test",
		Secure:   true,
		// Hydrator nil — Principal.Permissions deve ficar nil
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "test", Value: "sess1"})
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)

	if !gotOK {
		t.Fatal("Principal não injetado no context")
	}
	if gotPrincipal.UserID != "u1" || gotPrincipal.Email != "a@b.c" {
		t.Fatalf("Principal errado: %+v", gotPrincipal)
	}
	if gotPrincipal.Permissions != nil {
		t.Errorf("Permissions deve ser nil sem Hydrator; got %v", gotPrincipal.Permissions)
	}
}

func TestSession_WithHydrator(t *testing.T) {
	resolver := &fakeSessionResolver{
		users: map[admin.SessionID]admin.Session{
			"sess1": {UserID: "u1", Email: "a@b.c"},
		},
	}

	// Hydrator fake: retorna 1 role + 1 permission
	hydrator := &fakeHydrator{
		roles: []admin.RoleBinding{{Scope: admin.ScopePlatform, RoleID: "r1"}},
		perms: map[admin.Permission]struct{}{
			admin.PermReadUsers: {},
		},
	}

	var gotPrincipal admin.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, _ = PrincipalFromContext(r.Context())
	})

	mw := Session(SessionConfig{
		Resolver: resolver,
		Cookie:   "test",
		Secure:   true,
		Hydrator: NewCachedPrincipalHydrator(hydrator),
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "test", Value: "sess1"})
	rr := httptest.NewRecorder()
	mw(next).ServeHTTP(rr, req)

	if gotPrincipal.UserID != "u1" {
		t.Fatalf("UserID errado: %s", gotPrincipal.UserID)
	}
	if len(gotPrincipal.Roles) != 1 {
		t.Fatalf("Roles esperado 1, got %d", len(gotPrincipal.Roles))
	}
	if _, ok := gotPrincipal.Permissions[admin.PermReadUsers]; !ok {
		t.Errorf("PermReadUsers esperado em Permissions")
	}
}

type fakeHydrator struct {
	PrincipalHydrator // embed interface to satisfy it
	roles             []admin.RoleBinding
	perms             map[admin.Permission]struct{}
}

func (f *fakeHydrator) Hydrate(ctx context.Context, userID string) ([]admin.RoleBinding, map[admin.Permission]struct{}, error) {
	return f.roles, f.perms, nil
}

// CachedPrincipalHydrator tests

func TestCachedPrincipalHydrator_TTL(t *testing.T) {
	inner := &fakeHydrator{
		roles: nil,
		perms: map[admin.Permission]struct{}{admin.PermReadUsers: {}},
	}
	cached := NewCachedPrincipalHydrator(inner)

	// 1ª chamada: cache miss, vai ao inner
	_, perms1, err := cached.Hydrate(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := perms1[admin.PermReadUsers]; !ok {
		t.Fatal("perms esperado na 1ª chamada")
	}

	// 2ª chamada: cache hit, inner NÃO deve ser chamado
	inner.perms = nil // zera perms para detectar se inner foi chamado
	_, perms2, err := cached.Hydrate(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := perms2[admin.PermReadUsers]; !ok {
		t.Fatal("cache não retornou perms da 1ª chamada")
	}
}

func TestCachedPrincipalHydrator_Invalidate(t *testing.T) {
	inner := &fakeHydrator{
		roles: nil,
		perms: map[admin.Permission]struct{}{admin.PermReadUsers: {}},
	}
	cached := NewCachedPrincipalHydrator(inner)

	_, _, _ = cached.Hydrate(context.Background(), "u1")
	cached.Invalidate("u1")

	// Após invalidate, inner deve ser chamado de novo
	inner.perms = nil
	_, perms, _ := cached.Hydrate(context.Background(), "u1")
	if perms != nil {
		t.Errorf("após invalidate, inner deve ser chamado (perms seria nil)")
	}
}

// RequireScope tests

func TestRequireScope_Allows(t *testing.T) {
	// Set up: session com perm + role platform (necessário para Evaluate)
	mw := Session(SessionConfig{
		Resolver: &fakeSessionResolver{users: map[admin.SessionID]admin.Session{
			"s1": {UserID: "u1", Email: "a@b.c"},
		}},
		Cookie: "c",
		Secure: true,
		Hydrator: NewCachedPrincipalHydrator(&fakeHydrator{
			roles: []admin.RoleBinding{{Scope: admin.ScopePlatform, RoleID: "r1"}},
			perms: map[admin.Permission]struct{}{admin.PermReadUsers: {}},
		}),
	})

	audit := &fakeAudit{}
	gate := RequireScope(admin.PermReadUsers, admin.ScopePlatform, audit)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "c", Value: "s1"})
	rr := httptest.NewRecorder()
	mw(gate(next)).ServeHTTP(rr, req)

	if !called {
		t.Fatal("next não foi chamado")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status esperado 200, got %d", rr.Code)
	}
}

func TestRequireScope_Denies(t *testing.T) {
	// Set up: session SEM a perm requerida
	mw := Session(SessionConfig{
		Resolver: &fakeSessionResolver{users: map[admin.SessionID]admin.Session{
			"s1": {UserID: "u1", Email: "a@b.c"},
		}},
		Cookie: "c",
		Secure: true,
		Hydrator: NewCachedPrincipalHydrator(&fakeHydrator{
			roles: []admin.RoleBinding{{Scope: admin.ScopePlatform, RoleID: "r1"}},
			perms: map[admin.Permission]struct{}{admin.PermReadUsers: {}},
		}),
	})

	audit := &fakeAudit{}
	gate := RequireScope(admin.PermDeleteUsers, admin.ScopePlatform, audit)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "c", Value: "s1"})
	rr := httptest.NewRecorder()
	mw(gate(next)).ServeHTTP(rr, req)

	if called {
		t.Fatal("next não deveria ser chamado (permissão negada)")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status esperado 403, got %d", rr.Code)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("esperado 1 audit entry, got %d", len(audit.entries))
	}
	if audit.entries[0].Action != "auth.denied" {
		t.Errorf("action esperada 'auth.denied', got %q", audit.entries[0].Action)
	}
}

func TestRequireScope_FailClosed_NoHydrator(t *testing.T) {
	// Sem Hydrator, Principal.Permissions = nil → deve negar
	mw := Session(SessionConfig{
		Resolver: &fakeSessionResolver{users: map[admin.SessionID]admin.Session{
			"s1": {UserID: "u1", Email: "a@b.c"},
		}},
		Cookie: "c",
		Secure: true,
		// Hydrator: nil
	})

	audit := &fakeAudit{}
	gate := RequireScope(admin.PermReadUsers, admin.ScopePlatform, audit)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "c", Value: "s1"})
	rr := httptest.NewRecorder()
	mw(gate(next)).ServeHTTP(rr, req)

	if called {
		t.Fatal("next não deveria ser chamado (fail-closed sem Hydrator)")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status esperado 403, got %d", rr.Code)
	}
	// Audit deve ter sido gravada com reason=principal_not_hydrated
	if len(audit.entries) != 1 {
		t.Fatalf("esperado 1 audit entry, got %d", len(audit.entries))
	}
	if got := audit.entries[0].Metadata["reason"]; got != "principal_not_hydrated" {
		t.Errorf("reason esperado 'principal_not_hydrated', got %v", got)
	}
}

func TestRequireScope_NoAuditRepo(t *testing.T) {
	// auditRepo nil — não deve panic, nega normalmente
	mw := Session(SessionConfig{
		Resolver: &fakeSessionResolver{users: map[admin.SessionID]admin.Session{
			"s1": {UserID: "u1", Email: "a@b.c"},
		}},
		Cookie: "c",
		Secure: true,
		Hydrator: NewCachedPrincipalHydrator(&fakeHydrator{
			perms: map[admin.Permission]struct{}{},
		}),
	})

	gate := RequireScope(admin.PermReadUsers, admin.ScopePlatform, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next não deveria ser chamado")
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "c", Value: "s1"})
	rr := httptest.NewRecorder()
	mw(gate(next)).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status esperado 403 mesmo sem auditRepo, got %d", rr.Code)
	}
}
