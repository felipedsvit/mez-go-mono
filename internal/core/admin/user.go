package admin

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type AdminUserID string

type AuthKind string

const (
	AuthKindLocal AuthKind = "local"
	AuthKindOIDC  AuthKind = "oidc"
)

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
	UserStatusInvited  UserStatus = "invited"
)

type RoleBinding struct {
	RoleID   RoleID `json:"role_id"`
	TenantID string `json:"tenant_id,omitempty"`
	Scope    Scope  `json:"scope"`
}

type AdminUser struct {
	ID           AdminUserID   `json:"id"`
	Email        string        `json:"email"`
	Name         string        `json:"name"`
	AuthKind     AuthKind      `json:"auth_kind"`
	Status       UserStatus    `json:"status"`
	PasswordHash string        `json:"-"`
	IDPSubject   string        `json:"idp_subject,omitempty"`
	IDPIssuer    string        `json:"idp_issuer,omitempty"`
	RoleBindings []RoleBinding `json:"role_bindings,omitempty"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type UserFilter struct {
	Status   *UserStatus
	Search   string
	TenantID string
	Limit    int
	Offset   int
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidEmail(email string) bool {
	return emailRegex.MatchString(email)
}

func (u AdminUser) IsLocal() bool {
	return u.AuthKind == AuthKindLocal
}

func (u AdminUser) IsOIDC() bool {
	return u.AuthKind == AuthKindOIDC
}

func (u AdminUser) IsActive() bool {
	return u.Status == UserStatusActive
}

type UserRepo interface {
	GetByID(ctx context.Context, id AdminUserID) (AdminUser, error)
	GetByEmail(ctx context.Context, email string) (AdminUser, error)
	GetByIDP(ctx context.Context, issuer, subject string) (AdminUser, error)
	List(ctx context.Context, filter UserFilter) ([]AdminUser, error)
	Insert(ctx context.Context, u *AdminUser) error
	UpdateLastLogin(ctx context.Context, id AdminUserID) error
	SetStatus(ctx context.Context, id AdminUserID, status UserStatus) error
	AssignRole(ctx context.Context, userID AdminUserID, roleID RoleID, tenantID string) error
	RevokeRole(ctx context.Context, userID AdminUserID, roleID RoleID, tenantID string) error
	ListRoleBindings(ctx context.Context, userID AdminUserID) ([]RoleBinding, error)
}
