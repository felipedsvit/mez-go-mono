//go:build integration

// Testes de integração do adapter S3 contra MinIO em container.
// Requer: TESTCONTAINERS_RYUK_DISABLED=true (CI padrão) e Docker rodando.
//
// Cobertura:
//   - New() cria os buckets
//   - Put/Get/Get-error roundtrip
//   - UploadStream + DownloadStream para objeto grande
//   - DeletePrefix remove recursivamente

package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	minioImage    = "minio/minio:latest"
	minioUser     = "minioadmin"
	minioPassword = "minioadmin"
)

// startMinIO sobe um container MinIO e devolve (endpoint, terminate).
// Endpoint é host:port sem scheme (ex.: "localhost:32789").
func startMinIO(t *testing.T, ctx context.Context) (string, func()) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        minioImage,
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     minioUser,
			"MINIO_ROOT_PASSWORD": minioPassword,
		},
		Cmd:        []string{"server", "/data", "--address", ":9000"},
		WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000").WithStartupTimeout(60 * time.Second),
	}
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Skipf("testcontainers: cannot start minio: %v", err)
	}
	host, err := c.Host(ctx)
	require.NoError(t, err)
	port, err := c.MappedPort(ctx, "9000")
	require.NoError(t, err)
	terminate := func() { _ = c.Terminate(context.Background()) }
	return host + ":" + port.Port(), terminate
}

func newTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	addr, terminate := startMinIO(t, ctx)

	store, err := New(ctx, zerolog.Nop(), Config{
		Endpoint:     addr,
		AccessKey:    minioUser,
		SecretKey:    minioPassword,
		Bucket:       "test-media",
		BackupBucket: "test-backups",
		UseSSL:       false,
	})
	require.NoError(t, err)
	return store, terminate
}

func TestStore_PutGetDelete(t *testing.T) {
	store, terminate := newTestStore(t)
	defer terminate()
	ctx := context.Background()

	url, err := store.Put(ctx, "tenants/abc/file.txt", []byte("hello world"), "text/plain")
	require.NoError(t, err)
	require.Contains(t, url, "tenants/abc/file.txt")

	data, ct, err := store.Get(ctx, "tenants/abc/file.txt")
	require.NoError(t, err)
	require.Equal(t, "text/plain", ct)
	require.Equal(t, "hello world", string(data))

	require.NoError(t, store.Delete(ctx, "tenants/abc/file.txt"))

	_, _, err = store.Get(ctx, "tenants/abc/file.txt")
	require.Error(t, err)
}

func TestStore_UploadStream_Roundtrip(t *testing.T) {
	store, terminate := newTestStore(t)
	defer terminate()
	ctx := context.Background()

	// 12 MiB (>2 parts para forçar multipart)
	payload := make([]byte, 12*1024*1024)
	_, _ = rand.Read(payload)

	etag, err := store.UploadStream(ctx, store.MediaBucket(), "tenants/abc/big.bin", bytes.NewReader(payload), "application/octet-stream")
	require.NoError(t, err)
	require.NotEmpty(t, etag)

	rc, err := store.DownloadStream(ctx, store.MediaBucket(), "tenants/abc/big.bin")
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestStore_DeletePrefix_Recursive(t *testing.T) {
	store, terminate := newTestStore(t)
	defer terminate()
	ctx := context.Background()

	// 5 objetos em "tenants/abc/" + 2 em "tenants/def/"
	for i := 0; i < 5; i++ {
		_, err := store.Put(ctx, "tenants/abc/f"+string(rune('0'+i)), []byte("x"), "text/plain")
		require.NoError(t, err)
	}
	_, err := store.Put(ctx, "tenants/def/g", []byte("y"), "text/plain")
	require.NoError(t, err)

	removed, err := store.DeletePrefix(ctx, store.MediaBucket(), "tenants/abc/")
	require.NoError(t, err)
	require.Equal(t, 5, removed)

	// def/ permanece intacto
	_, _, err = store.Get(ctx, "tenants/def/g")
	require.NoError(t, err)
}
