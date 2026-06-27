package auth

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type LogoutUseCase interface {
	Logout(ctx context.Context, sessionID admin.SessionID) error
}

type LogoutService struct {
	sessions admin.SessionStore
	audit    admin.AuditRepo
}

func NewLogoutService(sessions admin.SessionStore, audit admin.AuditRepo) *LogoutService {
	return &LogoutService{sessions: sessions, audit: audit}
}

func (s *LogoutService) Logout(ctx context.Context, sessionID admin.SessionID) error {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil
	}

	if err := s.sessions.Delete(ctx, sessionID); err != nil {
		return err
	}

	s.recordAudit(ctx, session.UserID, session.Email)
	return nil
}

func (s *LogoutService) recordAudit(ctx context.Context, actorID admin.AdminUserID, email string) {
	entry := &admin.AuditEntry{
		ActorID: actorID,
		Action:  admin.ActionAuthLogout,
	}
	_ = s.audit.Record(ctx, entry)
}
