package auth

import (
	"context"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type SessionUseCase interface {
	Resolve(ctx context.Context, sessionID admin.SessionID) (admin.Session, error)
}

type SessionService struct {
	sessions   admin.SessionStore
	users      admin.UserRepo
	sessionTTL time.Duration
}

func NewSessionService(sessions admin.SessionStore, users admin.UserRepo, sessionTTL time.Duration) *SessionService {
	return &SessionService{
		sessions:   sessions,
		users:      users,
		sessionTTL: sessionTTL,
	}
}

func (s *SessionService) Resolve(ctx context.Context, sessionID admin.SessionID) (admin.Session, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return admin.Session{}, admin.ErrSessionExpired
	}

	if time.Now().After(session.ExpiresAt) {
		_ = s.sessions.Delete(ctx, sessionID)
		return admin.Session{}, admin.ErrSessionExpired
	}

	if err := s.sessions.Save(ctx, session, s.sessionTTL); err != nil {
		return admin.Session{}, err
	}

	return session, nil
}
