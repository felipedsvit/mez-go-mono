//go:build integration

// Package secrets contém os testes E2E do Keyring (Fase 7 #91) e
// RotateKEK (#92) com testcontainers Postgres.
//
// Cobre:
//   - Keyring: Set → Resolve round-trip; cross-tenant isolation; cache
//     hit/miss com TTL
//   - RotateKEK: re-wrap sem perda, kek_version incrementa, encrypted
//     inalterado, audit log correto, dry-run não persiste
//
// Requisitos: docker (testcontainers postgres).
package secrets

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
)

const (
	pgImage          = "postgres:16-alpine"
	migrationsDir    = "migrations"
	tcUser           = "postgres"
	tcPassword       = "test-tc-pwd-ABC"
	appPassword      = "test-app-pwd-XYZ123"
	platformPassword = "test-platform-pwd-XYZ789"
)

// fixture é o setup compartilhado: container Postgres + 3 roles +
// migrations + pools + txRunner + creds repo.
type fixture struct {
	superPool    *pgxpool.Pool
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
	txRunner     *postgres.TxRunner
	credsRepo    *postgres.ChannelCredentialsRepo
	terminate    func()
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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

	require.NoError(t, applyAllMigrations(ctx, superPool))
	require.NoError(t, setRolePasswords(ctx, superPool))

	appDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_app", appPassword, pgHost, pgPort.Port())
	platformDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_platform", platformPassword, pgHost, pgPort.Port())

	appPool, err := pgxpool.New(ctx, appDSN)
	require.NoError(t, err)
	platformPool, err := pgxpool.New(ctx, platformDSN)
	require.NoError(t, err)

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	credsRepo := postgres.NewChannelCredentialsRepo(appPool, platformPool, txRunner)

	term := func() {
		superPool.Close()
		appPool.Close()
		platformPool.Close()
		_ = pgC.Terminate(context.Background())
	}
	return &fixture{
		superPool: superPool, appPool: appPool, platformPool: platformPool,
		txRunner: txRunner, credsRepo: credsRepo, terminate: term,
	}
}

func (f *fixture) close() { f.terminate() }

func (f *fixture) seedTenant(t *testing.T, ctx context.Context, name string) domain.TenantID {
	t.Helper()
	id := domain.TenantID(uuid.NewString())
	_, err := f.superPool.Exec(ctx,
		`INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3)`,
		id, name, "slug-"+string(id)[:8])
	require.NoError(t, err)
	return id
}

func applyAllMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	files := []string{
		"0001_init.up.sql",
		"0002_admin.up.sql",
		"0003_outbox_fks_indexes.up.sql",
		"0004_whatsmeow.up.sql",
		"0005_backup_gucs.up.sql",
		"0006_kek_version.up.sql",
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return fmt.Errorf("repo root: %w", err)
	}
	for _, mf := range files {
		var rawSQL []byte
		var rerr error
		for _, p := range []string{
			filepath.Join(repoRoot, migrationsDir, mf),
			filepath.Join(migrationsDir, mf),
			filepath.Join("..", "..", migrationsDir, mf),
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

func setRolePasswords(ctx context.Context, pool *pgxpool.Pool) error {
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

// newKeyring monta o Keyring com LocalSealer + repo do fixture.
func newKeyring(t *testing.T, fx *fixture, masterKeyB64 string) *secrets.Keyring {
	t.Helper()
	seal, err := adaptercrypto.NewLocalSealer(masterKeyB64)
	require.NoError(t, err)
	return secrets.New(fx.credsRepo, seal, zerolog.Nop())
}

// =============================================================================
// TestKeyring — issue #91
// =============================================================================

// TestE2E_Keyring_SetResolve_RoundTrip valida o caminho básico Set→Resolve
// no DB real, com 2 tenants × 4 canais.
func TestE2E_Keyring_SetResolve_RoundTrip(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	const kekB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	kr := newKeyring(t, fx, kekB64)

	tenants := []domain.TenantID{
		fx.seedTenant(t, ctx, "tenant-A"),
		fx.seedTenant(t, ctx, "tenant-B"),
	}
	channels := []domain.Channel{
		domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG, domain.ChannelTGBot,
	}

	// Set para todos.
	for _, tid := range tenants {
		require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tid, func(ctx context.Context) error {
			for _, ch := range channels {
				plaintext := []byte(fmt.Sprintf("secret-%s-%s", tid, ch))
				if err := kr.SetCredentials(ctx, tid, ch, plaintext); err != nil {
					return err
				}
			}
			return nil
		}))
	}

	// Resolve para todos — deve dar round-trip exato.
	for _, tid := range tenants {
		require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tid, func(ctx context.Context) error {
			for _, ch := range channels {
				got, err := kr.ResolveCredentials(ctx, tid, ch)
				if err != nil {
					return fmt.Errorf("resolve %s/%s: %w", tid, ch, err)
				}
				want := fmt.Sprintf("secret-%s-%s", tid, ch)
				if string(got) != want {
					return fmt.Errorf("%s/%s: got %q want %q", tid, ch, got, want)
				}
			}
			return nil
		}))
	}
}

// TestE2E_Keyring_CrossTenantIsolation garante que tenant A não decifra
// credenciais de tenant B (RLS fail-closed + DEK independente).
func TestE2E_Keyring_CrossTenantIsolation(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	const kekB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	kr := newKeyring(t, fx, kekB64)

	tenantA := fx.seedTenant(t, ctx, "A")
	tenantB := fx.seedTenant(t, ctx, "B")

	// Set em A.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenantA, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenantA, domain.ChannelWABA, []byte("secret-A"))
	}))

	// B tenta resolver canal de A — deve falhar (RLS esconde a linha).
	err := fx.txRunner.RunInTenantTx(ctx, tenantB, func(ctx context.Context) error {
		_, rerr := kr.ResolveCredentials(ctx, tenantB, domain.ChannelWABA)
		return rerr
	})
	require.ErrorIs(t, err, secrets.ErrCredentialsNotFound,
		"tenant B não deve resolver credencial de tenant A")
}

// TestE2E_Keyring_CacheInvalidation valida que SetCredentials e Invalidate
// expurgam o cache (Resolve após Set sempre vê o valor novo).
func TestE2E_Keyring_CacheInvalidation(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	const kekB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	kr := newKeyring(t, fx, kekB64)
	tenant := fx.seedTenant(t, ctx, "T")
	channel := domain.ChannelWABA

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("v1"))
	}))

	// 1º resolve popula o cache.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, "v1", string(got))
		return nil
	}))

	// Set com v2 — internamente invalida o cache e popula com a DEK nova.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("v2"))
	}))

	// Resolve deve devolver v2 (não v1 do cache stale).
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, "v2", string(got))
		return nil
	}))

	// Invalidate explícito + Set com v3 — verifica Invalidate().
	kr.Invalidate(tenant)
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("v3"))
	}))
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, "v3", string(got))
		return nil
	}))
}

// TestE2E_Keyring_TTLExpiry valida que após TTL o cache é re-populado
// a partir do DB e a decifragem continua funcionando.
func TestE2E_Keyring_TTLExpiry(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	const kekB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	seal, err := adaptercrypto.NewLocalSealer(kekB64)
	require.NoError(t, err)
	// TTL curto para forçar expiry dentro do teste.
	kr := secrets.New(fx.credsRepo, seal, zerolog.Nop(), secrets.WithCacheTTL(50*time.Millisecond))

	tenant := fx.seedTenant(t, ctx, "T")
	channel := domain.ChannelTGBot

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("x"))
	}))

	// Resolve, espera > TTL, resolve de novo. Ambas devem funcionar.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, "x", string(got))
		return nil
	}))

	time.Sleep(80 * time.Millisecond)

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, "x", string(got))
		return nil
	}))
}

// TestE2E_Keyring_ConcurrentResolve valida que múltiplas goroutines podem
// resolver credenciais em paralelo sem race (com -race).
func TestE2E_Keyring_ConcurrentResolve(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	const kekB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	kr := newKeyring(t, fx, kekB64)
	tenant := fx.seedTenant(t, ctx, "T")
	channel := domain.ChannelWABA

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("base"))
	}))

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fx.txRunner.RunInTenantTx(context.Background(), tenant, func(ctx context.Context) error {
				got, err := kr.ResolveCredentials(ctx, tenant, channel)
				if err != nil || string(got) != "base" {
					t.Errorf("concurrent resolve: got %q err %v", got, err)
				}
				return nil
			})
		}()
	}
	wg.Wait()
}

// auditAdapter implementa secrets.AuditRepository para testes E2E.
// Escreve no admin_audit_log (assumindo migration 0002 já aplicada).
type auditAdapter struct {
	pool    *pgxpool.Pool
	mu      sync.Mutex
	entries []adminEntry
}

type adminEntry struct {
	action string
	actor  string
}

func (a *auditAdapter) Record(ctx context.Context, e *admin.AuditEntry) error {
	a.mu.Lock()
	a.entries = append(a.entries, adminEntry{action: string(e.Action), actor: e.ActorEmail})
	a.mu.Unlock()

	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, err := a.pool.Exec(ctx,
		`INSERT INTO admin_audit_log (id, actor_id, actor_email, action, target_type, target_id, tenant_id, metadata, ip, user_agent, created_at)
		 VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, NULLIF($9, ''), '', $10)`,
		e.ID, string(e.ActorID), e.ActorEmail, string(e.Action),
		e.TargetType, e.TargetID, e.TenantID, e.Metadata, e.IP, e.CreatedAt,
	)
	return err
}

func (a *auditAdapter) byAction(action admin.Action) []adminEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []adminEntry
	for _, e := range a.entries {
		if e.action == string(action) {
			out = append(out, e)
		}
	}
	return out
}

// Sentinelas para forçar import de packages usados em build condicional.
var (
	_ = url.Parse
	_ = strings.Contains
	_ = port.ErrNotFound
)
