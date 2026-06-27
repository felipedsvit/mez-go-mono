//go:build integration

// Package backup contém os testes E2E do pipeline de backup/restore/reset
// (issue #88).
//
// Cobre:
//   - Roundtrip: export → drop DB → restore → match (idempotência)
//   - Reset: export → reset → DB vazio + S3 limpo
//   - C7: recusa backup com schema_version > current
//   - D16: reset sem confirmação "RESET" é recusado
//
// Requisitos: docker (testcontainers postgres + minio).
package backup

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	adminrepo "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres/admin"
	s3store "github.com/felipedsvit/mez-go-mono/internal/adapter/storage/s3"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
)

const (
	pgImage          = "postgres:16-alpine"
	migrationsDir    = "migrations"
	tcUser           = "postgres"
	tcPassword       = "test-tc-pwd-ABC"
	appPassword      = "test-app-pwd-XYZ123"
	platformPassword = "test-platform-pwd-XYZ789"
)

// TestBackup_RoundTrip valida: export → drop DB → restore → match (idempotente).
func TestBackup_RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	superPool, appPool, platformPool, _, store, term := setupInfra(t, ctx)
	defer term()

	tenantID := uuid.NewString()
	seedTenantData(t, ctx, superPool, tenantID)

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	jobs := ucbackup.NewJobStore(time.Hour)
	svc := ucbackup.New(ucbackup.Options{
		Logger: zerolog.Nop(), TxRunner: txRunner, Store: store,
		PGXPool: appPool, PlatformPool: platformPool,
		Jobs: jobs, Disconnector: ucbackup.NoopDisconnector{},
	})

	snap1 := snapshotTenant(t, ctx, superPool, tenantID)

	res, err := svc.Export(ctx, ucbackup.ExportRequest{
		TenantID: tenantID, Actor: actorTest(), IncludeMedia: true,
	})
	require.NoError(t, err)
	waitJobDone(t, jobs, res.JobID, 60*time.Second)

	dropAllTenantData(t, ctx, superPool, tenantID)
	require.Zero(t, countAllTables(t, ctx, superPool, tenantID))

	r1, err := svc.Restore(ctx, ucbackup.RestoreRequest{
		TenantID: tenantID, BackupID: res.BackupID, Actor: actorTest(),
	})
	require.NoError(t, err)
	waitJobDone(t, jobs, r1.JobID, 60*time.Second)

	after1 := snapshotTenant(t, ctx, superPool, tenantID)
	assertSnapshotsEqual(t, snap1, after1)

	r2, err := svc.Restore(ctx, ucbackup.RestoreRequest{
		TenantID: tenantID, BackupID: res.BackupID, Actor: actorTest(),
	})
	require.NoError(t, err)
	waitJobDone(t, jobs, r2.JobID, 60*time.Second)
	after2 := snapshotTenant(t, ctx, superPool, tenantID)
	assertSnapshotsEqual(t, snap1, after2)
}

// TestBackup_Reset valida: export → reset → DB vazio + S3 limpo.
func TestBackup_Reset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	superPool, appPool, platformPool, _, store, term := setupInfra(t, ctx)
	defer term()

	tenantID := uuid.NewString()
	seedTenantData(t, ctx, superPool, tenantID)

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	jobs := ucbackup.NewJobStore(time.Hour)
	svc := ucbackup.New(ucbackup.Options{
		Logger: zerolog.Nop(), TxRunner: txRunner, Store: store,
		PGXPool: appPool, PlatformPool: platformPool,
		Jobs: jobs, Disconnector: ucbackup.NoopDisconnector{},
	})

	_, err := store.Put(ctx,
		fmt.Sprintf("tenants/%s/media/x.png", tenantID),
		[]byte("fake media"), "image/png")
	require.NoError(t, err)

	res, err := svc.Export(ctx, ucbackup.ExportRequest{
		TenantID: tenantID, Actor: actorTest(), IncludeMedia: true,
	})
	require.NoError(t, err)
	waitJobDone(t, jobs, res.JobID, 60*time.Second)

	require.Greater(t, countAllTables(t, ctx, superPool, tenantID), 0)

	r, err := svc.Reset(ctx, ucbackup.ResetRequest{
		TenantID:      tenantID,
		Actor:         actorTest(),
		ConfirmText:   "RESET",
		AdminPassword: "qualquer",
	}, nil)
	require.NoError(t, err)
	waitJobDone(t, jobs, r.JobID, 30*time.Second)

	require.Zero(t, countAllTables(t, ctx, superPool, tenantID))

	count, err := s3Count(ctx, store, store.MediaBucket(), "tenants/"+tenantID+"/")
	require.NoError(t, err)
	require.Zero(t, count, "esperava S3 limpo, encontrou %d objetos", count)
}

// TestBackup_SchemaVersion_RefuseUpgrade valida C7.
func TestBackup_SchemaVersion_RefuseUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	superPool, appPool, platformPool, _, store, term := setupInfra(t, ctx)
	defer term()

	tenantID := uuid.NewString()
	seedTenantData(t, ctx, superPool, tenantID)

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	jobs := ucbackup.NewJobStore(time.Hour)
	svc := ucbackup.New(ucbackup.Options{
		Logger: zerolog.Nop(), TxRunner: txRunner, Store: store,
		PGXPool: appPool, PlatformPool: platformPool,
		Jobs: jobs, Disconnector: ucbackup.NoopDisconnector{},
	})

	res, err := svc.Export(ctx, ucbackup.ExportRequest{
		TenantID: tenantID, Actor: actorTest(), IncludeMedia: true,
	})
	require.NoError(t, err)
	waitJobDone(t, jobs, res.JobID, 60*time.Second)

	corruptManifestSchema(t, ctx, store, tenantID, res.BackupID, 99)
	_, err = svc.Restore(ctx, ucbackup.RestoreRequest{
		TenantID: tenantID, BackupID: res.BackupID, Actor: actorTest(),
	})
	require.ErrorIs(t, err, ucbackup.ErrSchemaDowngrade)
}

// TestBackup_ResetRequiresConfirmText valida D16.
func TestBackup_ResetRequiresConfirmText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, appPool, platformPool, _, _, term := setupInfra(t, ctx)
	defer term()

	txRunner := postgres.NewTxRunner(appPool, platformPool, zerolog.Nop())
	jobs := ucbackup.NewJobStore(time.Hour)
	svc := ucbackup.New(ucbackup.Options{
		Logger: zerolog.Nop(), TxRunner: txRunner,
		PGXPool: appPool, PlatformPool: platformPool,
		Jobs: jobs, Disconnector: ucbackup.NoopDisconnector{},
	})

	_, err := svc.Reset(ctx, ucbackup.ResetRequest{
		TenantID:      uuid.NewString(),
		Actor:         actorTest(),
		ConfirmText:   "reset", // minúscula — inválido
		AdminPassword: "x",
	}, nil)
	require.ErrorIs(t, err, ucbackup.ErrResetRequiresConfirmText)
}

// --- helpers --------------------------------------------------------------

func actorTest() cdomain.Actor {
	return cdomain.Actor{
		Email: "test-admin@example.com",
		IP:    "127.0.0.1",
	}
}

type tenantSnapshot struct {
	contacts       int
	conversations  int
	messages       int
	inboundEvents  int
	outboundEvents int
}

func snapshotTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) tenantSnapshot {
	t.Helper()
	var s tenantSnapshot
	queries := map[string]*int{
		"contacts":        &s.contacts,
		"conversations":   &s.conversations,
		"messages":        &s.messages,
		"inbound_events":  &s.inboundEvents,
		"outbound_events": &s.outboundEvents,
	}
	for table, dst := range queries {
		err := pool.QueryRow(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE tenant_id = $1", table), tenantID).Scan(dst)
		require.NoError(t, err)
	}
	return s
}

func assertSnapshotsEqual(t *testing.T, a, b tenantSnapshot) {
	t.Helper()
	require.Equal(t, a.contacts, b.contacts, "contacts diff")
	require.Equal(t, a.conversations, b.conversations, "conversations diff")
	require.Equal(t, a.messages, b.messages, "messages diff")
	require.Equal(t, a.inboundEvents, b.inboundEvents, "inbound_events diff")
	require.Equal(t, a.outboundEvents, b.outboundEvents, "outbound_events diff")
}

func seedTenantData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3)`, tenantID, "Test Tenant", "test-"+tenantID[:8])
	require.NoError(t, err)

	contactIDs := make([]string, 10)
	for i := 0; i < 10; i++ {
		cid := uuid.NewString()
		contactIDs[i] = cid
		_, err := pool.Exec(ctx,
			`INSERT INTO contacts (id, tenant_id, channel, phone, name) VALUES ($1, $2, $3, $4, $5)`,
			cid, tenantID, "whatsapp", fmt.Sprintf("+5511%d", 900000000+i), fmt.Sprintf("User %d", i))
		require.NoError(t, err)
	}

	for i := 0; i < 5; i++ {
		convID := uuid.NewString()
		_, err := pool.Exec(ctx,
			`INSERT INTO conversations (id, tenant_id, channel, contact_id, status) VALUES ($1, $2, $3, $4, 'open')`,
			convID, tenantID, "whatsapp", contactIDs[i])
		require.NoError(t, err)
		for j := 0; j < 20; j++ {
			_, err := pool.Exec(ctx,
				`INSERT INTO messages (id, tenant_id, channel, conversation_id, contact_id, direction, type, body) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
				uuid.NewString(), tenantID, "whatsapp", convID, contactIDs[i], "in", "text", fmt.Sprintf("msg %d", j))
			require.NoError(t, err)
		}
	}

	for i := 0; i < 3; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO inbound_events (id, tenant_id, channel, source, payload) VALUES ($1, $2, $3, $4, $5)`,
			uuid.NewString(), tenantID, "whatsapp", "webhook", `{"test":true}`)
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		_, err := pool.Exec(ctx,
			`INSERT INTO outbound_events (id, tenant_id, channel, target, payload) VALUES ($1, $2, $3, $4, $5)`,
			uuid.NewString(), tenantID, "whatsapp", `{"to":"x"}`, `{"text":"y"}`)
		require.NoError(t, err)
	}
}

func dropAllTenantData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) {
	t.Helper()
	for _, table := range []string{
		"messages", "conversations", "outbound_events", "inbound_events",
		"channel_credentials", "contacts",
	} {
		_, err := pool.Exec(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE tenant_id = $1", table), tenantID)
		require.NoError(t, err)
	}
}

func countAllTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) int {
	t.Helper()
	var total int
	for _, table := range []string{
		"contacts", "conversations", "messages",
		"inbound_events", "outbound_events",
	} {
		var c int
		err := pool.QueryRow(ctx,
			fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE tenant_id = $1", table), tenantID).Scan(&c)
		require.NoError(t, err)
		total += c
	}
	return total
}

func s3Count(ctx context.Context, store *s3store.Store, bucket, prefix string) (int, error) {
	ch := store.Client().ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	count := 0
	for range ch {
		count++
	}
	return count, nil
}

func waitJobDone(t *testing.T, jobs *ucbackup.JobStore, jobID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := jobs.Get(jobID)
		if err == nil {
			// Lock antes de ler State (concorrência com runExport/runRestore
			// que mutam Tables/ProgressPct na mesma struct).
			job.Lock().Lock()
			state := job.State
			errMsg := job.Error
			job.Lock().Unlock()
			if state == ucbackup.StateDone || state == ucbackup.StateFailed {
				require.Equal(t, ucbackup.StateDone, state, "job %s failed: %s", jobID, errMsg)
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("job %s não terminou em %s", jobID, timeout)
}

func corruptManifestSchema(t *testing.T, ctx context.Context, store *s3store.Store, tenantID, backupID string, version int) {
	t.Helper()
	manifestKey := fmt.Sprintf("tenants/%s/backups/%s/manifest.json", tenantID, backupID)
	rc, err := store.DownloadStream(ctx, store.BackupBucket(), manifestKey)
	require.NoError(t, err)
	data, err := readAll(rc)
	rc.Close()
	require.NoError(t, err)

	prefix := []byte(`"schema_version": `)
	idx := bytes.Index(data, prefix)
	require.GreaterOrEqual(t, idx, 0, "schema_version não encontrado em %s", string(data))
	start := idx + len(prefix)
	end := start
	for end < len(data) && (data[end] >= '0' && data[end] <= '9') {
		end++
	}
	require.Greater(t, end, start, "número não encontrado após schema_version")
	patched := append([]byte{}, data[:start]...)
	patched = append(patched, []byte(fmt.Sprintf("%d", version))...)
	patched = append(patched, data[end:]...)
	_, err = store.UploadBytes(ctx, store.BackupBucket(), manifestKey, patched, "application/json")
	require.NoError(t, err)
}

func readAll(r interface{ Read(p []byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}

// setupInfra sobe Postgres + MinIO, aplica migrations, e retorna pools + store.
func setupInfra(t *testing.T, ctx context.Context) (*pgxpool.Pool, *pgxpool.Pool, *pgxpool.Pool, *adminrepo.DB, *s3store.Store, func()) {
	t.Helper()
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

	mC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "minio/minio:latest",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			},
			Cmd:        []string{"server", "/data", "--address", ":9000"},
			WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		_ = pgC.Terminate(context.Background())
		t.Skipf("testcontainers minio: %v", err)
	}

	pgHost, _ := pgC.Host(ctx)
	pgPort, _ := pgC.MappedPort(ctx, "5432")
	mHost, _ := mC.Host(ctx)
	mPort, _ := mC.MappedPort(ctx, "9000")

	superDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		tcUser, tcPassword, pgHost, pgPort.Port())
	superPool, err := pgxpool.New(ctx, superDSN)
	require.NoError(t, err)

	if err := applyAllMigrationsForBackup(ctx, superPool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if err := setRolePasswordsForBackup(ctx, superPool); err != nil {
		t.Fatalf("set role passwords: %v", err)
	}

	appDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_app", appPassword, pgHost, pgPort.Port())
	platformDSN := fmt.Sprintf("postgres://%s:%s@%s:%s/mez?sslmode=disable",
		"mez_platform", platformPassword, pgHost, pgPort.Port())

	appPool, err := pgxpool.New(ctx, appDSN)
	require.NoError(t, err)
	platformPool, err := pgxpool.New(ctx, platformDSN)
	require.NoError(t, err)

	adminDB, err := adminrepo.NewDB(ctx, platformDSN)
	require.NoError(t, err)

	store, err := s3store.New(ctx, zerolog.Nop(), s3store.Config{
		Endpoint:     fmt.Sprintf("%s:%s", mHost, mPort.Port()),
		AccessKey:    "minioadmin",
		SecretKey:    "minioadmin",
		Bucket:       "test-media",
		BackupBucket: "test-backups",
		UseSSL:       false,
	})
	require.NoError(t, err)

	term := func() {
		superPool.Close()
		appPool.Close()
		platformPool.Close()
		adminDB.Close()
		_ = pgC.Terminate(context.Background())
		_ = mC.Terminate(context.Background())
	}
	return superPool, appPool, platformPool, adminDB, store, term
}

func applyAllMigrationsForBackup(ctx context.Context, pool *pgxpool.Pool) error {
	files := []string{"0001_init.up.sql", "0002_admin.up.sql", "0003_outbox_fks_indexes.up.sql", "0004_whatsmeow.up.sql", "0005_backup_gucs.up.sql"}
	for _, mf := range files {
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
			return fmt.Errorf("read migration %s: %w", mf, err)
		}
		if _, err := pool.Exec(ctx, string(rawSQL)); err != nil {
			return fmt.Errorf("apply %s: %w", mf, err)
		}
	}
	// Cria a tabela schema_migrations que o golang-migrate usa (a nossa
	// application manual de migrations não cria). Usada pelo restore
	// para comparar SchemaVersion.
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			dirty   BOOLEAN NOT NULL DEFAULT FALSE
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// Marca os migrations aplicados.
	for i, mf := range files {
		version := i + 1
		_, _ = pool.Exec(ctx,
			`INSERT INTO schema_migrations (version, dirty) VALUES ($1, false) ON CONFLICT (version) DO NOTHING`,
			version)
		_ = mf
	}
	return nil
}

func setRolePasswordsForBackup(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		fmt.Sprintf(`ALTER ROLE mez_app      WITH LOGIN PASSWORD '%s'`, appPassword),
		fmt.Sprintf(`ALTER ROLE mez_migrate  WITH LOGIN PASSWORD '%s'`, "test-migrate"),
		fmt.Sprintf(`ALTER ROLE mez_platform WITH LOGIN PASSWORD '%s'`, platformPassword),
		// Garante permissões em schema_migrations (necessário para mez_app
		// no fallback — produção usa platformPool, mas test deve funcionar
		// independente).
		`GRANT SELECT ON schema_migrations TO mez_app`,
		`GRANT SELECT ON schema_migrations TO mez_platform`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

var _ = url.Parse
var _ = strings.Contains
