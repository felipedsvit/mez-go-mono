package messaging_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
)

// fakeConvRepo: stub in-memory para ConversationRepo (issue #126).
type fakeConvRepo struct {
	convs map[domain.ConversationID]*domain.Conversation
}

func newFakeConvRepo() *fakeConvRepo {
	return &fakeConvRepo{convs: make(map[domain.ConversationID]*domain.Conversation)}
}

func (f *fakeConvRepo) ListByTenant(_ context.Context, _ domain.TenantID) ([]domain.Conversation, error) {
	out := make([]domain.Conversation, 0, len(f.convs))
	for _, c := range f.convs {
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeConvRepo) Get(_ context.Context, id domain.ConversationID) (*domain.Conversation, error) {
	c, ok := f.convs[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeConvRepo) Upsert(_ context.Context, c *domain.Conversation) error {
	cp := *c
	f.convs[c.ID] = &cp
	return nil
}

func (f *fakeConvRepo) UpdateStatus(_ context.Context, _ domain.ConversationID, _ domain.ConversationStatus) error {
	return nil
}

type fakeMsgRepo struct {
	msgs map[domain.MessageID]*domain.Message
}

func (f *fakeMsgRepo) ListByConversation(_ context.Context, _ domain.ConversationID) ([]domain.Message, error) {
	return []domain.Message{}, nil
}
func (f *fakeMsgRepo) Get(_ context.Context, _ domain.MessageID) (*domain.Message, error) {
	return nil, port.ErrNotFound
}
func (f *fakeMsgRepo) Insert(_ context.Context, _ *domain.Message) error { return nil }
func (f *fakeMsgRepo) UpdateStatus(_ context.Context, _ domain.MessageID, _ domain.MessageStatus) error {
	return nil
}
func (f *fakeMsgRepo) SelectUnroutedMessages(_ context.Context, _ int) ([]domain.Message, error) {
	return nil, nil
}
func (f *fakeMsgRepo) MarkRouted(_ context.Context, _ domain.MessageID) error { return nil }

// TestListService_ListConversations: use case chama o repo (não transport).
func TestListService_ListConversations(t *testing.T) {
	convRepo := newFakeConvRepo()
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	convRepo.convs[conv.ID] = conv

	msgRepo := &fakeMsgRepo{}
	svc := ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())

	got, err := svc.ListConversations(context.Background(), "t")
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 conversation, got: %d", len(got))
	}
}

// TestListService_AssignConversation_NotFound: erro propagado do repo.
func TestListService_AssignConversation_NotFound(t *testing.T) {
	convRepo := newFakeConvRepo()
	msgRepo := &fakeMsgRepo{}
	svc := ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())

	err := svc.AssignConversation(context.Background(), "t", "missing", "agent-1")
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("expected port.ErrNotFound, got: %v", err)
	}
}

// TestListService_AssignConversation_Resolved: FSM guard.
func TestListService_AssignConversation_Resolved(t *testing.T) {
	convRepo := newFakeConvRepo()
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	_ = conv.Resolve()
	convRepo.convs[conv.ID] = conv

	msgRepo := &fakeMsgRepo{}
	svc := ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())

	err := svc.AssignConversation(context.Background(), "t", conv.ID, "agent-1")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// TestListService_AssignConversation_OK: caminho feliz.
func TestListService_AssignConversation_OK(t *testing.T) {
	convRepo := newFakeConvRepo()
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	convRepo.convs[conv.ID] = conv

	msgRepo := &fakeMsgRepo{}
	svc := ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())

	if err := svc.AssignConversation(context.Background(), "t", conv.ID, "agent-1"); err != nil {
		t.Fatalf("AssignConversation: %v", err)
	}
	if convRepo.convs[conv.ID].AssignedAgent != "agent-1" {
		t.Errorf("AssignedAgent should be agent-1, got: %s", convRepo.convs[conv.ID].AssignedAgent)
	}
}

// TestListService_ResolveConversation_OK: caminho feliz.
func TestListService_ResolveConversation_OK(t *testing.T) {
	convRepo := newFakeConvRepo()
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	convRepo.convs[conv.ID] = conv

	msgRepo := &fakeMsgRepo{}
	svc := ucmessaging.NewListService(convRepo, msgRepo, zerolog.Nop())

	if err := svc.ResolveConversation(context.Background(), "t", conv.ID); err != nil {
		t.Fatalf("ResolveConversation: %v", err)
	}
	if !convRepo.convs[conv.ID].IsResolved() {
		t.Error("conversation should be resolved")
	}
}
