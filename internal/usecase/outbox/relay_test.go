package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.VerifyTestMain(m)
}

// fakeOutbox é uma implementação em memória de port.OutboxRelay.
type fakeOutbox struct {
	mu       sync.Mutex
	pending  []domain.Message
	markSent map[domain.MessageID]bool
	markFail map[domain.MessageID]int
	markDLQ  map[domain.MessageID]bool
	attempts map[domain.MessageID]int
}

func newFakeOutbox() *fakeOutbox {
	return &fakeOutbox{
		markSent: make(map[domain.MessageID]bool),
		markFail: make(map[domain.MessageID]int),
		markDLQ:  make(map[domain.MessageID]bool),
		attempts: make(map[domain.MessageID]int),
	}
}

func (f *fakeOutbox) ClaimNext(_ context.Context, batchSize int) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, nil
	}
	n := batchSize
	if n > len(f.pending) {
		n = len(f.pending)
	}
	batch := append([]domain.Message(nil), f.pending[:n]...)
	f.pending = f.pending[n:]
	return batch, nil
}

func (f *fakeOutbox) PendingCount(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending), nil
}

func (f *fakeOutbox) MarkSent(_ context.Context, id domain.MessageID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markSent[id] = true
	return nil
}

func (f *fakeOutbox) MarkFailed(_ context.Context, id domain.MessageID, _ error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFail[id]++
	f.attempts[id] = f.markFail[id]
	return nil
}

func (f *fakeOutbox) MarkDLQ(_ context.Context, id domain.MessageID, _ error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markDLQ[id] = true
	return nil
}

func (f *fakeOutbox) GetAttempts(_ context.Context, id domain.MessageID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attempts[id], nil
}

func (f *fakeOutbox) push(m domain.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, m)
}

// fakeRegistry é uma implementação de port.SenderRegistry que retorna o
// sender configurado para um channel específico. Internamente usa factories
// (compatível com o port) que retornam sempre a mesma instância cached.
type fakeRegistry struct {
	mu        sync.Mutex
	factories map[domain.Channel]port.SenderFactory
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{factories: make(map[domain.Channel]port.SenderFactory)}
}

func (r *fakeRegistry) Register(ch domain.Channel, factory port.SenderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[ch] = factory
}

// RegisterSender é um helper que wrappa uma instância de Sender em factory.
func (r *fakeRegistry) RegisterSender(ch domain.Channel, s port.Sender) {
	r.Register(ch, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return s, nil
	})
}

func (r *fakeRegistry) Get(_ context.Context, _ domain.TenantID, ch domain.Channel) (port.Sender, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.factories[ch]
	if !ok {
		return nil, port.ErrSenderNotRegistered
	}
	return f(context.Background(), "")
}

func (r *fakeRegistry) Channels() []domain.Channel {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Channel, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	return out
}

func (r *fakeRegistry) Health(_ context.Context, _ domain.TenantID) map[domain.Channel]error {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[domain.Channel]error, len(r.factories))
	for k := range r.factories {
		out[k] = nil
	}
	return out
}

// okSender simula um sender que sempre funciona.
type okSender struct {
	called int
}

func (o *okSender) Send(_ context.Context, _ port.OutboundRequest) (string, error) {
	o.called++
	return "provider-msg-1", nil
}

func (o *okSender) Capabilities() port.CapabilitySet { return port.CapabilitySet{} }
func (o *okSender) Channel() domain.Channel          { return domain.ChannelWABA }

// errSender simula um provider sempre fora.
type errSender struct{}

var errFailed = errors.New("provider down")

func (e *errSender) Send(_ context.Context, _ port.OutboundRequest) (string, error) {
	return "", errFailed
}

func (e *errSender) Capabilities() port.CapabilitySet { return port.CapabilitySet{} }
func (e *errSender) Channel() domain.Channel          { return domain.ChannelWABA }

func TestRelay_NilRegistry_LeavesPending(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, nil, nil, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10}, log)
	_ = r.drain(context.Background())

	if out.markSent["m1"] {
		t.Error("nil registry should not mark sent")
	}
}

func TestRelay_OkSender_MarksSent(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	reg := newFakeRegistry()
	sender := &okSender{}
	reg.RegisterSender(domain.ChannelWABA, sender)
	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, reg, nil, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10}, log)
	_ = r.drain(context.Background())

	if !out.markSent["m1"] {
		t.Error("expected m1 marked sent")
	}
	if sender.called != 1 {
		t.Errorf("sender called %d, want 1", sender.called)
	}
}

func TestRelay_ErrSender_MarksFailed(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	reg := newFakeRegistry()
	reg.RegisterSender(domain.ChannelWABA, &errSender{})
	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, reg, nil, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10, MaxAttempts: 3}, log)
	_ = r.drain(context.Background())

	if out.markFail["m1"] != 1 {
		t.Errorf("expected 1 failure, got %d", out.markFail["m1"])
	}
	if out.markSent["m1"] {
		t.Error("should not be marked sent on error")
	}
}

func TestRelay_MaxAttempts_MovesToDLQ(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	reg := newFakeRegistry()
	reg.RegisterSender(domain.ChannelWABA, &errSender{})
	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, reg, nil, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10, MaxAttempts: 3}, log)
	// 3 tentativas.
	for i := 0; i < 3; i++ {
		out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})
		_ = r.drain(context.Background())
	}

	if !out.markDLQ["m1"] {
		t.Error("expected m1 moved to DLQ after 3 attempts")
	}
}

func TestRelay_UnregisteredChannel_RecordsFailure(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	reg := newFakeRegistry() // sem nada registrado
	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, reg, nil, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10, MaxAttempts: 2}, log)
	_ = r.drain(context.Background())

	if out.markFail["m1"] != 1 {
		t.Errorf("expected 1 failure, got %d", out.markFail["m1"])
	}
}

func TestRelay_Notify_TriggersDrain(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	reg := newFakeRegistry()
	sender := &okSender{}
	reg.RegisterSender(domain.ChannelWABA, sender)

	r := New(out, reg, nil, Config{PollInterval: 1 * time.Hour, BatchSize: 10}, log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})
		r.Notify()
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_ = r.Run(ctx)

	if !out.markSent["m1"] {
		t.Error("expected m1 to be drained via Notify")
	}
}
