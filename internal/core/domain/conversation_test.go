package domain_test

import (
	"errors"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// TestNewConversation_Valid: a factory cria a conversa em Open.
func TestNewConversation_Valid(t *testing.T) {
	conv, err := domain.NewConversation("tenant-1", domain.ChannelWABA, "contact-1", "peer-1")
	if err != nil {
		t.Fatalf("NewConversation: %v", err)
	}
	if conv.Status != domain.ConvStatusOpen {
		t.Errorf("Status mismatch: %s", conv.Status)
	}
	if !conv.IsOpen() {
		t.Error("IsOpen should be true")
	}
	if conv.IsResolved() {
		t.Error("IsResolved should be false")
	}
}

// TestNewConversation_Invalid cobre os caminhos de erro da factory.
func TestNewConversation_Invalid(t *testing.T) {
	cases := []struct {
		name                string
		tenant, ch, contact string
	}{
		{"empty tenant", "", string(domain.ChannelWABA), "c"},
		{"empty channel", "t", "", "c"},
		{"empty contact", "t", string(domain.ChannelWABA), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewConversation(domain.TenantID(c.tenant), domain.Channel(c.ch), domain.ContactID(c.contact), "p")
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}

// TestConversation_Assign: atribui a um agente; idempotente.
func TestConversation_Assign(t *testing.T) {
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	if err := conv.Assign("agent-1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if conv.AssignedAgent != "agent-1" {
		t.Errorf("AssignedAgent mismatch: %s", conv.AssignedAgent)
	}
	// Idempotente.
	if err := conv.Assign("agent-1"); err != nil {
		t.Errorf("re-Assign should be no-op: %v", err)
	}
	// Mudar de agente.
	if err := conv.Assign("agent-2"); err != nil {
		t.Errorf("Assign agent-2: %v", err)
	}
	if conv.AssignedAgent != "agent-2" {
		t.Errorf("AssignedAgent should be agent-2, got: %s", conv.AssignedAgent)
	}
	// Unassign.
	if err := conv.Assign(""); err != nil {
		t.Errorf("Assign empty: %v", err)
	}
	if conv.AssignedAgent != "" {
		t.Error("AssignedAgent should be empty after unassign")
	}
}

// TestConversation_Resolve: Resolve marca como resolvido; idempotente;
// Assign após Resolve falha (FSM guard).
func TestConversation_Resolve(t *testing.T) {
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	if err := conv.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !conv.IsResolved() {
		t.Error("IsResolved should be true")
	}
	// Idempotente.
	if err := conv.Resolve(); err != nil {
		t.Errorf("re-Resolve should be no-op: %v", err)
	}
	// Assign após Resolve falha.
	if err := conv.Assign("agent-x"); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// TestConversation_NewInboundMessage: AR method cria Message com
// Received; conversa não pode criar Message após Resolve (FSM guard).
func TestConversation_NewInboundMessage(t *testing.T) {
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	msg, err := conv.NewInboundMessage("hello", "provider-1")
	if err != nil {
		t.Fatalf("NewInboundMessage: %v", err)
	}
	if msg.Status != domain.MessageStatusReceived {
		t.Errorf("new Message should be Received, got: %s", msg.Status)
	}
	if msg.ConversationID != conv.ID {
		t.Errorf("Message.ConversationID mismatch: %s", msg.ConversationID)
	}
	if !msg.IsInbound() {
		t.Error("message should be inbound")
	}
	// Conversa resolvida não pode receber mensagem.
	_ = conv.Resolve()
	_, err = conv.NewInboundMessage("world", "provider-2")
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}
