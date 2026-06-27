package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type TenantRepo struct {
	db *DB
}

func scanTenant(row pgx.Row) (admin.Tenant, error) {
	var t admin.Tenant
	err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (r *TenantRepo) GetByID(ctx context.Context, id string) (admin.Tenant, error) {
	query := `SELECT id, name, slug, status, created_at, updated_at FROM tenants WHERE id = $1`
	t, err := scanTenant(r.db.pool.QueryRow(ctx, query, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return t, fmt.Errorf("tenant %q: %w", id, admin.ErrNotFound)
		}
		return t, fmt.Errorf("get tenant %q: %w", id, err)
	}
	return t, nil
}

func (r *TenantRepo) GetBySlug(ctx context.Context, slug string) (admin.Tenant, error) {
	query := `SELECT id, name, slug, status, created_at, updated_at FROM tenants WHERE slug = $1`
	t, err := scanTenant(r.db.pool.QueryRow(ctx, query, slug))
	if err != nil {
		if err == pgx.ErrNoRows {
			return t, fmt.Errorf("tenant slug %q: %w", slug, admin.ErrNotFound)
		}
		return t, fmt.Errorf("get tenant by slug %q: %w", slug, err)
	}
	return t, nil
}

func (r *TenantRepo) List(ctx context.Context, filter admin.TenantFilter) ([]admin.Tenant, error) {
	query := `SELECT id, name, slug, status, created_at, updated_at FROM tenants WHERE 1=1`
	args := []any{}
	argN := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argN)
		args = append(args, string(*filter.Status))
		argN++
	}
	if filter.Search != "" {
		query += fmt.Sprintf(" AND (name ILIKE $%d OR slug ILIKE $%d)", argN, argN)
		args = append(args, "%"+filter.Search+"%")
		argN++
	}

	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
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
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []admin.Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, t)
	}

	return tenants, nil
}

func (r *TenantRepo) Create(ctx context.Context, t *admin.Tenant) error {
	query := `INSERT INTO tenants (id, name, slug, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`

	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err := r.db.pool.Exec(ctx, query, t.ID, t.Name, t.Slug, string(t.Status), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create tenant %q: %w", t.Slug, admin.ErrTenantExists)
		}
		return fmt.Errorf("create tenant %q: %w", t.Slug, err)
	}
	return nil
}

func (r *TenantRepo) UpdateProfile(ctx context.Context, id, name, slug string) error {
	query := `UPDATE tenants SET name = $1, slug = $2, updated_at = $3 WHERE id = $4`
	tag, err := r.db.pool.Exec(ctx, query, name, slug, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update tenant %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update tenant %q: %w", id, admin.ErrNotFound)
	}
	return nil
}

func (r *TenantRepo) SetStatus(ctx context.Context, id string, status admin.TenantStatus) error {
	query := `UPDATE tenants SET status = $1, updated_at = $2 WHERE id = $3`
	tag, err := r.db.pool.Exec(ctx, query, string(status), time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("set tenant status %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set tenant status %q: %w", id, admin.ErrNotFound)
	}
	return nil
}
