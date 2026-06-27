package admin

import (
	"context"
	"time"
)

type Action string

const (
	ActionAuthLoginSuccess Action = "auth.login.success"
	ActionAuthLoginFailure Action = "auth.login.failure"
	ActionAuthLogout       Action = "auth.logout"
	// ActionSetupBootstrap is emitted when the /setup wizard creates the first admin.
	ActionSetupBootstrap   Action = "setup.bootstrap"
	ActionSetupRebootstrap Action = "setup.rebootstrap"
	ActionTenantCreate     Action = "tenant:create"
	ActionTenantUpdate     Action = "tenant:update"
	ActionTenantStatus     Action = "tenant:status"
	ActionTenantList       Action = "tenant:list"
	ActionUserCreate       Action = "user:create"
	ActionUserStatus       Action = "user:status"
	ActionUserRoleAssign   Action = "user:role.assign"
	ActionUserRoleRevoke   Action = "user:role.revoke"
	ActionRoleCreate       Action = "role:create"
	ActionRolePermissions  Action = "role:permissions"
	// ActionPlatformAccess is emitted by the RunAsPlatform wrapper (C5).
	// Every cross-tenant operation generates this entry before the operation
	// runs; if the operation fails, the wrapper rolls back, including the
	// audit row (atomic). Without the wrapper, the audit row is missing.
	ActionPlatformAccess Action = "platform:access"
)

func (a Action) Valid() bool {
	switch a {
	case ActionAuthLoginSuccess, ActionAuthLoginFailure, ActionAuthLogout,
		ActionSetupBootstrap, ActionSetupRebootstrap,
		ActionTenantCreate, ActionTenantUpdate, ActionTenantStatus, ActionTenantList,
		ActionUserCreate, ActionUserStatus, ActionUserRoleAssign, ActionUserRoleRevoke,
		ActionRoleCreate, ActionRolePermissions,
		ActionPlatformAccess:
		return true
	default:
		return false
	}
}

// AuditEntry is a single row in admin_audit_log. The combination of
// (actor_id, action, target_id, tenant_id) is a natural composite key for
// dedup-on-insert (the caller is expected to provide a UUID).
//
// actor_email is denormalized: a deleted user (ON DELETE SET NULL) must still
// leave a readable trail. tenant_id is nullable for platform-wide actions
// (NULL).
type AuditEntry struct {
	ID         string         `json:"id"`
	ActorID    AdminUserID    `json:"actor_id,omitempty"`
	ActorEmail string         `json:"actor_email"`
	Action     Action         `json:"action"`
	TargetType string         `json:"target_type,omitempty"`
	TargetID   string         `json:"target_id,omitempty"`
	TenantID   string         `json:"tenant_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	IP         string         `json:"ip,omitempty"`
	UserAgent  string         `json:"user_agent,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AuditFilter struct {
	ActorID  *AdminUserID
	Action   *Action
	TenantID string
	Limit    int
	Offset   int
}

// AuditRepo writes and reads AuditEntry. The RecordWithTx method exists so
// usecases can include audit in the SAME transaction as the mutation it
// audits (C5: atomic). Record is a best-effort convenience for cases that
// cannot open a tx (e.g. login failure where the underlying user lookup
// itself failed and we want to record the failed attempt regardless).
type AuditRepo interface {
	Record(ctx context.Context, entry *AuditEntry) error
	RecordWithTx(ctx context.Context, tx Tx, entry *AuditEntry) error
	List(ctx context.Context, filter AuditFilter) ([]AuditEntry, error)
}
