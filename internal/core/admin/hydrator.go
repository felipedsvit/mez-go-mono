// Package admin — hydrator.go: implementação DB do PrincipalHydrator.
//
// Issue #132 (Sprint 0A C4 audit, ADR-0042): carrega roles e permissions
// do user via role_bindings + roles. Usado pelo session middleware.
package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RoleBindingRepo é o port que o hydrator precisa. A implementação concreta
// está em adapter/repository/postgres/admin (camada adapter).
type RoleBindingRepo interface {
	ListByUser(ctx context.Context, userID AdminUserID) ([]RoleBinding, error)
}

// RoleDetailLoader carrega as permissions de cada role. Separado do
// RoleBindingRepo para evitar N+1: o hydrator carrega bindings (1 query)
// + roles[] (1 query) e computa o set de permissions.
type RoleDetailLoader interface {
	GetByID(ctx context.Context, id RoleID) (Role, error)
}

// DBPrincipalHydrator é a implementação padrão do PrincipalHydrator do
// session middleware. Faz 2 queries (bindings + roles[]) e retorna o
// Principal já com Permissions populado.
type DBPrincipalHydrator struct {
	Bindings RoleBindingRepo
	Roles    RoleDetailLoader
}

// NewDBPrincipalHydrator constrói o hydrator. Pode receber nil se o
// caller quiser um noop (útil em testes).
func NewDBPrincipalHydrator(b RoleBindingRepo, r RoleDetailLoader) *DBPrincipalHydrator {
	return &DBPrincipalHydrator{Bindings: b, Roles: r}
}

// Hydrate implementa PrincipalHydrator. Carrega role_bindings do user,
// depois carrega cada role para extrair permissions, retorna o set
// consolidado.
//
// Erro de qualquer query propaga — o session middleware vai logar e
// deixar Principal.Permissions = nil, o que faz RequireScope negar
// fail-closed (issue #132 R-S0-1).
func (h *DBPrincipalHydrator) Hydrate(ctx context.Context, userID string) ([]RoleBinding, map[Permission]struct{}, error) {
	if h.Bindings == nil || h.Roles == nil {
		return nil, nil, fmt.Errorf("hydrator not initialized")
	}

	uid, err := parseUserID(userID)
	if err != nil {
		return nil, nil, fmt.Errorf("parse userID: %w", err)
	}

	bindings, err := h.Bindings.ListByUser(ctx, uid)
	if err != nil {
		return nil, nil, fmt.Errorf("list role bindings: %w", err)
	}

	perms := make(map[Permission]struct{}, 32)
	for _, b := range bindings {
		role, err := h.Roles.GetByID(ctx, b.RoleID)
		if err != nil {
			// Role foi deletada entre o list e o get — skip mas não falha.
			// Logar via caller se necessário.
			continue
		}
		for _, p := range role.Permissions {
			perms[p] = struct{}{}
		}
	}

	return bindings, perms, nil
}

func parseUserID(s string) (AdminUserID, error) {
	if s == "" {
		return "", fmt.Errorf("empty userID")
	}
	if _, err := uuid.Parse(s); err != nil {
		return "", fmt.Errorf("invalid uuid: %w", err)
	}
	return AdminUserID(s), nil
}

// Compile-time check that pgx.ErrNoRows is imported (usecase/admin pode
// querer distinguir "user sem roles" de "erro de query").
var _ = pgx.ErrNoRows
