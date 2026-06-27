// Package middleware — authz.go: RequireScope middleware para handlers admin.
//
// Issue #132 (Sprint 0A C4 audit): handlers adminweb faziam auth() mas
// nunca authz(). Cada state-changing handler agora chama RequireScope(perm,
// scope) que consulta admin.Evaluate(principal, perm, scope) e nega com
// 403 + audit row se falhar.
//
// ADR-0042: depende de Principal estar hidratado pelo Session middleware.
// Se Principal.Permissions for nil (hydration falhou), nega fail-closed.
package middleware

import (
	"context"
	"net/http"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// RequireScope retorna middleware que valida se o Principal no context tem
// a permissão `perm` no escopo `scope`. Falha → 403 + audit row.
//
// auditRepo pode ser nil — em testes/unitários. Em produção, sempre setado
// para garantir audit trail de negações.
//
// Uso:
//
//	router.With(middleware.RequireScope(admin.PermDeleteUsers, admin.ScopePlatform, auditRepo))
func RequireScope(perm admin.Permission, scope admin.Scope, auditRepo admin.AuditRepo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok {
				// Não autenticado — RequireAuth deveria ter barrado antes.
				// Mas fail-closed é mandatório.
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			// Fail-closed se Principal não foi hidratado (Permissions nil).
			// Isso cobre o caso de hydration falhar (R-S0-1).
			if principal.Permissions == nil {
				recordAuthDenied(r.Context(), auditRepo, principal, perm, scope, "principal_not_hydrated")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			allowed := admin.Evaluate(principal, perm, scope)
			if !allowed {
				recordAuthDenied(r.Context(), auditRepo, principal, perm, scope, "insufficient_permission")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// recordAuthDenied emite uma audit row quando um Principal é negado por
// falta de permissão. Best-effort — se audit falhar, log mas não bloqueia
// a response (que já é 403). Caller garante que AuditEntry.ID é gerado.
func recordAuthDenied(ctx context.Context, repo admin.AuditRepo, p admin.Principal, perm admin.Permission, scope admin.Scope, reason string) {
	if repo == nil {
		return
	}
	entry := &admin.AuditEntry{
		ActorID:    p.UserID,
		ActorEmail: p.Email,
		Action:     "auth.denied",
		Metadata: map[string]any{
			"perm":   string(perm),
			"scope":  string(scope),
			"reason": reason,
		},
	}
	// Best-effort: se falhar, log mas não propaga (response já é 403)
	_ = repo.Record(ctx, entry)
}
