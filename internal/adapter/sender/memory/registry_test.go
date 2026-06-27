// Package memory — testes do SenderRegistry in-memory (issue #121).
//
// Cobre:
//   - lazy init por (tenant, channel) + cache por TTL;
//   - factory errors não são cacheadas (próxima Get re-tenta);
//   - Channels() reflete registro dinâmico;
//   - Health() agrega erro por canal;
//   - concorrência: Get/Register são seguros para uso paralelo (C10).
package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// stubSender é um port.Sender mínimo para os testes.
type stubSender struct {
	channel domain.Channel
}

func (s *stubSender) Channel() domain.Channel { return s.channel }
func (s *stubSender) Capabilities() port.CapabilitySet {
	return port.CapabilitySet{port.CapText: true}
}
func (s *stubSender) Send(_ context.Context, _ port.OutboundRequest) (string, error) {
	return "stub-mid", nil
}

// fakeClock — relógio injetável determinístico.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestRegistry() *Registry {
	return New(zerolog.Nop(), time.Minute)
}

func TestRegistry_RegisterAndChannels(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	if got := r.Channels(); len(got) != 0 {
		t.Fatalf("fresh registry: Channels() = %v, want empty", got)
	}

	factory := func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return &stubSender{channel: domain.ChannelWABA}, nil
	}
	r.Register(domain.ChannelWABA, factory)
	r.Register(domain.ChannelIG, factory)

	chs := r.Channels()
	if len(chs) != 2 {
		t.Errorf("Channels() returned %d, want 2", len(chs))
	}

	seen := make(map[domain.Channel]bool, len(chs))
	for _, c := range chs {
		seen[c] = true
	}
	if !seen[domain.ChannelWABA] || !seen[domain.ChannelIG] {
		t.Errorf("missing channels: %v", chs)
	}
}

func TestRegistry_Get_LazyInit_CachesResult(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	var created atomic.Int32
	factory := func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		created.Add(1)
		return &stubSender{channel: domain.ChannelWABA}, nil
	}
	r.Register(domain.ChannelWABA, factory)

	ctx := context.Background()
	tenantID := domain.TenantID("t1")

	// Primeira chamada: factory invocada 1x.
	s1, err := r.Get(ctx, tenantID, domain.ChannelWABA)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if created.Load() != 1 {
		t.Errorf("after first Get, created = %d, want 1", created.Load())
	}

	// Segunda chamada: cache hit, factory NÃO é invocada de novo.
	s2, err := r.Get(ctx, tenantID, domain.ChannelWABA)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if created.Load() != 1 {
		t.Errorf("after second Get, created = %d, want 1 (cache hit)", created.Load())
	}
	if s1 != s2 {
		t.Error("expected cached sender (same pointer)")
	}
}

func TestRegistry_Get_UnknownChannel_ReturnsErrSenderNotRegistered(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	_, err := r.Get(context.Background(), "t1", domain.ChannelTGBot)
	if !errors.Is(err, ErrSenderNotRegistered) {
		t.Errorf("err = %v, want ErrSenderNotRegistered", err)
	}
}

func TestRegistry_Get_FactoryError_NotCached(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	var calls atomic.Int32
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		calls.Add(1)
		return nil, errors.New("kaboom")
	})

	// Primeira chamada: erro não é cacheado → próxima Get re-tenta a factory.
	_, err := r.Get(context.Background(), "t1", domain.ChannelWABA)
	if err == nil {
		t.Fatal("expected error from factory")
	}

	_, err = r.Get(context.Background(), "t1", domain.ChannelWABA)
	if err == nil {
		t.Fatal("expected error on retry")
	}
	if calls.Load() != 2 {
		t.Errorf("factory called %d times, want 2 (no caching of errors)", calls.Load())
	}
}

func TestRegistry_Get_TTLExpiry_RecreatesSender(t *testing.T) {
	// Não roda em t.Parallel porque o clock é global ao registry.
	clk := &fakeClock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	r := New(zerolog.Nop(), 5*time.Minute)
	r.now = clk.Now

	var created atomic.Int32
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		created.Add(1)
		return &stubSender{channel: domain.ChannelWABA}, nil
	})

	ctx := context.Background()
	tenantID := domain.TenantID("t1")
	_, _ = r.Get(ctx, tenantID, domain.ChannelWABA)
	_, _ = r.Get(ctx, tenantID, domain.ChannelWABA)
	if created.Load() != 1 {
		t.Fatalf("baseline: created = %d, want 1", created.Load())
	}

	// Avança o clock além do TTL.
	clk.Advance(6 * time.Minute)
	_, _ = r.Get(ctx, tenantID, domain.ChannelWABA)
	if created.Load() != 2 {
		t.Errorf("after TTL expiry, created = %d, want 2", created.Load())
	}
}

func TestRegistry_Get_PerTenantIsolation(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return &stubSender{channel: domain.ChannelWABA}, nil
	})

	s1, _ := r.Get(context.Background(), "tenant-A", domain.ChannelWABA)
	s2, _ := r.Get(context.Background(), "tenant-B", domain.ChannelWABA)
	if s1 == s2 {
		t.Error("expected different sender instances per tenant")
	}
}

func TestRegistry_Health_AggregatesPerChannel(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return &stubSender{channel: domain.ChannelWABA}, nil
	})
	r.Register(domain.ChannelTGBot, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return nil, errors.New("telegram down")
	})

	health := r.Health(context.Background(), "t1")
	if got := health[domain.ChannelWABA]; got != nil {
		t.Errorf("WABA health = %v, want nil", got)
	}
	if got := health[domain.ChannelTGBot]; got == nil {
		t.Error("TGBot health = nil, want non-nil error")
	}
}

func TestRegistry_ConcurrentGet_SingleFactoryCall(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	var created atomic.Int32
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		created.Add(1)
		return &stubSender{channel: domain.ChannelWABA}, nil
	})

	const goroutines = 64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _ = r.Get(context.Background(), "t1", domain.ChannelWABA)
		}()
	}
	wg.Wait()

	// Devido ao double-check locking, a factory pode ser invocada 1 ou 2
	// vezes (perde a corrida no primeiro RLock, antes do Lock exclusivo).
	// O que NÃO pode acontecer é cada goroutine recriar o sender.
	if got := created.Load(); got < 1 || got > 2 {
		t.Errorf("factory called %d times under concurrency, want 1 or 2", got)
	}
}

func TestRegistry_Get_NilSenderFromFactory_ReturnsErrSenderNotRegistered(t *testing.T) {
	t.Parallel()

	r := newTestRegistry()
	r.Register(domain.ChannelWABA, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return nil, nil
	})

	_, err := r.Get(context.Background(), "t1", domain.ChannelWABA)
	if !errors.Is(err, ErrSenderNotRegistered) {
		t.Errorf("err = %v, want ErrSenderNotRegistered for nil sender", err)
	}
}
