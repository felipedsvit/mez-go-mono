package admin

import (
	"context"
	"errors"
)

// Tx is the transaction abstraction. The application code uses this interface
// instead of importing pgx directly, so C5 atomic-audit pattern works even if
// the underlying driver changes. Tx implements Commit/Rollback and provides
// access to a queryable handle.
type Tx interface {
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// TxRunner opens a transaction and runs fn within it. The run-as-platform
// wrapper (RunAsPlatform) uses this; per-tenant paths use RunInTenantTx.
type TxRunner interface {
	// RunInTenantTx begins a tx, sets mez.tenant_id to tenantID (LOCAL), then
	// invokes fn. The tenant_id setting is reset automatically on commit/rollback.
	RunInTenantTx(ctx context.Context, tenantID string, fn func(ctx context.Context) error) error
	// RunAsPlatform begins a tx on the platform pool, writes an audit row
	// recording the access (C5), then invokes fn. If the audit write fails
	// the entire op is aborted before fn runs.
	RunAsPlatform(ctx context.Context, actor Actor, action Action, targetID, targetType, tenantID string, fn func(ctx context.Context) error) error
}

// Actor is the audit envelope. Email is always set; ID is optional (empty for
// anonymous flows like /setup bootstrap, where the row is recorded but the
// actor doesn't exist yet at insert time).
type Actor struct {
	ID    AdminUserID
	Email string
	IP    string
}

// ErrNotInTransaction is returned when AuditRepo.RecordWithTx is called with
// a nil tx. Callers should fall back to Record in that case (or surface the
// error, depending on the audit policy).
var ErrNotInTransaction = errors.New("audit: not in transaction")
