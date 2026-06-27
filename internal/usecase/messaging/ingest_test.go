package messaging

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/rs/zerolog"
)

// ---- fakes ---------------------------------------------------------------

type fakeContactRepo struct {
	mu      sync.Mutex
	store   map[string]domain.Contact
	upserts int
}

func newFakeContactRepo() *fakeContactRepo {
	return &fakeContactRepo{store: make(map[string]domain.Contact)}
}

func (f *fakeContactRepo) Upsert(_ context.Context, c *domain.Contact) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upserts++
	f.store[string(c.ID)] = *c
	return nil
}

func (f *fakeContactRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Contact, error) {
	return nil, nil
}
func (f *fakeContactRepo) Get(_ context.Context, id domain.ContactID) (*domain.Contact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.store[string(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &c, nil
}

type fakeConvRepo struct {
	mu    sync.Mutex
	store map[string]domain.Conversation
}

func newFakeConvRepo() *fakeConvRepo {
	return &fakeConvRepo{store: make(map[string]domain.Conversation)}
}
func (f *fakeConvRepo) Upsert(_ context.Context, c *domain.Conversation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.store[string(c.ID)] = *c
	return nil
}
func (f *fakeConvRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Conversation, error) {
	return nil, nil
}
func (f *fakeConvRepo) Get(_ context.Context, id domain.ConversationID) (*domain.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.store[string(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &c, nil
}
func (f *fakeConvRepo) UpdateStatus(_ context.Context, _ domain.ConversationID, _ domain.ConversationStatus) error {
	return nil
}

type fakeMsgRepo struct {
	mu     sync.Mutex
	store  map[string]domain.Message
	insert int
}

func newFakeMsgRepo() *fakeMsgRepo {
	return &fakeMsgRepo{store: make(map[string]domain.Message)}
}
func (f *fakeMsgRepo) Insert(_ context.Context, m *domain.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insert++
	f.store[string(m.ID)] = *m
	return nil
}
func (f *fakeMsgRepo) ListByConversation(_ context.Context, _ domain.ConversationID) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeMsgRepo) Get(_ context.Context, id domain.MessageID) (*domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.store[string(id)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &m, nil
}
func (f *fakeMsgRepo) UpdateStatus(_ context.Context, _ domain.MessageID, _ domain.MessageStatus) error {
	return nil
}
func (f *fakeMsgRepo) SelectUnroutedMessages(_ context.Context, _ int) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeMsgRepo) MarkRouted(_ context.Context, _ domain.MessageID) error {
	return nil
}

type fakeOutbox struct {
	mu      sync.Mutex
	inserts int
}

func (f *fakeOutbox) Insert(_ context.Context, _ *domain.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inserts++
	return nil
}
func (f *fakeOutbox) PendingCount(_ context.Context) (int, error)                     { return 0, nil }
func (f *fakeOutbox) ClaimNext(_ context.Context, _ int) ([]domain.Message, error)    { return nil, nil }
func (f *fakeOutbox) MarkSent(_ context.Context, _ domain.MessageID) error            { return nil }
func (f *fakeOutbox) MarkFailed(_ context.Context, _ domain.MessageID, _ error) error { return nil }

type fakeBus struct {
	mu     sync.Mutex
	events []event.InboundEvent
}

func (f *fakeBus) PublishInbound(e event.InboundEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

// fakeTxRunner é um TxRunner em memória que apenas executa fn().
// O tenant_id é setado no ctx via setConfig apenas para fins de inspeção.
type fakeTxRunner struct {
	mu       sync.Mutex
	executed int
}

func (f *fakeTxRunner) RunInTenantTx(_ context.Context, _ domain.TenantID, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	f.executed++
	f.mu.Unlock()
	return fn(context.Background())
}
func (f *fakeTxRunner) RunAsPlatform(_ context.Context, _ string, fn func(ctx context.Context) error) error {
	return fn(context.Background())
}

// ---- tests ---------------------------------------------------------------

func TestIngestor_HappyPath(t *testing.T) {
	contacts := newFakeContactRepo()
	convs := newFakeConvRepo()
	msgs := newFakeMsgRepo()
	outbox := &fakeOutbox{}
	tx := &fakeTxRunner{}
	bus := &fakeBus{}
	log := zerolog.Nop()

	ing := NewIngestor(contacts, convs, msgs, outbox, tx,
		WithBus(bus), WithLogger(log))

	id, err := ing.Ingest(context.Background(), event.InboundEvent{
		TenantID:  "t1",
		Channel:   event.ChannelWABA,
		MessageID: "wamid.ABC",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if id == "" {
		t.Fatal("empty message id")
	}
	if contacts.upserts != 1 {
		t.Errorf("contacts upserts = %d, want 1", contacts.upserts)
	}
	if msgs.insert != 1 {
		t.Errorf("messages insert = %d, want 1", msgs.insert)
	}
	if outbox.inserts != 1 {
		t.Errorf("outbox inserts = %d, want 1", outbox.inserts)
	}
	if len(bus.events) != 1 {
		t.Errorf("bus events = %d, want 1", len(bus.events))
	}
	if tx.executed != 1 {
		t.Errorf("tx executed = %d, want 1", tx.executed)
	}
}

func TestIngestor_RequiresTenantID(t *testing.T) {
	ing := NewIngestor(newFakeContactRepo(), newFakeConvRepo(), newFakeMsgRepo(), &fakeOutbox{}, &fakeTxRunner{})
	_, err := ing.Ingest(context.Background(), event.InboundEvent{
		Channel:   event.ChannelWABA,
		MessageID: "x",
	})
	if err == nil {
		t.Error("expected error for empty tenant_id")
	}
}

func TestIngestor_RequiresMessageID(t *testing.T) {
	ing := NewIngestor(newFakeContactRepo(), newFakeConvRepo(), newFakeMsgRepo(), &fakeOutbox{}, &fakeTxRunner{})
	_, err := ing.Ingest(context.Background(), event.InboundEvent{
		TenantID: "t1",
		Channel:  event.ChannelWABA,
	})
	if err == nil {
		t.Error("expected error for empty message_id")
	}
}

func TestIngestor_WorksWithoutBus(t *testing.T) {
	ing := NewIngestor(newFakeContactRepo(), newFakeConvRepo(), newFakeMsgRepo(), &fakeOutbox{}, &fakeTxRunner{})

	id, err := ing.Ingest(context.Background(), event.InboundEvent{
		TenantID:  "t1",
		Channel:   event.ChannelTGBot,
		MessageID: "tg:1:2",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if id == "" {
		t.Fatal("empty message id")
	}
}
