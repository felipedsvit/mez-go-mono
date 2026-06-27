//go:build integration

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgImage     = "postgres:16-alpine"
	tcUser      = "postgres"
	tcPassword  = "test-tc-pwd"
	pgDB        = "mez"
	migratePwd  = "test-migrate-pwd-XYZ"
	appPwd      = "test-app-pwd-XYZ"
	platformPwd = "test-platform-pwd-XYZ"
)

// setupPGContainer sobe um postgres 16-alpine + aplica todas as migrations.
// Retorna um *pgxpool.Pool com superuser (para testes).
func setupPGContainer(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()

	pgC, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage(pgImage),
		tcpostgres.WithDatabase(pgDB),
		tcpostgres.WithUsername(tcUser),
		tcpostgres.WithPassword(tcPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Skipf("testcontainers: cannot start postgres: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = pgC.Terminate(stopCtx)
	})

	host, _ := pgC.Host(ctx)
	port, _ := pgC.MappedPort(ctx, "5432/tcp")
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		tcUser, tcPassword, host, port.Port(), pgDB)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect superuser: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := applyMigrations(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	// Configura as 3 roles que o mez-go-mono espera.
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER ROLE mez_app WITH LOGIN PASSWORD '%s'`, appPwd)); err != nil {
		t.Logf("mez_app role alter: %v (may already exist)", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER ROLE mez_migrate WITH LOGIN PASSWORD '%s'`, migratePwd)); err != nil {
		t.Logf("mez_migrate role alter: %v", err)
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf(`ALTER ROLE mez_platform WITH LOGIN PASSWORD '%s'`, platformPwd)); err != nil {
		t.Logf("mez_platform role alter: %v", err)
	}

	return pool
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrations := []string{
		"0001_init.up.sql",
		"0002_admin.up.sql",
		"0003_outbox_fks_indexes.up.sql",
		"0004_whatsmeow.up.sql",
		"0005_backup_gucs.up.sql",
		"0006_kek_version.up.sql",
	}
	for _, mf := range migrations {
		candidates := []string{
			filepath.Join("..", "..", "migrations", mf),
			filepath.Join("migrations", mf),
		}
		var raw []byte
		var err error
		for _, p := range candidates {
			raw, err = os.ReadFile(p)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", mf, err)
		}
		if _, err := pool.Exec(ctx, string(raw)); err != nil {
			return fmt.Errorf("apply %s: %w", mf, err)
		}
	}
	return nil
}

// seedTenantForTest insere um tenant via superuser e retorna o ID.
func seedTenantForTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (name, slug, active) VALUES ($1, $2, true) RETURNING id`,
		name, fmt.Sprintf("test-%s-%d", name, time.Now().UnixNano()),
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	return id
}

// seedConversationForTest insere uma conversa para o tenant.
func seedConversationForTest(ctx context.Context, t *testing.T, pool *pgxpool.Pool, tenantID, channel, contactID string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO conversations (tenant_id, channel, contact_id, status, external_id) VALUES ($1, $2, $3, 'open', $4) RETURNING id`,
		tenantID, channel, contactID, "ext-1",
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed conv: %v", err)
	}
	return id
}
