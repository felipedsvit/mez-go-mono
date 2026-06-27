// Package whatsmeow — tests do pipeline whatsmeow (Fase 4 #67).
//
// Cobre:
//   - Manager: lazy init, LRU eviction, GetOrCreate idempotência.
//   - Dispatcher: bounded buffers + recover (C10) — panic num tenant não derruba o processo.
//   - Adapter: Send + Actions (D6).
//   - ReconnectThrottle: backoff exponencial após 429/515.
//
// Roda em CI via `go test ./...` (sem build tag — Fase 4 build verde).

package whatsmeow

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

// noopStateSaver é um whatsappStateSaver in-memory para testes.
type noopStateSaver struct {
	mu   sync.Mutex
	data map[string]struct {
		daySent   int
		dayAnchor time.Time
		timelock  time.Time
		health    int
	}
}

func newNoopStateSaver() *noopStateSaver {
	return &noopStateSaver{data: make(map[string]struct {
		daySent   int
		dayAnchor time.Time
		timelock  time.Time
		health    int
	})}
}

func (n *noopStateSaver) key(tenant, jid string) string { return tenant + "|" + jid }

func (n *noopStateSaver) LoadWarmupState(_ context.Context, tenant, jid string) (int, time.Time, time.Time, int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if v, ok := n.data[n.key(tenant, jid)]; ok {
		return v.daySent, v.dayAnchor, v.timelock, v.health
	}
	return 0, time.Now().UTC().Truncate(24 * time.Hour), time.Time{}, 100
}

func (n *noopStateSaver) SaveWarmupState(_ context.Context, tenant, jid string, ds int, anchor, tl time.Time, health int) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.data[n.key(tenant, jid)] = struct {
		daySent   int
		dayAnchor time.Time
		timelock  time.Time
		health    int
	}{ds, anchor, tl, health}
	return nil
}

func TestManager_GetOrCreate_LazyInit(t *testing.T) {
	log := zerolog.Nop()
	mgr := NewManager(DefaultConfig(), nil, log)
	ctx := context.Background()

	tenantID := domain.TenantID("tenant-1")
	created := atomic.Int32{}
	factory := func(_ context.Context, _ domain.TenantID) (Client, error) {
		created.Add(1)
		return NewStubClient("t1", log), nil
	}

	a1, err := mgr.GetOrCreate(ctx, tenantID, factory)
	if err != nil {
		t.Fatalf("first GetOrCreate: %v", err)
	}
	if a1 == nil {
		t.Fatal("adapter nil")
	}
	if created.Load() != 1 {
		t.Errorf("expected 1 create, got %d", created.Load())
	}

	// Second call: same instance, no re-create.
	a2, err := mgr.GetOrCreate(ctx, tenantID, factory)
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if a1 != a2 {
		t.Error("expected same adapter instance")
	}
	if created.Load() != 1 {
		t.Errorf("expected 1 create (cached), got %d", created.Load())
	}
}

func TestManager_Bounded_LRUEviction(t *testing.T) {
	log := zerolog.Nop()
	mgr := NewManager(Config{
		MaxActiveTenants: 2,
		ConnectTimeout:   time.Second,
		MaxBackoff:       time.Minute,
	}, nil, log)
	ctx := context.Background()
	factory := func(_ context.Context, _ domain.TenantID) (Client, error) {
		return NewStubClient("t", log), nil
	}

	// Cria 3 tenants; o primeiro deve ser evictado.
	_, _ = mgr.GetOrCreate(ctx, "t1", factory)
	_, _ = mgr.GetOrCreate(ctx, "t2", factory)
	_, _ = mgr.GetOrCreate(ctx, "t3", factory)

	if mgr.Size() != 2 {
		t.Errorf("size = %d, want 2", mgr.Size())
	}
	if _, ok := mgr.Get("t1"); ok {
		t.Error("t1 should have been evicted (LRU)")
	}
	if _, ok := mgr.Get("t2"); !ok {
		t.Error("t2 should still be present")
	}
	if _, ok := mgr.Get("t3"); !ok {
		t.Error("t3 should still be present")
	}
}

func TestManager_DisconnectAll(t *testing.T) {
	log := zerolog.Nop()
	mgr := NewManager(DefaultConfig(), nil, log)
	ctx := context.Background()
	_, _ = mgr.GetOrCreate(ctx, "t1", func(_ context.Context, _ domain.TenantID) (Client, error) {
		return NewStubClient("t1", log), nil
	})

	mgr.DisconnectAll()
	// Após DisconnectAll, Size deve ser mantido (state in-memory) mas clients
	// estão disconnected.
	if mgr.Size() != 1 {
		t.Errorf("size = %d, want 1", mgr.Size())
	}
}

func TestReconnectThrottle_Backoff429(t *testing.T) {
	tr := NewReconnectThrottle(60 * time.Second)
	now := time.Unix(1000, 0)

	// Primeira chamada: backoff 60s (minInterval). reason="backoff".
	wait, reason := tr.Next(errors.New("status 429"), now)
	if reason != "backoff" {
		t.Errorf("reason = %q, want backoff", reason)
	}
	if wait < 60*time.Second {
		t.Errorf("wait = %v, want >=60s", wait)
	}

	// Segunda chamada (no mesmo now, antes do "last" da primeira):
	// backoff dobra para 120s, base=120s. Como elapsed é negativo,
	// wait = 120 - (-60) = 180s (espera até t+180s).
	wait2, _ := tr.Next(errors.New("status 429"), now)
	if wait2 < 120*time.Second {
		t.Errorf("wait2 = %v, want >=120s (backoff dobrou)", wait2)
	}

	// Reset volta ao min interval.
	tr.Reset()
	wait3, reason3 := tr.Next(errors.New("other error"), now)
	if reason3 != "throttled" {
		t.Errorf("after reset reason = %q, want throttled (erro não-429)", reason3)
	}
	if wait3 < 60*time.Second {
		t.Errorf("after reset wait3 = %v, want >=60s", wait3)
	}
}

func TestWarmup_QuotaRamp(t *testing.T) {
	store := newNoopStateSaver()
	// Inicia com start = 4 dias atrás (dia 4 → 10/dia).
	w := NewWarmup("t1", "jid1", store)
	w.start = time.Now().UTC().Add(-4 * 24 * time.Hour)
	now := time.Now().UTC()

	allow, _ := w.AllowSend(now, false)
	if !allow {
		t.Error("day 4 should allow (cota=10)")
	}
}

func TestAdapter_Send_NotConnected(t *testing.T) {
	log := zerolog.Nop()
	stub := NewStubClient("t1", log)
	dispatch := NewDispatcher(log)
	adapter := NewAdapter("t1", stub, dispatch, nil, log)
	// Sem Connect; SetConnected não foi chamado.

	_, err := adapter.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWAWeb,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeText,
		Body:     "oi",
	})
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestAdapter_Send_OK(t *testing.T) {
	log := zerolog.Nop()
	stub := NewStubClient("t1", log)
	_ = stub.Connect(context.Background())
	dispatch := NewDispatcher(log)
	adapter := NewAdapter("t1", stub, dispatch, nil, log)
	adapter.SetConnected(true)

	wamid, err := adapter.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWAWeb,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeText,
		Body:     "oi",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if wamid == "" {
		t.Error("wamid vazio")
	}
}

func TestDispatcher_PanicRecovered(t *testing.T) {
	log := zerolog.Nop()
	d := NewDispatcher(log)
	ctx := context.Background()
	called := atomic.Int32{}

	d.Start(ctx,
		func(_ context.Context, _ any) {
			called.Add(1)
			panic("boom") // simulando panic num handler — recover() deve segurar
		},
		func(_ context.Context, _ any) {},
	)
	defer d.Stop()

	// Enfileira 5 eventos; cada handler deve panicar mas o dispatcher
	// sobrevive (C10).
	for i := 0; i < 5; i++ {
		d.HandleRaw(MessageEvent{})
	}
	time.Sleep(100 * time.Millisecond) // deixa as goroutines processarem
	if called.Load() != 5 {
		t.Errorf("handler chamado %d vezes, want 5", called.Load())
	}
}

func TestDispatcher_BoundedDrop(t *testing.T) {
	log := zerolog.Nop()
	d := NewDispatcher(log)
	ctx := context.Background()
	processed := atomic.Int32{}

	// Handler lento: enfileira + bloqueia para encher o buffer.
	d.Start(ctx,
		func(_ context.Context, _ any) {
			processed.Add(1)
			time.Sleep(50 * time.Millisecond)
		},
		func(_ context.Context, _ any) {},
	)
	defer d.Stop()

	// Estoura o buffer (2048) com eventos — alguns devem ser dropados.
	for i := 0; i < eventBuffer+10; i++ {
		d.HandleRaw(MessageEvent{})
	}
	if processed.Load() == 0 {
		t.Error("esperava ao menos 1 processado")
	}
	t.Logf("processados=%d (drop-safe em ação)", processed.Load())
}
