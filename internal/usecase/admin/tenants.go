package admin

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type Actor struct {
	ID        admin.AdminUserID
	Email     string
	IP        string
	UserAgent string
}

type TenantUseCase interface {
	List(ctx context.Context, filter admin.TenantFilter) ([]admin.Tenant, error)
	Get(ctx context.Context, id string) (admin.Tenant, error)
	Provision(ctx context.Context, name, slug string, actor Actor) (*admin.Tenant, error)
	SetStatus(ctx context.Context, id string, status admin.TenantStatus, actor Actor) error
}

type TenantService struct {
	tenants admin.TenantRepo
	audit   admin.AuditRepo
}

func NewTenantService(tenants admin.TenantRepo, audit admin.AuditRepo) *TenantService {
	return &TenantService{tenants: tenants, audit: audit}
}

func (s *TenantService) List(ctx context.Context, filter admin.TenantFilter) ([]admin.Tenant, error) {
	return s.tenants.List(ctx, filter)
}

func (s *TenantService) Get(ctx context.Context, id string) (admin.Tenant, error) {
	return s.tenants.GetByID(ctx, id)
}

func (s *TenantService) Provision(ctx context.Context, name, slug string, actor Actor) (*admin.Tenant, error) {
	t, err := admin.NewTenant(name, slug)
	if err != nil {
		return nil, err
	}

	if err := s.tenants.Create(ctx, t); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, actor, admin.ActionTenantCreate, t.ID, t.ID)
	return t, nil
}

func (s *TenantService) SetStatus(ctx context.Context, id string, status admin.TenantStatus, actor Actor) error {
	if err := s.tenants.SetStatus(ctx, id, status); err != nil {
		return err
	}

	s.recordAudit(ctx, actor, admin.ActionTenantStatus, id, "")
	return nil
}

func (s *TenantService) recordAudit(ctx context.Context, actor Actor, action admin.Action, targetID, tenantID string) {
	entry := &admin.AuditEntry{
		ActorID:  actor.ID,
		Action:   action,
		TargetID: targetID,
		TenantID: tenantID,
		IP:       actor.IP,
	}
	_ = s.audit.Record(ctx, entry)
}
