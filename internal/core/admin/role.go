package admin

import (
	"context"
	"time"
)

type RoleID string

type Scope string

const (
	ScopePlatform Scope = "platform"
	ScopeTenant   Scope = "tenant"
)

type Permission string

const (
	PermReadTenants    Permission = "tenants:read"
	PermCreateTenants  Permission = "tenants:create"
	PermUpdateTenants  Permission = "tenants:update"
	PermDeleteTenants  Permission = "tenants:delete"
	PermReadUsers      Permission = "users:read"
	PermCreateUsers    Permission = "users:create"
	PermUpdateUsers    Permission = "users:update"
	PermDeleteUsers    Permission = "users:delete"
	PermReadRoles      Permission = "roles:read"
	PermCreateRoles    Permission = "roles:create"
	PermUpdateRoles    Permission = "roles:update"
	PermReadAudit      Permission = "audit:read"
	PermReadSecrets    Permission = "secrets:read"
	PermCreateSecrets  Permission = "secrets:create"
	PermManageChannels Permission = "channels:manage"
)

type Role struct {
	ID          RoleID       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scope       Scope        `json:"scope"`
	TenantID    string       `json:"tenant_id,omitempty"`
	IsBuiltin   bool         `json:"is_builtin"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func (r Role) HasPermission(perm Permission) bool {
	for _, p := range r.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

func (r Role) IsPlatform() bool {
	return r.Scope == ScopePlatform
}

type RoleRepo interface {
	GetByID(ctx context.Context, id RoleID) (Role, error)
	ListBuiltins(ctx context.Context) ([]Role, error)
	ListByTenant(ctx context.Context, tenantID string) ([]Role, error)
	Insert(ctx context.Context, r *Role) error
	SetPermissions(ctx context.Context, id RoleID, permissions []Permission) error
	ListPermissions(ctx context.Context, id RoleID) ([]Permission, error)
}
