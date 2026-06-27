package relay

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/rs/zerolog"
)

// fakeOutbox é uma implementação em memória de port.OutboxRelay.
type fakeOutbox struct {
	mu       sync.Mutex
	pending  []domain.Message
	markSent map[domain.MessageID]bool
	markFail map[domain.MessageID]int
}

func newFakeOutbox() *fakeOutbox {
	return &fakeOutbox{
		markSent: make(map[domain.MessageID]bool),
		markFail: make(map[domain.MessageID]int),
	}
}

func (f *fakeOutbox) Insert(_ context.Context, _ *domain.Message) error {
	return nil
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
	return nil
}

func (f *fakeOutbox) MarkDLQ(_ context.Context, id domain.MessageID, _ error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markSent[id] = false // dlq marker (sentinel)
	return nil
}

func (f *fakeOutbox) push(m domain.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, m)
}

func TestRelay_NoopSender_LeavesPending(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	sender := NewNoopSender(log)

	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, sender, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10}, log)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = r.Run(ctx)
	_ = r.drain(ctx)

	if out.markSent["m1"] {
		t.Error("noop sender should not mark sent")
	}
	if out.markFail["m1"] > 0 {
		t.Errorf("noop sender should not mark failed; got %d failures", out.markFail["m1"])
	}
}

type okSender struct {
	called int
}

func (o *okSender) Send(_ context.Context, _ domain.Message) (string, error) {
	o.called++
	return "provider-msg-1", nil
}

func TestRelay_OkSender_MarksSent(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	sender := &okSender{}

	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, sender, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10}, log)
	r.drain(context.Background())

	if !out.markSent["m1"] {
		t.Error("expected m1 marked sent")
	}
	if sender.called != 1 {
		t.Errorf("sender called %d, want 1", sender.called)
	}
}

type errSender struct{}

var errFailed = errors.New("provider down")

func (e *errSender) Send(_ context.Context, _ domain.Message) (string, error) {
	return "", errFailed
}

func TestRelay_ErrSender_MarksFailed(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	sender := &errSender{}

	out.push(domain.Message{ID: "m1", Channel: domain.ChannelWABA, Body: "hi"})

	r := New(out, sender, Config{PollInterval: 10 * time.Millisecond, BatchSize: 10}, log)
	r.drain(context.Background())

	if out.markFail["m1"] != 1 {
		t.Errorf("expected 1 failure, got %d", out.markFail["m1"])
	}
	if out.markSent["m1"] {
		t.Error("should not be marked sent on error")
	}
}

func TestRelay_Notify_TriggersDrain(t *testing.T) {
	log := zerolog.Nop()
	out := newFakeOutbox()
	sender := &okSender{}

	r := New(out, sender, Config{PollInterval: 1 * time.Hour, BatchSize: 10}, log)

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

func TestNoopSender_ImplementsSender(t *testing.T) {
	var s Sender = NewNoopSender(zerolog.Nop())
	if s == nil {
		t.Fatal("noop sender nil")
	}
	_, err := s.Send(context.Background(), domain.Message{ID: "x"})
	if !errors.Is(err, ErrSenderNotImplemented) {
		t.Errorf("err = %v, want ErrSenderNotImplemented", err)
	}
}
