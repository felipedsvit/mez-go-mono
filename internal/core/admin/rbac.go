package admin

import (
	"context"
	"strings"
)

// Principal is the authorization subject. It is built by the application
// layer from the session + DB lookups; the core does not know how to build
// it (no I/O).
//
// Permissions is a set (map) for O(1) lookup. Roles is kept as a slice
// because the size is small (typically 1-3) and we need iteration order for
// diagnostic purposes. TenantID is empty for platform-wide principals.
type Principal struct {
	UserID      AdminUserID             `json:"user_id"`
	Email       string                  `json:"email"`
	TenantID    string                  `json:"tenant_id,omitempty"`
	Permissions map[Permission]struct{} `json:"-"`
	Roles       []RoleBinding           `json:"roles"`
}

// Authorizer is the port used by middleware to evaluate a permission check.
// Concrete impls live in usecase/admin (use the same Evaluate pure function
// for the decision).
type Authorizer interface {
	Authorize(ctx context.Context, userID AdminUserID, tenantID string, perm Permission, scope Scope) (bool, error)
}

// HasPermission returns true if the principal holds the given permission.
func (p Principal) HasPermission(perm Permission) bool {
	if p.Permissions == nil {
		return false
	}
	_, ok := p.Permissions[perm]
	return ok
}

// IsPlatform returns true if the principal holds at least one platform-scoped
// role. The check is based on RoleBinding.Scope, not on string parsing of
// the role ID (the previous implementation used strings.Contains which was
// brittle: any role ID containing "platform" would qualify).
func (p Principal) IsPlatform() bool {
	for _, r := range p.Roles {
		if r.Scope == ScopePlatform {
			return true
		}
	}
	return false
}

// Evaluate is the pure RBAC decision function. The caller (usecase or
// middleware) builds the Principal; this function does not touch the DB.
//
// Rules (per mez-go reference + README §12):
//  1. The principal must hold the permission.
//  2. If the requested scope is platform, the principal must have at least
//     one platform-scoped role (tenant-scoped roles cannot do platform
//     actions, even if they technically have the permission key).
//  3. Tenant-scoped actions require either a tenant-scoped role binding
//     for the requested tenant, or any platform role (which covers).
func Evaluate(p Principal, perm Permission, scope Scope) bool {
	if !p.HasPermission(perm) {
		return false
	}
	switch scope {
	case ScopePlatform:
		return p.IsPlatform()
	case ScopeTenant:
		// Platform role covers any tenant.
		if p.IsPlatform() {
			return true
		}
		// Otherwise the principal must have a binding for this tenant.
		if p.TenantID == "" {
			return false
		}
		for _, r := range p.Roles {
			if r.Scope == ScopeTenant && (r.TenantID == "" || r.TenantID == p.TenantID) {
				return true
			}
		}
		return false
	}
	return false
}

// String redacts sensitive fields for log lines. Never log the full
// Permissions set — it can be large.
func (p Principal) String() string {
	roles := make([]string, 0, len(p.Roles))
	for _, r := range p.Roles {
		roles = append(roles, string(r.RoleID))
	}
	return "Principal{user=" + string(p.UserID) + ", email=" + p.Email +
		", tenant=" + p.TenantID + ", roles=[" + strings.Join(roles, ",") + "]}"
}
