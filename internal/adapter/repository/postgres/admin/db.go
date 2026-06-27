package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type DB struct {
	pool *pgxpool.Pool
}

func NewDB(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("admin db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("admin db ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

func (db *DB) Close() {
	db.pool.Close()
}

// pgxTxAdapter wraps pgx.Tx to satisfy admin.Tx.
type pgxTxAdapter struct {
	tx pgx.Tx
}

func (a *pgxTxAdapter) Commit(ctx context.Context) error   { return a.tx.Commit(ctx) }
func (a *pgxTxAdapter) Rollback(ctx context.Context) error { return a.tx.Rollback(ctx) }

// pgxQueryer is implemented by both *pgxpool.Pool and pgx.Tx so we can use a
// single helper for inserts that work in or out of a transaction.
type pgxQueryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// RunAsPlatform is the C5 wrapper: opens a transaction on the platform pool
// (mez_platform with BYPASSRLS), writes a platform:access audit row FIRST,
// then runs fn. If the audit write fails, fn does not run. If fn fails, the
// entire transaction (audit included) is rolled back — so an admin
// cross-tenant action without an audit trail is impossible.
//
// The audit row is atomic with the mutation, eliminating the mez-go pattern
// of out-of-band `_ = s.audit.Record(...)` that could lose audit on crash.
func (db *DB) RunAsPlatform(
	ctx context.Context,
	actor admin.Actor,
	action admin.Action,
	targetID, targetType, tenantID string,
	fn func(ctx context.Context) error,
) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("run-as-platform begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// 1) Pre-write the platform:access audit row in the SAME tx. If this
	//    fails (rare; could be a constraint or connection issue), the
	//    operation is aborted before any mutation runs.
	entry := &admin.AuditEntry{
		ID:         uuid.NewString(),
		ActorID:    actor.ID,
		ActorEmail: actor.Email,
		Action:     admin.ActionPlatformAccess,
		TargetType: targetType,
		TargetID:   targetID,
		TenantID:   tenantID,
		IP:         actor.IP,
		CreatedAt:  time.Now().UTC(),
		Metadata: map[string]any{
			"requested_action": string(action),
		},
	}
	const auditSQL = `INSERT INTO admin_audit_log
		(id, actor_id, actor_email, action, target_type, target_id, tenant_id, metadata, ip, user_agent, created_at)
		VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), '', $10)`
	if _, err := tx.Exec(ctx, auditSQL,
		entry.ID, string(entry.ActorID), entry.ActorEmail, string(entry.Action),
		entry.TargetType, entry.TargetID, entry.TenantID, entry.Metadata, entry.IP, entry.CreatedAt,
	); err != nil {
		return fmt.Errorf("run-as-platform audit: %w", err)
	}

	// 2) Run the actual mutation. It will see the same tx; if it uses
	//    RecordWithTx for its own audit, both rows commit atomically.
	if err := fn(ctx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("run-as-platform commit: %w", err)
	}
	committed = true
	return nil
}

// RunInTenantTx begins a tx and sets mez.tenant_id to tenantID (LOCAL), then
// invokes fn. Used by per-tenant paths that need to live inside an RLS scope.
// tenantID="" is rejected (fail-closed; use RunAsPlatform for cross-tenant).
func (db *DB) RunInTenantTx(ctx context.Context, tenantID string, fn func(ctx context.Context) error) error {
	if tenantID == "" {
		return errors.New("run-in-tenant-tx: tenantID required")
	}
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("run-in-tenant-tx begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// set_config(name, value, is_local) where is_local=true means the
	// setting is reset when the current transaction ends — safe with pooling.
	if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)", tenantID); err != nil {
		return fmt.Errorf("run-in-tenant-tx set tenant_id: %w", err)
	}

	if err := fn(ctx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("run-in-tenant-tx commit: %w", err)
	}
	committed = true
	return nil
}

// WithTx is a generic helper for cases that need a tx but are not
// RunAsPlatform-shaped (e.g. multi-step mutations that should still be
// atomic but are tenant-scoped). It does NOT pre-write an audit row — the
// caller is responsible for using RecordWithTx on its own audit step.
func (db *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("with-tx begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if err := fn(ctx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("with-tx commit: %w", err)
	}
	committed = true
	return nil
}

// WithTxAdapter returns a function that wraps a pgx.Tx as admin.Tx, suitable
// for passing to RecordWithTx.
func WithTxAdapter(tx pgx.Tx) admin.Tx {
	return &pgxTxAdapter{tx: tx}
}

type Repositories struct {
	Users   *UserRepo
	Roles   *RoleRepo
	Audit   *AuditRepo
	Tenants *TenantRepo
}

func NewRepositories(db *DB) *Repositories {
	return &Repositories{
		Users:   &UserRepo{db: db},
		Roles:   &RoleRepo{db: db},
		Audit:   &AuditRepo{db: db},
		Tenants: &TenantRepo{db: db},
	}
}
