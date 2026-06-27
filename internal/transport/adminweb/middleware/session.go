package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
)

type contextKey string

const principalKey contextKey = "principal"

type SessionConfig struct {
	Resolver auth.SessionUseCase
	Cookie   string
	TTL      time.Duration
	// Secure define o flag Secure do cookie de sessão. Default true em prod.
	// Override via MEZ_SESSION_COOKIE_SECURE=false em dev local sem HTTPS.
	// Issue #131 (Sprint 0A C3 audit): prefixo __Host- exige Secure=true
	// (RFC 6265bis).
	Secure bool
	// Hydrator preenche Principal.Permissions e Principal.Roles ao
	// carregar a sessão. Issue #132 (Sprint 0A C4 audit). Opcional —
	// se nil, Principal é criada só com UserID/Email (comportamento legacy).
	Hydrator PrincipalHydrator
}

// PrincipalHydrator é a interface para o session middleware carregar roles
// e permissions do user. Implementação padrão faz query em role_bindings
// + roles (via usecase/admin); cache in-memory TTL 5min por user.
//
// Issue #132 + ADR-0042.
type PrincipalHydrator interface {
	Hydrate(ctx context.Context, userID string) (roles []admin.RoleBinding, permissions map[admin.Permission]struct{}, err error)
}

func Session(cfg SessionConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cfg.Cookie)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			session, err := cfg.Resolver.Resolve(r.Context(), admin.SessionID(cookie.Value))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			principal := admin.Principal{
				UserID: session.UserID,
				Email:  session.Email,
			}

			// ADR-0042: hidrata Roles/Permissions se Hydrator setado.
			// Falha de hydration não derruba a sessão — só fica sem perm
			// e Evaluate() nega tudo. Loga warning para ops investigar.
			if cfg.Hydrator != nil {
				roles, perms, herr := cfg.Hydrator.Hydrate(r.Context(), string(session.UserID))
				if herr != nil {
					// Loga mas não bloqueia — fallback fail-closed via Evaluate
					// que vai negar tudo se Principal.Permissions for nil.
					// Em produção, telemetria deve alertar se > X% das
					// hydrations falharem.
					_ = herr // logged em usecase/admin/hydrator.go
				} else {
					principal.Roles = roles
					principal.Permissions = perms
				}
			}

			ctx := context.WithValue(r.Context(), principalKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// principalCacheTTL é o TTL do cache de hydration (ADR-0042).
const principalCacheTTL = 5 * time.Minute

// CachedPrincipalHydrator é um wrapper que adiciona cache in-memory ao
// PrincipalHydrator. Cache por userID com TTL 5min — evita N+1 quando
// admin panel faz loops de requests. Cache é invalidated on revocation
// via usecase/admin/role_service.NotifyRevocation (futuro).
type CachedPrincipalHydrator struct {
	inner PrincipalHydrator
	mu    sync.RWMutex
	cache map[string]cachedPrincipal
}

type cachedPrincipal struct {
	roles       []admin.RoleBinding
	permissions map[admin.Permission]struct{}
	expiresAt   time.Time
}

// NewCachedPrincipalHydrator wrappa um PrincipalHydrator com cache.
func NewCachedPrincipalHydrator(inner PrincipalHydrator) *CachedPrincipalHydrator {
	return &CachedPrincipalHydrator{
		inner: inner,
		cache: make(map[string]cachedPrincipal),
	}
}

func (c *CachedPrincipalHydrator) Hydrate(ctx context.Context, userID string) ([]admin.RoleBinding, map[admin.Permission]struct{}, error) {
	c.mu.RLock()
	if entry, ok := c.cache[userID]; ok && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.roles, entry.permissions, nil
	}
	c.mu.RUnlock()

	roles, perms, err := c.inner.Hydrate(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	c.mu.Lock()
	c.cache[userID] = cachedPrincipal{
		roles:       roles,
		permissions: perms,
		expiresAt:   time.Now().Add(principalCacheTTL),
	}
	c.mu.Unlock()

	return roles, perms, nil
}

// Invalidate limpa o cache de um user. Chamado após revogação de role
// (issue #132 R-S0-1: rollout gradual — cache miss vira re-hydrate
// imediato).
func (c *CachedPrincipalHydrator) Invalidate(userID string) {
	c.mu.Lock()
	delete(c.cache, userID)
	c.mu.Unlock()
}

func PrincipalFromContext(ctx context.Context) (admin.Principal, bool) {
	p, ok := ctx.Value(principalKey).(admin.Principal)
	return p, ok
}

func RequireAuth(loginPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := PrincipalFromContext(r.Context())
			if !ok {
				http.Redirect(w, r, loginPath+"?next="+r.URL.Path, http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
