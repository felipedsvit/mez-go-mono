package admin

import (
	"context"
	"errors"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// ErrTenantRequired é retornado quando o caller pede audit sem
// especificar tenantID e sem flag CrossTenant. Issue #154 (M8 audit,
// Sprint 0C).
var ErrTenantRequired = errors.New("audit: tenantID required for tenant-scoped caller")

// ErrLimitTooHigh é retornado quando limit > maxLimit. Issue #154.
var ErrLimitTooHigh = errors.New("audit: limit too high")

// maxAuditLimit é o cap máximo de audit entries por query (anti-DoS).
const maxAuditLimit = 1000

// defaultAuditLimit é aplicado se caller não especificar.
const defaultAuditLimit = 100

// AuditQueryUseCase interface estendida: caller passa CrossTenant bool
// indicando se tem permissão platform (perfeito audit cross-tenant).
type AuditQueryUseCase interface {
	List(ctx context.Context, filter admin.AuditFilter, crossTenant bool) ([]admin.AuditEntry, error)
}

type AuditQueryService struct {
	audit admin.AuditRepo
}

func NewAuditQueryService(audit admin.AuditRepo) *AuditQueryService {
	return &AuditQueryService{audit: audit}
}

// List implementa o gate fail-closed do #154:
//   - Se crossTenant=false e filter.TenantID vazio → ErrTenantRequired
//   - Se filter.Limit > maxAuditLimit → ErrLimitTooHigh
//   - Se filter.Limit <= 0 → defaultAuditLimit
//
// Caller (HTTP handler) deve passar crossTenant=true apenas se
// validou PermAuditRead com ScopePlatform (RequireScope middleware,
// #132 Sprint 0A).
func (s *AuditQueryService) List(ctx context.Context, filter admin.AuditFilter, crossTenant bool) ([]admin.AuditEntry, error) {
	if !crossTenant && filter.TenantID == "" {
		return nil, ErrTenantRequired
	}
	if filter.Limit > maxAuditLimit {
		return nil, ErrLimitTooHigh
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultAuditLimit
	}
	return s.audit.List(ctx, filter)
}
