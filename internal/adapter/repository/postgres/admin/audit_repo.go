package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type AuditRepo struct {
	db *DB
}

func (r *AuditRepo) Record(ctx context.Context, entry *admin.AuditEntry) error {
	return r.insert(ctx, r.db.pool, entry)
}

// RecordWithTx inserts the audit row inside the given transaction (C5 atomic
// audit pattern). If tx is nil, returns admin.ErrNotInTransaction so the
// caller can fall back to Record (best-effort) or surface the error.
func (r *AuditRepo) RecordWithTx(ctx context.Context, tx admin.Tx, entry *admin.AuditEntry) error {
	if tx == nil {
		return admin.ErrNotInTransaction
	}
	pgxTx, ok := tx.(pgxTx)
	if !ok {
		return fmt.Errorf("audit: tx is not a pgx.Tx: %T", tx)
	}
	return r.insert(ctx, pgxTx, entry)
}

type pgxTx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (r *AuditRepo) insert(ctx context.Context, q pgxTx, entry *admin.AuditEntry) error {
	query := `INSERT INTO admin_audit_log (id, actor_id, actor_email, action, target_type, target_id, tenant_id, metadata, ip, user_agent, created_at)
		VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), NULLIF($10, ''), $11)`

	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}

	_, err := q.Exec(ctx, query,
		entry.ID, string(entry.ActorID), entry.ActorEmail, string(entry.Action),
		entry.TargetType, entry.TargetID, entry.TenantID, entry.Metadata,
		entry.IP, entry.UserAgent, entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record audit: %w", err)
	}

	return nil
}

func (r *AuditRepo) List(ctx context.Context, filter admin.AuditFilter) ([]admin.AuditEntry, error) {
	query := `SELECT id, actor_id, action, COALESCE(target_id, ''), COALESCE(tenant_id, ''), metadata, COALESCE(ip, ''), COALESCE(user_agent, ''), created_at
		FROM admin_audit_log WHERE 1=1`
	args := []any{}
	argN := 1

	if filter.ActorID != nil {
		query += fmt.Sprintf(" AND actor_id = $%d", argN)
		args = append(args, string(*filter.ActorID))
		argN++
	}
	if filter.Action != nil {
		query += fmt.Sprintf(" AND action = $%d", argN)
		args = append(args, string(*filter.Action))
		argN++
	}
	if filter.TenantID != "" {
		query += fmt.Sprintf(" AND tenant_id = $%d", argN)
		args = append(args, filter.TenantID)
		argN++
	}

	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	query += fmt.Sprintf(" LIMIT $%d", argN)
	args = append(args, limit)
	argN++

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argN)
		args = append(args, filter.Offset)
	}

	rows, err := r.db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	defer rows.Close()

	var entries []admin.AuditEntry
	for rows.Next() {
		var e admin.AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.TargetID, &e.TenantID, &e.Metadata, &e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		entries = append(entries, e)
	}

	return entries, nil
}
