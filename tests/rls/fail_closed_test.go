//go:build integration

// Package rls contains the fail-closed RLS regression test (issue #14,
// C3/C4 + Phase 1 admin tables). It uses testcontainers to spin up a real
// Postgres 16, applies migrations/0001_init.up.sql + 0002_admin.up.sql,
// creates the three roles (mez_migrate, mez_app, mez_platform), and asserts:
//
//  1. mez_app without mez.tenant_id CANNOT read messages (fail-closed).
//  2. mez_app with mez.tenant_id=A can read A but not B (RLS filter, not error).
//  3. mez_platform CAN read cross-tenant (BYPASSRLS, audited).
//  4. mez_migrate (table owner) STILL has RLS enforced (FORCE RLS — C3).
//  5. mez_app can SELECT admin_users (read access for LoginService) but
//     CANNOT INSERT/UPDATE/DELETE (write blocked — only RunAsPlatform,
//     which uses mez_platform, can write).
//  6. mez_platform can write admin_users freely (BYPASSRLS).
//  7. mez_app CAN insert into admin_audit_log (login failure audit) but
//     CANNOT update/delete (append-only via REVOKE — defense in depth).
//
// Run:
//
//	make test-integration
//	# or
//	go test -tags integration -race -count=1 -shuffle=on -timeout 5m ./tests/rls/...
package rls

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgImage          = "postgres:16-alpine"
	migrationsDir    = "migrations"
	migrationFile    = "0001_init.up.sql"
	adminMigration   = "0002_admin.up.sql"
	appPassword      = "mez_app_pwd"
	migratePassword  = "mez_migrate_pwd"
	platformPassword = "mez_platform_pwd"
)

// TestRLSFailClosed is the suite entry point — runs each subtest in order.
func TestRLSFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 1. Spin up Postgres 16.
	pgC, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage(pgImage),
		tcpostgres.WithDatabase("mez"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("testcontainers: cannot start postgres (docker daemon?): %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	// 2. Connect as superuser to bootstrap roles + apply migration.
	superDSN, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	superPool, err := pgxpool.New(ctx, superDSN)
	if err != nil {
		t.Fatalf("super pool: %v", err)
	}
	t.Cleanup(superPool.Close)

	if err := applyMigrations(ctx, superPool); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if err := setRolePasswords(ctx, superPool); err != nil {
		t.Fatalf("set role passwords: %v", err)
	}

	// 3. Create two tenants and seed messages for each.
	tenantA, tenantB, err := seedTenants(ctx, superPool)
	if err != nil {
		t.Fatalf("seed tenants: %v", err)
	}

	// 4. Open three pools under the three roles. Build the DSN manually
	//    from the superuser connection so we can swap user/password.
	superURL, err := url.Parse(superDSN)
	if err != nil {
		t.Fatalf("parse super dsn: %v", err)
	}
	host := superURL.Hostname()
	port := superURL.Port()
	if port == "" {
		port = "5432"
	}
	dbName := strings.TrimPrefix(superURL.Path, "/")

	appPool := mustPool(ctx, t, buildDSN(host, port, dbName, "mez_app", appPassword))
	t.Cleanup(appPool.Close)

	platformPool := mustPool(ctx, t, buildDSN(host, port, dbName, "mez_platform", platformPassword))
	t.Cleanup(platformPool.Close)

	migratePool := mustPool(ctx, t, buildDSN(host, port, dbName, "mez_migrate", migratePassword))
	t.Cleanup(migratePool.Close)

	t.Run("MezApp_WithoutTenantContext_Fails", func(t *testing.T) {
		// C4: mez.tenant_id is NOT set. RLS policy uses current_setting(..., false)
		// which raises an error when the GUC is absent → query MUST fail.
		var n int
		err := appPool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n)
		if err == nil {
			t.Fatalf("expected error without mez.tenant_id (C4 fail-closed); got count=%d", n)
		}
		t.Logf("OK: mez_app without mez.tenant_id → %v", err)
	})

	t.Run("MezApp_WithTenantA_OnlySeesA", func(t *testing.T) {
		// Set GUC for tenant A and verify RLS filters to A only.
		if _, err := appPool.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, false)", tenantA); err != nil {
			t.Fatalf("set tenant_id: %v", err)
		}
		var n int
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE tenant_id = $1`, tenantA).Scan(&n); err != nil {
			t.Fatalf("count A: %v", err)
		}
		if n == 0 {
			t.Errorf("expected >=1 message for tenant A, got 0")
		}

		var nB int
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE tenant_id = $1`, tenantB).Scan(&nB); err != nil {
			t.Fatalf("count B: %v", err)
		}
		if nB != 0 {
			t.Errorf("RLS leak: tenant A saw %d rows from tenant B (expected 0)", nB)
		}
	})

	t.Run("MezApp_WithWrongTenant_ReturnsZero", func(t *testing.T) {
		// RLS doesn't raise; it just returns 0 rows. (vs C4 which raises when
		// the GUC is *absent* — here GUC is set, so policy filters to 0 rows.)
		if _, err := appPool.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, false)", tenantA); err != nil {
			t.Fatalf("set tenant_id: %v", err)
		}
		var n int
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE tenant_id = $1`, tenantB).Scan(&n); err != nil {
			t.Fatalf("count B from A ctx: %v", err)
		}
		if n != 0 {
			t.Errorf("expected 0 (RLS filter), got %d", n)
		}
	})

	t.Run("MezPlatform_CanReadCrossTenant", func(t *testing.T) {
		// mez_platform has BYPASSRLS — sees both tenants without mez.tenant_id.
		var n int
		if err := platformPool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n); err != nil {
			t.Fatalf("platform count: %v", err)
		}
		if n < 2 {
			t.Errorf("expected >=2 (both tenants), got %d", n)
		}
	})

	t.Run("MezMigrate_Owner_StillFails_FROCE_RLS", func(t *testing.T) {
		// C3: FORCE ROW LEVEL SECURITY — even the table owner (mez_migrate)
		// is subject to RLS. The owner normally bypasses RLS by default;
		// FORCE is the C3 fix.
		var n int
		err := migratePool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n)
		if err == nil {
			t.Fatalf("expected FAILURE for owner without mez.tenant_id (FORCE RLS), got %d rows", n)
		}
		t.Logf("OK: mez_migrate without mez.tenant_id → %v", err)
	})

	// -------------------------------------------------------------------------
	// Phase 1 — admin tables (0002_admin.up.sql)
	// -------------------------------------------------------------------------

	t.Run("MezApp_CanSelectAdminUsers", func(t *testing.T) {
		// LoginService needs to look up users by email/id. The policy grants
		// SELECT to mez_app so the lookup works. No tenant_id is required
		// because admin_users is intentionally global (C5).
		var n int
		if err := appPool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n); err != nil {
			t.Fatalf("mez_app should be able to SELECT admin_users, got: %v", err)
		}
		t.Logf("OK: mez_app SELECT admin_users → %d rows", n)
	})

	t.Run("MezApp_CannotInsertAdminUsers", func(t *testing.T) {
		// The policy `admin_users_write` is USING(false) WITH CHECK(false) for
		// mez_app, so INSERT/UPDATE/DELETE must all fail. This is the C5
		// safety net: mez_app can never create an admin user, only mez_platform
		// (via RunAsPlatform) can.
		_, err := appPool.Exec(ctx,
			`INSERT INTO admin_users (email) VALUES ('attacker@example.com')`)
		if err == nil {
			t.Fatalf("mez_app should NOT be able to INSERT into admin_users")
		}
		t.Logf("OK: mez_app INSERT admin_users blocked → %v", err)
	})

	t.Run("MezApp_CannotUpdateAdminUsers", func(t *testing.T) {
		_, err := appPool.Exec(ctx, `UPDATE admin_users SET name = 'hacked'`)
		if err == nil {
			t.Fatalf("mez_app should NOT be able to UPDATE admin_users")
		}
		t.Logf("OK: mez_app UPDATE admin_users blocked → %v", err)
	})

	t.Run("MezApp_CannotDeleteAdminUsers", func(t *testing.T) {
		_, err := appPool.Exec(ctx, `DELETE FROM admin_users`)
		if err == nil {
			t.Fatalf("mez_app should NOT be able to DELETE from admin_users")
		}
		t.Logf("OK: mez_app DELETE admin_users blocked → %v", err)
	})

	t.Run("MezPlatform_CanWriteAdminUsers", func(t *testing.T) {
		// mez_platform has BYPASSRLS — usado pelo RunAsPlatform wrapper. Verifica
		// que o caminho do control plane está desbloqueado.
		var id string
		err := platformPool.QueryRow(ctx,
			`INSERT INTO admin_users (email, auth_kind, idp_subject, idp_issuer)
			 VALUES ('rls-test@example.com', 'oidc', 'rls-test-subject', 'rls-test-issuer')
			 RETURNING id`).Scan(&id)
		if err != nil {
			t.Fatalf("mez_platform should be able to INSERT admin_users, got: %v", err)
		}
		if _, err := platformPool.Exec(ctx, `DELETE FROM admin_users WHERE email = 'rls-test@example.com'`); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
		t.Logf("OK: mez_platform INSERT/DELETE admin_users → id=%s", id)
	})

	t.Run("MezApp_CanInsertAuditLog_ButNotUpdateOrDelete", func(t *testing.T) {
		// LoginService writes audit_log rows (login success/failure) using
		// mez_app. Revoke UPDATE, DELETE ensures append-only.
		_, err := appPool.Exec(ctx,
			`INSERT INTO admin_audit_log (actor_email, action) VALUES ('test@x.com', 'auth.login.failure')`)
		if err != nil {
			t.Fatalf("mez_app should INSERT admin_audit_log, got: %v", err)
		}
		// UPDATE blocked
		if _, err := appPool.Exec(ctx, `UPDATE admin_audit_log SET action = 'tampered'`); err == nil {
			t.Errorf("mez_app should NOT UPDATE admin_audit_log (REVOKE)")
		}
		// DELETE blocked
		if _, err := appPool.Exec(ctx, `DELETE FROM admin_audit_log`); err == nil {
			t.Errorf("mez_app should NOT DELETE admin_audit_log (REVOKE)")
		}
		// Cleanup via superuser
		_, _ = superPool.Exec(ctx, `DELETE FROM admin_audit_log WHERE actor_email = 'test@x.com'`)
		t.Logf("OK: mez_app can INSERT but not UPDATE/DELETE admin_audit_log")
	})
}

// ---- helpers -----------------------------------------------------------------

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	for _, mf := range []string{migrationFile, adminMigration} {
		candidates := []string{
			filepath.Join("..", "..", migrationsDir, mf),
			filepath.Join(migrationsDir, mf),
		}
		var rawSQL []byte
		var err error
		for _, p := range candidates {
			rawSQL, err = os.ReadFile(p)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("read migration %s (tried %v): %w", mf, candidates, err)
		}
		if _, err := pool.Exec(ctx, string(rawSQL)); err != nil {
			return fmt.Errorf("apply migration %s: %w", mf, err)
		}
	}
	return nil
}

func setRolePasswords(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		fmt.Sprintf(`ALTER ROLE mez_app      WITH LOGIN PASSWORD '%s'`, appPassword),
		fmt.Sprintf(`ALTER ROLE mez_migrate  WITH LOGIN PASSWORD '%s'`, migratePassword),
		fmt.Sprintf(`ALTER ROLE mez_platform WITH LOGIN PASSWORD '%s'`, platformPassword),
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

// seedTenants inserts two tenants and one message per tenant as the
// superuser. Returns the two tenant UUIDs.
func seedTenants(ctx context.Context, pool *pgxpool.Pool) (string, string, error) {
	var tenantA, tenantB string
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, slug) VALUES ('Tenant A', 'tenant-a') RETURNING id`).Scan(&tenantA); err != nil {
		return "", "", fmt.Errorf("insert tenant A: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, slug) VALUES ('Tenant B', 'tenant-b') RETURNING id`).Scan(&tenantB); err != nil {
		return "", "", fmt.Errorf("insert tenant B: %w", err)
	}

	insertMessage := func(tenantID string) error {
		var contactID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO contacts (tenant_id, channel, phone, name) VALUES ($1::uuid, 'waba', $1::text, 'C') RETURNING id`,
			tenantID,
		).Scan(&contactID); err != nil {
			return fmt.Errorf("insert contact: %w", err)
		}
		var convID string
		if err := pool.QueryRow(ctx,
			`INSERT INTO conversations (tenant_id, channel, contact_id, status) VALUES ($1::uuid, 'waba', $2::uuid, 'open') RETURNING id`,
			tenantID, contactID,
		).Scan(&convID); err != nil {
			return fmt.Errorf("insert conversation: %w", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (tenant_id, channel, conversation_id, contact_id, direction, type, status, body)
			 VALUES ($1::uuid, 'waba', $2::uuid, $3::uuid, 'inbound', 'text', 'received', 'hello')`,
			tenantID, convID, contactID,
		); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		return nil
	}
	if err := insertMessage(tenantA); err != nil {
		return "", "", err
	}
	if err := insertMessage(tenantB); err != nil {
		return "", "", err
	}
	return tenantA, tenantB, nil
}

func buildDSN(host, port, db, user, pass string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, db)
}

func mustPool(ctx context.Context, t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New(%q): %v", dsn, err)
	}
	if err := p.Ping(ctx); err != nil {
		t.Fatalf("ping %q: %v", dsn, err)
	}
	return p
}
