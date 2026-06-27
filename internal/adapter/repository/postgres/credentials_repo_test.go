//go:build integration

// Package postgres — credentials_repo_test.go: testes E2E do
// ChannelCredentialsRepo (#90) com testcontainers Postgres.
//
// Cobre:
//   - Get/Upsert/Delete happy path dentro de RunInTenantTx
//   - RLS fail-closed: tenant A não lê tenant B
//   - ForEachTenant itera todos os tenants via RunAsPlatform
//   - UpdateWrappedDEK muda wrapped_dek e kek_version, mantém encrypted
//
// Requisitos: docker (testcontainers postgres).
package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

const (
	pgImage          = "postgres:16-alpine"
	migrationsDir    = "migrations"
	tcUser           = "postgres"
	tcPassword       = "test-tc-pwd-ABC"
	appPassword      = "test-app-pwd-XYZ123"
	platformPassword = "test-platform-pwd-XYZ789"
)

// credFixture é o setup compartilhado: container Postgres + 3 roles +
// migrations + pools (super, app, platform) + txRunner + repo.
type credFixture struct {
	superPool      *pgxpool.Pool
	appPool        *pgxpool.Pool
	platformPool   *pgxpool.Pool
	txRunner       *postgres.TxRunner
	repo           *postgres.ChannelCredentialsRepo
	tenantA        domain.TenantID
	tenantB        domain.TenantID
	terminate      func()
}

func setupCredFixture(t *testing.T) *credFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        pgImage,
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_USER":     tcUser,
				"POSTGRES_PASSWORD": tcPassword,
				"POSTGRES_DB":       "mez",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("testcontainers postgres: %v", err)
	}

	pgHost, _ := pgC.Host(ctx)
	pgPort, _ := pgC.MappedPort(ctx, "5432")

	superDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		tcUser, tcPassword, pgHost, pgPort.Port())
	superPool, err := pgxpool.New(ctx, superDSN)
	require.NoError(t, err)

	require.NoError(t, applyMigrationsForCred(ctx, superPool))
	require.NoError(t, setRolePasswordsForCred(ctx, superPool))

	appDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_app", appPassword, pgHost, pgPort.Port())
	platformDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_platform", platformPassword, pgHost, pgPort.Port())

	appPool, err := pgxpool.New(ctx, appDSN)
	require.NoError(t, err)
	platformPool, err := pgxpool.New(ctx, platformDSN)
	require.NoError(t, err)

	tenantA := domain.TenantID(uuid.NewString())
	tenantB := domain.TenantID(uuid.NewString())
	// tenants pais — necessário por causa do FK em channel_credentials.
	// Inserimos via superPool (BYPASSRLS no postgres role).
	_, err = superPool.Exec(ctx, `INSERT INTO tenants (id, name, slug) VALUES ($1, 'A', 'a'), ($2, 'B', 'b')`, tenantA, tenantB)
	require.NoError(t, err)

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	repo := postgres.NewChannelCredentialsRepo(appPool, platformPool, txRunner)

	term := func() {
		superPool.Close()
		appPool.Close()
		platformPool.Close()
		_ = pgC.Terminate(context.Background())
	}
	return &credFixture{
		superPool: superPool, appPool: appPool, platformPool: platformPool,
		txRunner: txRunner, repo: repo,
		tenantA: tenantA, tenantB: tenantB,
		terminate: term,
	}
}

func (f *credFixture) close() { f.terminate() }

func applyMigrationsForCred(ctx context.Context, pool *pgxpool.Pool) error {
	files := []string{
		"0001_init.up.sql",
		"0002_admin.up.sql",
		"0003_outbox_fks_indexes.up.sql",
		"0004_whatsmeow.up.sql",
		"0005_backup_gucs.up.sql",
		"0006_kek_version.up.sql",
	}
	// Caminho absoluto do repo: necessário pq `go test` muda o cwd.
	// O cwd durante o teste é internal/adapter/repository/postgres/;
	// precisamos de 4 ".." para chegar ao repo root onde está migrations/.
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	for _, mf := range files {
		var rawSQL []byte
		var rerr error
		for _, p := range []string{
			filepath.Join(repoRoot, migrationsDir, mf),
			filepath.Join(migrationsDir, mf),
			filepath.Join("..", "..", "..", "..", migrationsDir, mf),
		} {
			rawSQL, rerr = os.ReadFile(p)
			if rerr == nil {
				break
			}
		}
		if rerr != nil {
			return fmt.Errorf("read %s: %w", mf, rerr)
		}
		if _, err := pool.Exec(ctx, string(rawSQL)); err != nil {
			return fmt.Errorf("apply %s: %w", mf, err)
		}
	}
	return nil
}

func setRolePasswordsForCred(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		fmt.Sprintf(`ALTER ROLE mez_app      WITH LOGIN PASSWORD '%s'`, appPassword),
		fmt.Sprintf(`ALTER ROLE mez_migrate  WITH LOGIN PASSWORD '%s'`, "test-migrate"),
		fmt.Sprintf(`ALTER ROLE mez_platform WITH LOGIN PASSWORD '%s'`, platformPassword),
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// Tests
// =============================================================================

func TestChannelCredentials_UpsertGet_RoundTrip(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	wrapped := []byte("wrapped-dek-bytes")
	encrypted := []byte("encrypted-credential-bytes")
	channel := domain.ChannelWABA

	err := fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		return fx.repo.Upsert(ctx, fx.tenantA, channel, wrapped, encrypted, 1)
	})
	require.NoError(t, err)

	var got *port.CredentialRow
	err = fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		var ferr error
		got, ferr = fx.repo.Get(ctx, fx.tenantA, channel)
		return ferr
	})
	require.NoError(t, err)
	require.Equal(t, fx.tenantA, got.TenantID)
	require.Equal(t, channel, got.Channel)
	require.Equal(t, wrapped, got.WrappedDEK)
	require.Equal(t, encrypted, got.Encrypted)
	require.Equal(t, 1, got.KEKVersion)
}

func TestChannelCredentials_Get_NotFound(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	err := fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		_, err := fx.repo.Get(ctx, fx.tenantA, domain.ChannelIG)
		return err
	})
	require.ErrorIs(t, err, port.ErrNotFound)
}

func TestChannelCredentials_RLS_TenantIsolation(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	// tenant A grava uma credencial.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		return fx.repo.Upsert(ctx, fx.tenantA, domain.ChannelWABA, []byte("w-a"), []byte("e-a"), 1)
	}))

	// tenant B TENTA ler — RLS fail-closed esconde.
	err := fx.txRunner.RunInTenantTx(ctx, fx.tenantB, func(ctx context.Context) error {
		_, err := fx.repo.Get(ctx, fx.tenantB, domain.ChannelWABA)
		return err
	})
	require.ErrorIs(t, err, port.ErrNotFound, "tenant B não deve ver credencial de tenant A")
}

func TestChannelCredentials_Requires_RunInTenantTx(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	// Sem tx: deve falhar com mensagem explícita.
	_, err := fx.repo.Get(ctx, fx.tenantA, domain.ChannelWABA)
	require.ErrorContains(t, err, "RunInTenantTx")

	err = fx.repo.Upsert(ctx, fx.tenantA, domain.ChannelWABA, []byte("w"), []byte("e"), 1)
	require.ErrorContains(t, err, "RunInTenantTx")

	err = fx.repo.Delete(ctx, fx.tenantA, domain.ChannelWABA)
	require.ErrorContains(t, err, "RunInTenantTx")
}

func TestChannelCredentials_ForEachTenant(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	// 2 tenants × 3 canais = 6 credenciais.
	for _, tid := range []domain.TenantID{fx.tenantA, fx.tenantB} {
		require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tid, func(ctx context.Context) error {
			for _, ch := range []domain.Channel{domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG} {
				if err := fx.repo.Upsert(ctx, tid, ch, []byte("w"), []byte("e"), 1); err != nil {
					return err
				}
			}
			return nil
		}))
	}

	seen := 0
	err := fx.repo.ForEachTenant(ctx, "system:test", func(ctx context.Context, row port.CredentialRow) error {
		seen++
		require.NotEmpty(t, row.WrappedDEK)
		require.NotEmpty(t, row.Encrypted)
		require.Equal(t, 1, row.KEKVersion)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 6, seen)
}

func TestChannelCredentials_UpdateWrappedDEK_PreservesEncrypted(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	wrapped := []byte("old-wrapped-dek")
	encrypted := []byte("encrypted-credential-stable")
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		return fx.repo.Upsert(ctx, fx.tenantA, domain.ChannelWABA, wrapped, encrypted, 1)
	}))

	newWrapped := []byte("new-wrapped-dek-after-rotate")
	err := fx.repo.UpdateWrappedDEK(ctx, fx.tenantA, domain.ChannelWABA, newWrapped, 2, nil)
	require.NoError(t, err)

	var got *port.CredentialRow
	err = fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		var ferr error
		got, ferr = fx.repo.Get(ctx, fx.tenantA, domain.ChannelWABA)
		return ferr
	})
	require.NoError(t, err)
	require.Equal(t, newWrapped, got.WrappedDEK)
	require.Equal(t, encrypted, got.Encrypted, "encrypted deve permanecer inalterado (DEK não muda)")
	require.Equal(t, 2, got.KEKVersion)
	require.Nil(t, got.RotationWindowUntil)
}

func TestChannelCredentials_UpdateWrappedDEK_WithWindow(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		return fx.repo.Upsert(ctx, fx.tenantA, domain.ChannelTGBot, []byte("w"), []byte("e"), 1)
	}))

	window := time.Now().Add(24 * time.Hour).UTC()
	err := fx.repo.UpdateWrappedDEK(ctx, fx.tenantA, domain.ChannelTGBot, []byte("w2"), 2, &window)
	require.NoError(t, err)

	var got *port.CredentialRow
	err = fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		var ferr error
		got, ferr = fx.repo.Get(ctx, fx.tenantA, domain.ChannelTGBot)
		return ferr
	})
	require.NoError(t, err)
	require.NotNil(t, got.RotationWindowUntil)
	require.WithinDuration(t, window, *got.RotationWindowUntil, time.Second)
}

func TestChannelCredentials_Delete_Idempotent(t *testing.T) {
	fx := setupCredFixture(t)
	defer fx.close()
	ctx := context.Background()

	// Delete sem linha existente: não retorna erro.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		return fx.repo.Delete(ctx, fx.tenantA, domain.ChannelWABA)
	}))

	// Insere e deleta.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		if err := fx.repo.Upsert(ctx, fx.tenantA, domain.ChannelWABA, []byte("w"), []byte("e"), 1); err != nil {
			return err
		}
		return fx.repo.Delete(ctx, fx.tenantA, domain.ChannelWABA)
	}))

	// Get após delete: not found.
	err := fx.txRunner.RunInTenantTx(ctx, fx.tenantA, func(ctx context.Context) error {
		_, err := fx.repo.Get(ctx, fx.tenantA, domain.ChannelWABA)
		return err
	})
	require.ErrorIs(t, err, port.ErrNotFound)
}

// Sanidade: garante que errors.Is continua funcionando com o wrapping de
// ErrNotFound (evita regressão em mudanças de wrapping).
func TestChannelCredentials_NotFound_ErrorsIs(t *testing.T) {
	wrapped := fmt.Errorf("get: %w", port.ErrNotFound)
	require.True(t, errors.Is(wrapped, port.ErrNotFound))
}

var _ = url.Parse
var _ = strings.Contains
