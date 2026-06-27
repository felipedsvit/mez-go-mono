package admin

import (
	"context"
	"time"
)

type SessionID string

type Session struct {
	ID        SessionID   `json:"id"`
	UserID    AdminUserID `json:"user_id"`
	Email     string      `json:"email"`
	ExpiresAt time.Time   `json:"expires_at"`
	CreatedAt time.Time   `json:"created_at"`
}

type SessionStore interface {
	Save(ctx context.Context, session Session, ttl time.Duration) error
	Get(ctx context.Context, id SessionID) (Session, error)
	Delete(ctx context.Context, id SessionID) error
}
