// Package messenger — testes do adapter Facebook Messenger.
//
// Cobre a matriz Send + Actions (D6):
//   - text message → mid-stub
//   - reaction → no-op stub
//   - mark_read/typing → no-op silencioso
//   - edit/revoke/presence → erro (MSG não suporta)
//   - action desconhecida → erro
//   - Capabilities: text/media/reactions/mark_read/typing/groups/persistent_menu.
package messenger

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient("", "", "tok"), zerolog.Nop())
}

func TestNewClient_Defaults(t *testing.T) {
	t.Parallel()

	c := NewClient("", "", "tok")
	if c.baseURL != "https://graph.facebook.com" {
		t.Errorf("baseURL default missing")
	}
	if c.version != "v21.0" {
		t.Errorf("version default missing")
	}
}

func TestAdapter_ChannelAndCapabilities(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	if got := a.Channel(); got != domain.ChannelMSG {
		t.Errorf("Channel() = %q, want messenger", got)
	}
	caps := a.Capabilities()
	want := []port.Capability{
		port.CapText, port.CapMedia, port.CapReactions,
		port.CapMarkRead, port.CapTyping, port.CapGroups, port.CapPersistentMenu,
	}
	for _, c := range want {
		if !caps.Supports(c) {
			t.Errorf("MSG should support %q", c)
		}
	}
	for _, c := range []port.Capability{
		port.CapEdit, port.CapDelete, port.CapPresence,
		port.CapPayments, port.CapCalls,
	} {
		if caps.Supports(c) {
			t.Errorf("MSG should NOT support %q", c)
		}
	}
}

func TestAdapter_Send_Text(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	mid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Type: domain.MessageTypeText, Body: "olá",
	})
	if err != nil {
		t.Fatalf("Send text: %v", err)
	}
	if mid == "" {
		t.Error("expected non-empty mid")
	}
}

func TestAdapter_Action_Reaction_Stub(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.ActionReaction,
	})
	if err != nil {
		t.Errorf("reaction stub: %v", err)
	}
}

func TestAdapter_Action_MarkReadTyping_NoOp(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	for _, action := range []port.Action{port.ActionMarkRead, port.ActionTyping} {
		mid, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelMSG,
			PeerID: "PSID-XYZ", Action: action,
		})
		if err != nil {
			t.Errorf("%s should be no-op, got error: %v", action, err)
		}
		if mid != "" {
			t.Errorf("%s should not return mid, got %q", action, mid)
		}
	}
}

func TestAdapter_Action_EditRevokePresence_NotSupported(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	for _, action := range []port.Action{port.ActionEdit, port.ActionRevoke, port.ActionPresence} {
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelMSG,
			PeerID: "PSID-XYZ", Action: action,
		})
		if err == nil {
			t.Errorf("MSG should reject %q", action)
		}
	}
}

func TestAdapter_Action_Unknown_ReturnsError(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.Action("nonsense"),
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestMessengerCapabilities_MatchesMatrix(t *testing.T) {
	t.Parallel()

	caps := MessengerCapabilities()
	want := map[port.Capability]bool{
		port.CapText:           true,
		port.CapMedia:          true,
		port.CapReactions:      true,
		port.CapMarkRead:       true,
		port.CapTyping:         true,
		port.CapGroups:         true,
		port.CapPersistentMenu: true,
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("MessengerCapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
}
