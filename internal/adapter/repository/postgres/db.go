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

// appQFromCtxOrPool retorna a pgx.Tx ativa armazenada no ctx por
// RunInTenantTx, ou cai para o pool quando a query é executada fora de
// uma transação tenant-scoped.
//
// UNSAFE: chamar esta função fora de um ctx produzido por RunInTenantTx
// é um BUG. Em mez_app (sem BYPASSRLS) a query subsequente falha com
// "no rows" porque mez.tenant_id não está setado — fail-closed correto,
// mas opaco. Em mez_platform (BYPASSRLS) a query silenciosamente retorna
// dados de outros tenants. Por isso o nome contém "OrPool" e este
// comentário UNSAFE: cada caller é responsável por garantir que está
// dentro de uma tx ativa.
//
// Issue #123 (3.11 do review DDD-Hex): renomeado de appQFromCtxOrPool para
// deixar a armadilha explícita.
func appQFromCtxOrPool(ctx context.Context, pool *pgxpool.Pool) querier {
	if tx, ok := ctx.Value(appTxKey).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// TxOption configura uma transação aberta por RunInTenantTx/BeginTenantTx.
// Pattern: functional options (Fase 0 — portado de mez-go).
type TxOption func(*txCfg)

type txCfg struct {
	isolation  pgx.TxIsoLevel
	deferAll   bool // SET CONSTRAINTS ALL DEFERRED (C6 — restore topológico)
}

func (c *txCfg) apply(opts []TxOption) {
	for _, o := range opts {
		o(c)
	}
}

// WithIsolation sobrescreve o isolation level (default ReadCommitted).
// Use WithRepeatableRead para backups (#81) — snapshot consistente do tenant.
func WithIsolation(level pgx.TxIsoLevel) TxOption {
	return func(c *txCfg) { c.isolation = level }
}

// WithRepeatableRead é um atalho para WithIsolation(RepeatableRead).
// Necessário no export (#81) para que o NDJSON seja gerado contra um
// snapshot consistente mesmo se houver writes concorrentes no tenant.
func WithRepeatableRead() TxOption {
	return WithIsolation(pgx.RepeatableRead)
}

// WithDeferConstraints adia a validação de FKs para o COMMIT (C6).
// Necessário no restore (#82) porque as tabelas filhas são inseridas antes
// das pais (ordem topológica reversa: contacts → conversations → messages).
// Só funciona se a FK foi criada DEFERRABLE INITIALLY DEFERRED — 0003 já fez.
func WithDeferConstraints() TxOption {
	return func(c *txCfg) { c.deferAll = true }
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

// RunInTenantTx abre tx com defaults (ReadCommitted, sem defer).
// Preservado para não quebrar callers existentes.
func (r *TxRunner) RunInTenantTx(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context) error) error {
	return r.RunInTenantTxWithOpts(ctx, tenantID, fn)
}

// RunInTenantTxWithOpts é a versão configurável (#81/#82 usam).
// Aceita WithRepeatableRead, WithDeferConstraints, etc.
func (r *TxRunner) RunInTenantTxWithOpts(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context) error, opts ...TxOption) error {
	cfg := txCfg{isolation: pgx.ReadCommitted}
	cfg.apply(opts)

	tx, err := r.appPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: cfg.isolation})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)", string(tenantID)); err != nil {
		return fmt.Errorf("set tenant_id: %w", err)
	}
	if cfg.deferAll {
		if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
			return fmt.Errorf("defer constraints: %w", err)
		}
	}

	ctx = context.WithValue(ctx, appTxKey, tx)
	if err := fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BeginTenantTx abre uma tx e devolve para o caller gerenciar. Usado por
// use cases que precisam de múltiplas operações sequenciais sem callback
// (ex.: export NDJSON com controle de stream). O caller é responsável por
// Commit/Rollback e por setar mez.tenant_id via SET LOCAL se quiser
// reutilizar appQFromCtxOrPool.
func (r *TxRunner) BeginTenantTx(ctx context.Context, tenantID domain.TenantID, opts ...TxOption) (pgx.Tx, context.Context, error) {
	cfg := txCfg{isolation: pgx.ReadCommitted}
	cfg.apply(opts)

	tx, err := r.appPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: cfg.isolation})
	if err != nil {
		return nil, ctx, fmt.Errorf("begin tx: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)", string(tenantID)); err != nil {
		_ = tx.Rollback(ctx)
		return nil, ctx, fmt.Errorf("set tenant_id: %w", err)
	}
	if cfg.deferAll {
		if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
			_ = tx.Rollback(ctx)
			return nil, ctx, fmt.Errorf("defer constraints: %w", err)
		}
	}
	return tx, context.WithValue(ctx, appTxKey, tx), nil
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
