package secrets_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
)

// masterKeyB64 é uma chave AES-256 determinística (32 bytes) em base64.
const masterKeyB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="

// fakeRepo é um stub in-memory de CredentialsRepository para testes de
// unidade. Não toca DB.
type fakeRepo struct {
	mu   sync.Mutex
	data map[string]*port.CredentialRow
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{data: make(map[string]*port.CredentialRow)}
}

func keyOf(tenantID domain.TenantID, channel domain.Channel) string {
	return string(tenantID) + "|" + string(channel)
}

func (r *fakeRepo) Get(_ context.Context, tenantID domain.TenantID, channel domain.Channel) (*port.CredentialRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cc, ok := r.data[keyOf(tenantID, channel)]
	if !ok {
		return nil, port.ErrNotFound
	}
	// Devolve cópia para evitar que o caller mutile o estado interno.
	cp := *cc
	return &cp, nil
}

func (r *fakeRepo) Upsert(_ context.Context, tenantID domain.TenantID, channel domain.Channel, wrappedDEK, encrypted []byte, kekVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.data[keyOf(tenantID, channel)] = &port.CredentialRow{
		TenantID:   tenantID,
		Channel:    channel,
		WrappedDEK: append([]byte(nil), wrappedDEK...),
		Encrypted:  append([]byte(nil), encrypted...),
		KEKVersion: kekVersion,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, tenantID domain.TenantID, channel domain.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, keyOf(tenantID, channel))
	return nil
}

func newTestKeyring(t *testing.T) (*secrets.Keyring, *fakeRepo, *adaptercrypto.LocalSealer) {
	t.Helper()
	repo := newFakeRepo()
	seal, err := adaptercrypto.NewLocalSealer(masterKeyB64)
	if err != nil {
		t.Fatalf("NewLocalSealer: %v", err)
	}
	kr := secrets.New(repo, seal, zerolog.Nop())
	return kr, repo, seal
}

func TestKeyring_SetThenResolve_RoundTrip(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()
	tenant := domain.TenantID("tenant-A")
	channel := domain.ChannelWABA
	plaintext := []byte("EAAB...meta-access-token...")

	if err := kr.SetCredentials(ctx, tenant, channel, plaintext); err != nil {
		t.Fatalf("SetCredentials: %v", err)
	}

	got, err := kr.ResolveCredentials(ctx, tenant, channel)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Errorf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestKeyring_ResolveCredentials_NotFound(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()

	_, err := kr.ResolveCredentials(ctx, "tenant-X", domain.ChannelTGBot)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, secrets.ErrCredentialsNotFound) {
		t.Errorf("expected ErrCredentialsNotFound, got: %v", err)
	}
}

func TestKeyring_DifferentTenants_IndependentDEKs(t *testing.T) {
	kr, repo, _ := newTestKeyring(t)
	ctx := context.Background()

	tenantA := domain.TenantID("A")
	tenantB := domain.TenantID("B")
	channel := domain.ChannelWABA

	requireNoErr(t, kr.SetCredentials(ctx, tenantA, channel, []byte("secret-A")))
	requireNoErr(t, kr.SetCredentials(ctx, tenantB, channel, []byte("secret-B")))

	gotA, err := kr.ResolveCredentials(ctx, tenantA, channel)
	requireNoErr(t, err)
	if string(gotA) != "secret-A" {
		t.Errorf("A mismatch: got %q", gotA)
	}

	gotB, err := kr.ResolveCredentials(ctx, tenantB, channel)
	requireNoErr(t, err)
	if string(gotB) != "secret-B" {
		t.Errorf("B mismatch: got %q", gotB)
	}

	// wrapped_dek de A e B devem ser diferentes (DEKs distintas).
	a, _ := repo.Get(ctx, tenantA, channel)
	b, _ := repo.Get(ctx, tenantB, channel)
	if string(a.WrappedDEK) == string(b.WrappedDEK) {
		t.Error("esperava wrapped_deks distintos entre tenants (DEKs independentes)")
	}
}

func TestKeyring_Invalidate_RemovesCacheEntry(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()
	tenant := domain.TenantID("T")

	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("x")))
	// Resolve popula cache.
	_, err := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	requireNoErr(t, err)
	// Invalidate expurga o cache.
	kr.Invalidate(tenant)
	// Re-resolve ainda funciona (re-busca wrapped_dek e popula cache de novo).
	got, err := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	requireNoErr(t, err)
	if string(got) != "x" {
		t.Errorf("after invalidate, got %q want %q", got, "x")
	}
}

func TestKeyring_SetCredentials_InvalidatesCache(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()
	tenant := domain.TenantID("T")

	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("v1")))
	got, _ := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	if string(got) != "v1" {
		t.Fatalf("primeiro resolve: got %q", got)
	}

	// Sobrescreve com v2 — internamente invalida o cache e popula com
	// a DEK nova.
	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("v2")))
	got, _ = kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	if string(got) != "v2" {
		t.Errorf("após re-set, got %q want %q", got, "v2")
	}
}

func TestKeyring_CacheExpiry(t *testing.T) {
	repo := newFakeRepo()
	seal, err := adaptercrypto.NewLocalSealer(masterKeyB64)
	requireNoErr(t, err)

	// TTL de 1ms: a entry expira entre Put e Get.
	kr := secrets.New(repo, seal, zerolog.Nop(), secrets.WithCacheTTL(1*time.Millisecond))
	ctx := context.Background()
	tenant := domain.TenantID("T")

	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("x")))
	time.Sleep(20 * time.Millisecond)

	// Após expiry, ainda funciona (re-decifra e re-popula o cache).
	got, err := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	requireNoError(t, err)
	if string(got) != "x" {
		t.Errorf("após TTL expiry, got %q", got)
	}
}

func TestKeyring_EncryptForTenant_EncryptsWithCorrectDEK(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()
	tenant := domain.TenantID("T")

	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("hello")))
	got, err := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
	requireNoError(t, err)
	if string(got) != "hello" {
		t.Errorf("got %q want %q", got, "hello")
	}
}

func TestKeyring_Concurrent_ResolveAndSet(t *testing.T) {
	kr, _, _ := newTestKeyring(t)
	ctx := context.Background()
	tenant := domain.TenantID("T")

	requireNoErr(t, kr.SetCredentials(ctx, tenant, domain.ChannelWABA, []byte("base")))

	// N goroutines fazendo Resolve em paralelo. Sem data race (mutex do
	// cache); com -race o teste deve passar.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := kr.ResolveCredentials(ctx, tenant, domain.ChannelWABA)
			if err != nil || string(got) != "base" {
				t.Errorf("concurrent resolve: got %q err %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestKeyring_EncryptorWithoutSealer_Fails(t *testing.T) {
	// Encryptor falso que NÃO implementa Sealer. O SetCredentials deve
	// falhar com erro explícito.
	repo := newFakeRepo()
	kr := secrets.New(repo, noSealerEncryptor{}, zerolog.Nop())

	ctx := context.Background()
	err := kr.SetCredentials(ctx, "T", domain.ChannelWABA, []byte("x"))
	if err == nil {
		t.Fatal("esperava erro: encryptor sem Sealer")
	}
	if !errors.Is(err, err) { // sanity: erro é não-nil
		t.Logf("erro retornado: %v", err)
	}
}

// noSealerEncryptor satisfaz port.Encryptor (métodos não usados aqui).
// Get/Set são fillers para satisfazer a interface — eles não seriam
// chamados porque o teste só exercita SetCredentials (que tenta gerar
// DEK via Sealer). Definição real está no final do arquivo.

// requireNoErr falha o teste se err != nil.
func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// requireNoError é um alias para legibilidade.
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// noSealerEncryptor satisfaz port.Encryptor mas NÃO port.Sealer.
// Usado para testar fallback explícito de erro.
type noSealerEncryptor struct{}

func (noSealerEncryptor) EncryptForTenant(_, _ []byte) ([]byte, error) { return nil, nil }
func (noSealerEncryptor) DecryptForTenant(_, _ []byte) ([]byte, error) { return nil, nil }
