package admin

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type RoleUseCase interface {
	ListBuiltins(ctx context.Context) ([]admin.Role, error)
	Get(ctx context.Context, id admin.RoleID) (admin.Role, error)
	Create(ctx context.Context, name string, scope admin.Scope, tenantID string, actor Actor) (*admin.Role, error)
	SetPermissions(ctx context.Context, id admin.RoleID, permissions []admin.Permission, actor Actor) error
}

type RoleService struct {
	roles admin.RoleRepo
	audit admin.AuditRepo
}

func NewRoleService(roles admin.RoleRepo, audit admin.AuditRepo) *RoleService {
	return &RoleService{roles: roles, audit: audit}
}

func (s *RoleService) ListBuiltins(ctx context.Context) ([]admin.Role, error) {
	return s.roles.ListBuiltins(ctx)
}

func (s *RoleService) Get(ctx context.Context, id admin.RoleID) (admin.Role, error) {
	return s.roles.GetByID(ctx, id)
}

func (s *RoleService) Create(ctx context.Context, name string, scope admin.Scope, tenantID string, actor Actor) (*admin.Role, error) {
	r := &admin.Role{
		Name:     name,
		Scope:    scope,
		TenantID: tenantID,
	}

	if err := s.roles.Insert(ctx, r); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, actor, admin.ActionRoleCreate, string(r.ID), tenantID)
	return r, nil
}

func (s *RoleService) SetPermissions(ctx context.Context, id admin.RoleID, permissions []admin.Permission, actor Actor) error {
	role, err := s.roles.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if role.IsBuiltin {
		return admin.ErrProtectedRole
	}

	if err := s.roles.SetPermissions(ctx, id, permissions); err != nil {
		return err
	}

	s.recordAudit(ctx, actor, admin.ActionRolePermissions, string(id), role.TenantID)
	return nil
}

func (s *RoleService) recordAudit(ctx context.Context, actor Actor, action admin.Action, targetID, tenantID string) {
	entry := &admin.AuditEntry{
		ActorID:  actor.ID,
		Action:   action,
		TargetID: targetID,
		TenantID: tenantID,
		IP:       actor.IP,
	}
	_ = s.audit.Record(ctx, entry)
}
