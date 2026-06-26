package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// querier is the common interface satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type ctxKey int

const appTxKey ctxKey = iota

// appQFromCtx returns the active pgx.Tx stored in ctx by RunInTenantTx, or
// falls back to pool for queries executed outside a tenant transaction.
func appQFromCtx(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx, ok := ctx.Value(appTxKey).(pgx.Tx); ok {
		return tx
	}
	return pool
}

type TxRunner struct {
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
	log          zerolog.Logger
}

func NewTxRunner(appPool, platformPool *pgxpool.Pool, log zerolog.Logger) *TxRunner {
	return &TxRunner{
		appPool:      appPool,
		platformPool: platformPool,
		log:          log,
	}
}

func (r *TxRunner) RunInTenantTx(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context) error) error {
	tx, err := r.appPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)", string(tenantID)); err != nil {
		return fmt.Errorf("set tenant_id: %w", err)
	}

	ctx = context.WithValue(ctx, appTxKey, tx)
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *TxRunner) RunAsPlatform(ctx context.Context, actor string, fn func(ctx context.Context) error) error {
	tx, err := r.platformPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin platform tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := writeAuditLog(ctx, tx, actor, "platform_access"); err != nil {
		return fmt.Errorf("write platform audit: %w", err)
	}

	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func writeAuditLog(ctx context.Context, tx pgx.Tx, actor, action string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO audit_log (actor, action, created_at) VALUES ($1, $2, NOW())`,
		actor, action,
	)
	return err
}

func ConnectPool(ctx context.Context, dsn string, maxConns int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = int32(maxConns)
	cfg.MinConns = 2

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}
