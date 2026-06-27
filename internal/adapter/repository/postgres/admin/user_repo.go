package admin

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type UserRepo struct {
	db *DB
}

func scanUser(row pgx.Row) (admin.AdminUser, error) {
	var u admin.AdminUser
	var passwordHash, idpSubject, idpIssuer *string

	err := row.Scan(
		&u.ID, &u.Email, &u.Name, &u.AuthKind, &u.Status,
		&passwordHash, &idpSubject, &idpIssuer,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return u, err
	}

	if passwordHash != nil {
		u.PasswordHash = *passwordHash
	}
	if idpSubject != nil {
		u.IDPSubject = *idpSubject
	}
	if idpIssuer != nil {
		u.IDPIssuer = *idpIssuer
	}

	return u, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id admin.AdminUserID) (admin.AdminUser, error) {
	query := `SELECT id, email, name, auth_kind, status, password_hash, idp_subject, idp_issuer, created_at, updated_at
		FROM admin_users WHERE id = $1`

	u, err := scanUser(r.db.pool.QueryRow(ctx, query, string(id)))
	if err != nil {
		if err == pgx.ErrNoRows {
			return u, fmt.Errorf("admin user %q: %w", id, admin.ErrNotFound)
		}
		return u, fmt.Errorf("get admin user %q: %w", id, err)
	}
	return u, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (admin.AdminUser, error) {
	query := `SELECT id, email, name, auth_kind, status, password_hash, idp_subject, idp_issuer, created_at, updated_at
		FROM admin_users WHERE email = $1`

	u, err := scanUser(r.db.pool.QueryRow(ctx, query, email))
	if err != nil {
		if err == pgx.ErrNoRows {
			return u, fmt.Errorf("admin user %q: %w", email, admin.ErrNotFound)
		}
		return u, fmt.Errorf("get admin user by email %q: %w", email, err)
	}
	return u, nil
}

func (r *UserRepo) GetByIDP(ctx context.Context, issuer, subject string) (admin.AdminUser, error) {
	query := `SELECT id, email, name, auth_kind, status, password_hash, idp_subject, idp_issuer, created_at, updated_at
		FROM admin_users WHERE idp_issuer = $1 AND idp_subject = $2`

	u, err := scanUser(r.db.pool.QueryRow(ctx, query, issuer, subject))
	if err != nil {
		if err == pgx.ErrNoRows {
			return u, fmt.Errorf("admin user idp %s/%s: %w", issuer, subject, admin.ErrNotFound)
		}
		return u, fmt.Errorf("get admin user by idp %s/%s: %w", issuer, subject, err)
	}
	return u, nil
}

func (r *UserRepo) List(ctx context.Context, filter admin.UserFilter) ([]admin.AdminUser, error) {
	query := `SELECT id, email, name, auth_kind, status, password_hash, idp_subject, idp_issuer, created_at, updated_at
		FROM admin_users WHERE 1=1`
	args := []any{}
	argN := 1

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argN)
		args = append(args, string(*filter.Status))
		argN++
	}
	if filter.Search != "" {
		query += fmt.Sprintf(" AND (email ILIKE $%d OR name ILIKE $%d)", argN, argN)
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
		return nil, fmt.Errorf("list admin users: %w", err)
	}
	defer rows.Close()

	var users []admin.AdminUser
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan admin user: %w", err)
		}
		users = append(users, u)
	}

	return users, nil
}

func (r *UserRepo) Insert(ctx context.Context, u *admin.AdminUser) error {
	if u.ID == "" {
		u.ID = admin.AdminUserID(uuid.NewString())
	}

	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now

	const query = `INSERT INTO admin_users (id, email, name, auth_kind, status, password_hash, idp_subject, idp_issuer, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	_, err := r.db.pool.Exec(ctx, query,
		string(u.ID), u.Email, u.Name, string(u.AuthKind), string(u.Status),
		nullif(u.PasswordHash), nullif(u.IDPSubject), nullif(u.IDPIssuer),
		u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("insert admin user %q: %w", u.Email, admin.ErrEmailTaken)
		}
		return fmt.Errorf("insert admin user %q: %w", u.Email, err)
	}
	return nil
}

func (r *UserRepo) UpdateLastLogin(ctx context.Context, id admin.AdminUserID) error {
	query := `UPDATE admin_users SET updated_at = $1 WHERE id = $2`
	_, err := r.db.pool.Exec(ctx, query, time.Now().UTC(), string(id))
	if err != nil {
		return fmt.Errorf("update last login %q: %w", id, err)
	}
	return nil
}

func (r *UserRepo) SetStatus(ctx context.Context, id admin.AdminUserID, status admin.UserStatus) error {
	query := `UPDATE admin_users SET status = $1, updated_at = $2 WHERE id = $3`
	tag, err := r.db.pool.Exec(ctx, query, string(status), time.Now().UTC(), string(id))
	if err != nil {
		return fmt.Errorf("set user status %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set user status %q: %w", id, admin.ErrNotFound)
	}
	return nil
}

func (r *UserRepo) AssignRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string) error {
	query := `INSERT INTO admin_user_roles (user_id, role_id, tenant_id) VALUES ($1, $2, NULLIF($3, '')) ON CONFLICT DO NOTHING`
	_, err := r.db.pool.Exec(ctx, query, string(userID), string(roleID), tenantID)
	if err != nil {
		return fmt.Errorf("assign role %s to user %s: %w", roleID, userID, err)
	}
	return nil
}

func (r *UserRepo) RevokeRole(ctx context.Context, userID admin.AdminUserID, roleID admin.RoleID, tenantID string) error {
	query := `DELETE FROM admin_user_roles WHERE user_id = $1 AND role_id = $2 AND (tenant_id = $3 OR ($3 = '' AND tenant_id IS NULL))`
	_, err := r.db.pool.Exec(ctx, query, string(userID), string(roleID), tenantID)
	if err != nil {
		return fmt.Errorf("revoke role %s from user %s: %w", roleID, userID, err)
	}
	return nil
}

func (r *UserRepo) ListRoleBindings(ctx context.Context, userID admin.AdminUserID) ([]admin.RoleBinding, error) {
	query := `SELECT role_id, COALESCE(tenant_id, ''), 
		CASE WHEN tenant_id IS NULL THEN 'platform' ELSE 'tenant' END
		FROM admin_user_roles WHERE user_id = $1`

	rows, err := r.db.pool.Query(ctx, query, string(userID))
	if err != nil {
		return nil, fmt.Errorf("list role bindings %s: %w", userID, err)
	}
	defer rows.Close()

	var bindings []admin.RoleBinding
	for rows.Next() {
		var b admin.RoleBinding
		var tenantID string
		if err := rows.Scan(&b.RoleID, &tenantID, &b.Scope); err != nil {
			return nil, fmt.Errorf("scan role binding: %w", err)
		}
		b.TenantID = tenantID
		bindings = append(bindings, b)
	}

	return bindings, nil
}

func nullif(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isUniqueViolation(err error) bool {
	return err != nil && (contains(err.Error(), "23505") || contains(err.Error(), "unique constraint"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
