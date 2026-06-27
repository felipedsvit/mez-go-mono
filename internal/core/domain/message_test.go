package domain_test

import (
	"errors"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// TestNewOutboundMessage: factory outbound usa Notified como status inicial.
func TestNewOutboundMessage(t *testing.T) {
	msg, err := domain.NewOutboundMessage("t", domain.ChannelWABA, "conv-1", "c-1", "hello", domain.MessageTypeText)
	if err != nil {
		t.Fatalf("NewOutboundMessage: %v", err)
	}
	if msg.Status != domain.MessageStatusNotified {
		t.Errorf("outbound should start Notified, got: %s", msg.Status)
	}
	if !msg.IsOutbound() {
		t.Error("message should be outbound")
	}
	if msg.IsInbound() {
		t.Error("outbound should not be inbound")
	}
}

// TestMessage_MarkRouted: Received → Routed; FSM guard.
func TestMessage_MarkRouted(t *testing.T) {
	// Cria via AR para começar em Received.
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	msg, _ := conv.NewInboundMessage("hello", "pm-1")

	if err := msg.MarkRouted(); err != nil {
		t.Fatalf("MarkRouted: %v", err)
	}
	if msg.Status != domain.MessageStatusRouted {
		t.Errorf("Status mismatch: %s", msg.Status)
	}
	// Não pode chamar de novo.
	if err := msg.MarkRouted(); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// TestMessage_MarkNotified: idempotente; transita Received|Routed → Notified.
func TestMessage_MarkNotified(t *testing.T) {
	conv, _ := domain.NewConversation("t", domain.ChannelWABA, "c", "p")
	msg, _ := conv.NewInboundMessage("hello", "pm-1")

	// Received → Notified: ok.
	if err := msg.MarkNotified(); err != nil {
		t.Fatalf("MarkNotified from Received: %v", err)
	}
	// Idempotente.
	if err := msg.MarkNotified(); err != nil {
		t.Errorf("re-MarkNotified should be no-op: %v", err)
	}

	// Novo path: Received → Routed → Notified.
	msg2, _ := conv.NewInboundMessage("hi", "pm-2")
	_ = msg2.MarkRouted()
	if err := msg2.MarkNotified(); err != nil {
		t.Errorf("MarkNotified from Routed: %v", err)
	}
}

// TestNewContact_Valid: factory valida campos obrigatórios.
func TestNewContact_Valid(t *testing.T) {
	c, err := domain.NewContact("t", domain.ChannelWABA, "peer-1")
	if err != nil {
		t.Fatalf("NewContact: %v", err)
	}
	if c.ID == "" {
		t.Error("ID should be generated")
	}
}

// TestNewContact_Invalid: cobre entradas inválidas.
func TestNewContact_Invalid(t *testing.T) {
	cases := []struct {
		name           string
		tenant, ch, id string
	}{
		{"empty tenant", "", string(domain.ChannelWABA), "p"},
		{"empty channel", "t", "", "p"},
		{"empty provider id", "t", string(domain.ChannelWABA), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewContact(domain.TenantID(c.tenant), domain.Channel(c.ch), c.id)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}

// TestContact_Rename: ignora nome vazio (limpar).
func TestContact_Rename(t *testing.T) {
	c, _ := domain.NewContact("t", domain.ChannelWABA, "p")
	c.Rename("  John  ")
	if c.Name != "John" {
		t.Errorf("Name should be trimmed: %s", c.Name)
	}
	c.Rename("   ")
	if c.Name != "John" {
		t.Errorf("empty Rename should be ignored, got: %s", c.Name)
	}
}
