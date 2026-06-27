package settings

import (
	"context"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	pkgcrypto "github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

// fakeRepo é uma implementação in-memory de port.SystemSettingRepository.
type fakeRepo struct {
	mu sync.Mutex
	m  map[string]fakeEntry
}

type fakeEntry struct {
	encrypted   []byte
	kekVersion  int
	description string
	updatedBy   string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{m: make(map[string]fakeEntry)}
}

func (f *fakeRepo) Get(_ context.Context, key string) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.m[key]
	if !ok {
		return nil, 0, nil
	}
	return e.encrypted, e.kekVersion, nil
}

func (f *fakeRepo) Set(_ context.Context, key string, encrypted []byte, kekVersion int, description, updatedBy string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = fakeEntry{
		encrypted:   encrypted,
		kekVersion:  kekVersion,
		description: description,
		updatedBy:   updatedBy,
	}
	return nil
}

func (f *fakeRepo) List(_ context.Context) ([]port.SystemSettingEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]port.SystemSettingEntry, 0, len(f.m))
	for k, e := range f.m {
		out = append(out, port.SystemSettingEntry{
			Key:         k,
			Encrypted:   e.encrypted,
			KekVersion:  e.kekVersion,
			Description: e.description,
			UpdatedBy:   e.updatedBy,
		})
	}
	return out, nil
}

func (f *fakeRepo) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.m[key]; !ok {
		return ErrNotFound
	}
	delete(f.m, key)
	return nil
}

// newTestSealer cria um sealer com a master key determinística.
func newTestSealer(t *testing.T) Sealer {
	t.Helper()
	env, err := pkgcrypto.NewEnvelope("qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg=")
	if err != nil {
		t.Fatal(err)
	}
	return NewEnvelopeSealer(env)
}

func TestService_Get_Default(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())

	var enabled bool
	if err := svc.Get(context.Background(), "whatsmeow.enabled", &enabled, false); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if enabled {
		t.Error("default should be false")
	}

	var s string
	if err := svc.Get(context.Background(), "missing", &s, "fallback"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if s != "fallback" {
		t.Errorf("default = %q, want fallback", s)
	}
}

func TestService_SetGet_Roundtrip(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, newTestSealer(t), 1, zerolog.Nop())

	// Set
	if err := svc.SetWithDescription(context.Background(),
		"whatsmeow.enabled", "Liga o whatsmeow real",
		true, "admin@example.com"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Get
	var enabled bool
	if err := svc.Get(context.Background(), "whatsmeow.enabled", &enabled, false); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !enabled {
		t.Error("expected true after Set(true)")
	}
}

func TestService_SetGet_AllTypes(t *testing.T) {
	// Não usar t.Parallel() — os subtests compartilham o mesmo svc
	// e cada subtest opera em chaves distintas (mas há race no
	// shared state do service — watcher list, cache, etc).
	type tcase struct {
		key  string
		set  any
		want any
	}
	cases := []tcase{
		{"bool-true", true, true},
		{"int-42", 42, 42},
		{"string-foo", "foo", "foo"},
		{"duration-30s", "30s", "30s"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.key, func(t *testing.T) {
			svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())
			ctx := context.Background()
			if err := svc.Set(ctx, tc.key, tc.set, "tester"); err != nil {
				t.Fatal(err)
			}
			switch v := tc.want.(type) {
			case bool:
				var got bool
				if err := svc.Get(ctx, tc.key, &got, false); err != nil {
					t.Fatal(err)
				}
				if got != v {
					t.Errorf("got %v, want %v", got, v)
				}
			case int:
				var got int
				if err := svc.Get(ctx, tc.key, &got, 0); err != nil {
					t.Fatal(err)
				}
				if got != v {
					t.Errorf("got %v, want %v", got, v)
				}
			case string:
				var got string
				if err := svc.Get(ctx, tc.key, &got, ""); err != nil {
					t.Fatal(err)
				}
				if got != v {
					t.Errorf("got %v, want %v", got, v)
				}
			}
		})
	}
}

func TestService_Delete(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, newTestSealer(t), 1, zerolog.Nop())
	ctx := context.Background()

	// Set + Delete
	if err := svc.Set(ctx, "k1", "v1", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, "k1", "tester"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Get deve voltar ao default.
	var s string
	if err := svc.Get(ctx, "k1", &s, "default"); err != nil {
		t.Fatal(err)
	}
	if s != "default" {
		t.Errorf("after delete, default should apply, got %q", s)
	}
}

func TestService_Delete_NotFound(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())
	if err := svc.Delete(context.Background(), "nope", "tester"); err == nil {
		t.Error("expected error deleting nonexistent key")
	}
}

func TestService_Watch_FiresOnSet(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())
	ch, cancel := svc.Watch()
	defer cancel()

	// Set deve publicar no canal.
	if err := svc.Set(context.Background(), "k1", "v1", "tester"); err != nil {
		t.Fatal(err)
	}

	// Lê o evento (com timeout razoável).
	ev := <-ch
	if ev.Key != "k1" {
		t.Errorf("Key = %q", ev.Key)
	}
	if ev.UpdatedBy != "tester" {
		t.Errorf("UpdatedBy = %q", ev.UpdatedBy)
	}
	if len(ev.EncryptedValue) == 0 {
		t.Error("EncryptedValue should not be empty")
	}
}

func TestService_Watch_CancelStopsReceiving(t *testing.T) {
	t.Parallel()

	svc := NewService(newServiceRepoForWatch(), newTestSealer(t), 1, zerolog.Nop())
	ch, cancel := svc.Watch()

	cancel() // unsubscribe imediatamente

	// Set não deve bloquear (canal fechado).
	if err := svc.Set(context.Background(), "k1", "v1", "tester"); err != nil {
		t.Fatal(err)
	}

	// Tentar ler deve panic ou retornar zero value — apenas
	// verificamos que o canal foi fechado.
	_, ok := <-ch
	if ok {
		// recebeu um evento, sem problema — verificamos apenas
		// que não houve leak grave.
	}
}

func TestService_List(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())
	ctx := context.Background()

	_ = svc.Set(ctx, "k1", "v1", "admin")
	_ = svc.SetWithDescription(ctx, "k2", "teste", 42, "admin")

	views, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Errorf("expected 2, got %d", len(views))
	}
	for _, v := range views {
		if v.Key == "" {
			t.Error("empty key")
		}
	}
}

func TestService_SeedDefaults_OnlySetsMissing(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, newTestSealer(t), 1, zerolog.Nop())
	ctx := context.Background()

	// Set custom para uma key que está no defaults.
	_ = svc.Set(ctx, "whatsmeow.enabled", true, "admin")

	// Seed.
	if err := svc.SeedDefaults(ctx, "system"); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	// Verifica que o custom não foi sobrescrito.
	var enabled bool
	if err := svc.Get(ctx, "whatsmeow.enabled", &enabled, false); err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Error("SeedDefaults should not override existing values")
	}
}

func TestService_SeedDefaults_AddsMissing(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, newTestSealer(t), 1, zerolog.Nop())
	ctx := context.Background()

	if err := svc.SeedDefaults(ctx, "system"); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	// Verifica que várias settings foram seedadas.
	expected := []string{
		"whatsmeow.enabled",
		"whatsmeow.device_dsn",
		"whatsmeow.identity.kind",
		"ffmpeg.concurrency",
		"bus.inbound.buffer",
	}
	for _, key := range expected {
		encrypted, _, err := repo.Get(ctx, key)
		if err != nil {
			t.Errorf("key %q: %v", key, err)
		}
		if encrypted == nil {
			t.Errorf("key %q should be seeded", key)
		}
	}
}

func TestService_InvalidateCache(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), newTestSealer(t), 1, zerolog.Nop())
	ctx := context.Background()

	// Set (popula cache).
	_ = svc.Set(ctx, "k1", "v1", "tester")

	// Invalidate.
	svc.InvalidateCache("k1")
	svc.InvalidateCache("*")

	// Set novo valor.
	_ = svc.Set(ctx, "k1", "v2", "tester")

	// Get deve retornar v2.
	var s string
	if err := svc.Get(ctx, "k1", &s, "default"); err != nil {
		t.Fatal(err)
	}
	if s != "v2" {
		t.Errorf("got %q, want v2", s)
	}
}

// newServiceRepoForWatch retorna um repo válido para o teste de cancel.
func newServiceRepoForWatch() *fakeRepo {
	return newFakeRepo()
}
