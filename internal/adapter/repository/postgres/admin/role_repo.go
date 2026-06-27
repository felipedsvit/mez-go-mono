package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type RoleRepo struct {
	db *DB
}

func scanRole(row pgx.Row) (admin.Role, error) {
	var r admin.Role
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Scope, &r.TenantID, &r.IsBuiltin, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, err
	}
	return r, nil
}

func (r *RoleRepo) GetByID(ctx context.Context, id admin.RoleID) (admin.Role, error) {
	query := `SELECT id, name, description, scope, COALESCE(tenant_id, ''), is_builtin, created_at, updated_at
		FROM admin_roles WHERE id = $1`

	role, err := scanRole(r.db.pool.QueryRow(ctx, query, string(id)))
	if err != nil {
		if err == pgx.ErrNoRows {
			return role, fmt.Errorf("role %q: %w", id, admin.ErrNotFound)
		}
		return role, fmt.Errorf("get role %q: %w", id, err)
	}

	perms, err := r.ListPermissions(ctx, id)
	if err != nil {
		return role, err
	}
	role.Permissions = perms

	return role, nil
}

func (r *RoleRepo) ListBuiltins(ctx context.Context) ([]admin.Role, error) {
	query := `SELECT id, name, description, scope, COALESCE(tenant_id, ''), is_builtin, created_at, updated_at
		FROM admin_roles WHERE is_builtin = true ORDER BY name`

	return r.listRoles(ctx, query)
}

func (r *RoleRepo) ListByTenant(ctx context.Context, tenantID string) ([]admin.Role, error) {
	query := `SELECT id, name, description, scope, COALESCE(tenant_id, ''), is_builtin, created_at, updated_at
		FROM admin_roles WHERE tenant_id = $1 ORDER BY name`

	rows, err := r.db.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list roles by tenant %q: %w", tenantID, err)
	}
	defer rows.Close()

	var roles []admin.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		perms, _ := r.ListPermissions(ctx, role.ID)
		role.Permissions = perms
		roles = append(roles, role)
	}

	return roles, nil
}

func (r *RoleRepo) listRoles(ctx context.Context, query string, args ...any) ([]admin.Role, error) {
	rows, err := r.db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []admin.Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}

func (r *RoleRepo) Insert(ctx context.Context, role *admin.Role) error {
	query := `INSERT INTO admin_roles (id, name, description, scope, tenant_id, is_builtin, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8)`

	if role.ID == "" {
		// Issue #157 (Security M15, CWE-330/340): Role ID anterior era
		// `role_<unixnano-hex>`, previsível e collisivo em inserts
		// concorrentes. UUIDv4 random é o padrão usado em user_repo.go
		// e admin_audit_log (id).
		role.ID = admin.RoleID("role_" + uuid.NewString())
	}

	now := time.Now().UTC()
	role.CreatedAt = now
	role.UpdatedAt = now

	_, err := r.db.pool.Exec(ctx, query,
		string(role.ID), role.Name, role.Description, string(role.Scope),
		role.TenantID, role.IsBuiltin, role.CreatedAt, role.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert role %q: %w", role.Name, err)
	}

	return nil
}

func (r *RoleRepo) SetPermissions(ctx context.Context, id admin.RoleID, permissions []admin.Permission) error {
	tx, err := r.db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_id = $1`, string(id))
	if err != nil {
		return fmt.Errorf("delete permissions: %w", err)
	}

	for _, perm := range permissions {
		_, err = tx.Exec(ctx, `INSERT INTO admin_role_permissions (role_id, permission) VALUES ($1, $2)`, string(id), string(perm))
		if err != nil {
			return fmt.Errorf("insert permission %q: %w", perm, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit permissions: %w", err)
	}

	return nil
}

func (r *RoleRepo) ListPermissions(ctx context.Context, id admin.RoleID) ([]admin.Permission, error) {
	query := `SELECT permission FROM admin_role_permissions WHERE role_id = $1 ORDER BY permission`

	rows, err := r.db.pool.Query(ctx, query, string(id))
	if err != nil {
		return nil, fmt.Errorf("list permissions %q: %w", id, err)
	}
	defer rows.Close()

	var perms []admin.Permission
	for rows.Next() {
		var p admin.Permission
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}

	return perms, nil
}
