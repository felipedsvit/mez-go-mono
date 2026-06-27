// Package instagram — testes do adapter Instagram Direct.
//
// Cobre a matriz Send + Actions (D6):
//   - text message → mid-stub
//   - reaction → reaction payload
//   - mark_read → no-op silencioso (IG não tem read endpoint)
//   - edit/revoke → erro (API não suporta)
//   - typing/presence → erro
//   - action desconhecida → erro
//   - Capabilities: text/media/reactions.
package instagram

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient("", "", "page-1", "tok"), zerolog.Nop())
}

func TestNewClient_Defaults(t *testing.T) {
	t.Parallel()

	c := NewClient("", "", "p1", "tok")
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
	if got := a.Channel(); got != domain.ChannelIG {
		t.Errorf("Channel() = %q, want instagram", got)
	}
	caps := a.Capabilities()
	for _, c := range []port.Capability{port.CapText, port.CapMedia, port.CapReactions} {
		if !caps.Supports(c) {
			t.Errorf("IG should support %q", c)
		}
	}
	for _, c := range []port.Capability{
		port.CapEdit, port.CapDelete, port.CapTyping, port.CapPresence,
		port.CapGroups, port.CapPayments,
	} {
		if caps.Supports(c) {
			t.Errorf("IG should NOT support %q", c)
		}
	}
}

func TestAdapter_Send_Text(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	mid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelIG,
		PeerID:   "IGSID-XYZ",
		Type:     domain.MessageTypeText,
		Body:     "olá",
	})
	if err != nil {
		t.Fatalf("Send text: %v", err)
	}
	if mid == "" {
		t.Error("expected non-empty mid")
	}
}

func TestAdapter_Action_Reaction(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	mid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID:         "t1",
		Channel:          domain.ChannelIG,
		PeerID:           "IGSID-XYZ",
		Action:           port.ActionReaction,
		TargetProviderID: "mid.AAAA",
		ReactionEmoji:    "❤️",
	})
	if err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if mid == "" {
		t.Error("expected non-empty mid")
	}
}

func TestAdapter_Action_MarkRead_SilentNoOp(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	mid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Action: port.ActionMarkRead,
	})
	if err != nil {
		t.Errorf("mark_read on IG should be no-op, got error: %v", err)
	}
	if mid != "" {
		t.Errorf("mark_read should not return a mid, got %q", mid)
	}
}

func TestAdapter_Action_EditRevoke_NotSupported(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	for _, action := range []port.Action{port.ActionEdit, port.ActionRevoke} {
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelIG,
			PeerID: "IGSID-XYZ", Action: action,
		})
		if err == nil {
			t.Errorf("IG should reject %q", action)
		}
	}
}

func TestAdapter_Action_TypingPresence_NotSupported(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	for _, action := range []port.Action{port.ActionTyping, port.ActionPresence} {
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelIG,
			PeerID: "IGSID-XYZ", Action: action,
		})
		if err == nil {
			t.Errorf("IG should reject %q", action)
		}
	}
}

func TestAdapter_Action_Unknown_ReturnsError(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Action: port.Action("nonsense"),
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestInstagramCapabilities_MatchesMatrix(t *testing.T) {
	t.Parallel()

	caps := InstagramCapabilities()
	want := map[port.Capability]bool{
		port.CapText:      true,
		port.CapMedia:     true,
		port.CapReactions: true,
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("InstagramCapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
}
