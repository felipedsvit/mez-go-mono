package admin

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type AuditQueryUseCase interface {
	List(ctx context.Context, filter admin.AuditFilter) ([]admin.AuditEntry, error)
}

type AuditQueryService struct {
	audit admin.AuditRepo
}

func NewAuditQueryService(audit admin.AuditRepo) *AuditQueryService {
	return &AuditQueryService{audit: audit}
}

func (s *AuditQueryService) List(ctx context.Context, filter admin.AuditFilter) ([]admin.AuditEntry, error) {
	return s.audit.List(ctx, filter)
}
