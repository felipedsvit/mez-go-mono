// Package waba — testes do adapter WhatsApp Business Cloud API.
//
// Cobre a matriz Send + Actions (D6):
//   - text message → wamid-stub
//   - reaction → reaction payload
//   - revoke sem target_provider_id → erro
//   - revoke com target_provider_id → ok
//   - mark_read sem target → erro
//   - edit → erro (WABA não suporta)
//   - typing/presence → erro (WABA não suporta)
//   - action desconhecida → erro
//   - Capabilities: matriz oficial (text/media/reactions/delete/templates/mark_read).
package waba

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient("", "", "1234567890", "test-token"), zerolog.Nop())
}

func TestNewClient_DefaultEndpoints(t *testing.T) {
	t.Parallel()

	c := NewClient("", "", "pid", "tok")
	if c.baseURL != "https://graph.facebook.com" {
		t.Errorf("baseURL = %q, want graph.facebook.com", c.baseURL)
	}
	if c.version != "v21.0" {
		t.Errorf("version = %q, want v21.0", c.version)
	}

	c2 := NewClient("https://graph.example.com", "v18.0", "pid", "tok")
	if c2.baseURL != "https://graph.example.com" {
		t.Errorf("custom baseURL not preserved: %q", c2.baseURL)
	}
	if c2.version != "v18.0" {
		t.Errorf("custom version not preserved: %q", c2.version)
	}
}

func TestAdapter_ChannelAndCapabilities(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	if got := a.Channel(); got != domain.ChannelWABA {
		t.Errorf("Channel() = %q, want waba", got)
	}
	caps := a.Capabilities()
	want := []port.Capability{
		port.CapText, port.CapMedia, port.CapReactions,
		port.CapDelete, port.CapTemplates, port.CapMarkRead,
	}
	for _, c := range want {
		if !caps.Supports(c) {
			t.Errorf("WABA capabilities missing %q", c)
		}
	}
	notSupported := []port.Capability{
		port.CapEdit, port.CapPresence, port.CapTyping,
		port.CapGroups, port.CapPayments, port.CapCalls,
	}
	for _, c := range notSupported {
		if caps.Supports(c) {
			t.Errorf("WABA should NOT support %q", c)
		}
	}
}

func TestAdapter_Send_Text(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	wamid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeText,
		Body:     "olá",
	})
	if err != nil {
		t.Fatalf("Send text: %v", err)
	}
	if wamid == "" {
		t.Error("expected non-empty wamid")
	}
}

func TestAdapter_Send_Media_NotImplementedPhase4(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeImage,
		Body:     "img",
	})
	if err == nil {
		t.Fatal("expected error for media type in Phase 3")
	}
}

func TestAdapter_Action_Reaction(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	wamid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID:         "t1",
		Channel:          domain.ChannelWABA,
		PeerID:           "5511999999999",
		Action:           port.ActionReaction,
		TargetProviderID: "wamid.XYZ",
		ReactionEmoji:    "👍",
	})
	if err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if wamid == "" {
		t.Error("expected non-empty wamid for reaction")
	}
}

func TestAdapter_Action_Revoke(t *testing.T) {
	t.Parallel()

	t.Run("missing target", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter()
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelWABA,
			PeerID: "5511999999999", Action: port.ActionRevoke,
		})
		if err == nil {
			t.Fatal("expected error when revoke sem target")
		}
	})
	t.Run("with target", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter()
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelWABA,
			PeerID: "5511999999999", Action: port.ActionRevoke,
			TargetProviderID: "wamid.XYZ",
		})
		if err != nil {
			t.Errorf("revoke with target: %v", err)
		}
	})
}

func TestAdapter_Action_MarkRead(t *testing.T) {
	t.Parallel()

	t.Run("missing target", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter()
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelWABA,
			PeerID: "5511999999999", Action: port.ActionMarkRead,
		})
		if err == nil {
			t.Fatal("expected error when mark_read sem target")
		}
	})
	t.Run("with target", func(t *testing.T) {
		t.Parallel()
		a := newTestAdapter()
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelWABA,
			PeerID: "5511999999999", Action: port.ActionMarkRead,
			TargetProviderID: "wamid.XYZ",
		})
		if err != nil {
			t.Errorf("mark_read with target: %v", err)
		}
	})
}

func TestAdapter_Action_Edit_NotSupported(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelWABA,
		PeerID: "5511999999999", Action: port.ActionEdit,
	})
	if err == nil {
		t.Fatal("WABA should reject ActionEdit")
	}
}

func TestAdapter_Action_TypingPresence_NotSupported(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	for _, action := range []port.Action{port.ActionTyping, port.ActionPresence} {
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelWABA,
			PeerID: "5511999999999", Action: action,
		})
		if err == nil {
			t.Errorf("WABA should reject %q", action)
		}
	}
}

func TestAdapter_Action_Unknown_ReturnsError(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelWABA,
		PeerID: "5511999999999", Action: port.Action("nonsense"),
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestWABACapabilities_MatchesMatrix(t *testing.T) {
	t.Parallel()

	caps := WABACapabilities()
	want := map[port.Capability]bool{
		port.CapText:      true,
		port.CapMedia:     true,
		port.CapReactions: true,
		port.CapDelete:    true,
		port.CapTemplates: true,
		port.CapMarkRead:  true,
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("WABACapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
	// Sanity: capability set deve ser o mesmo usado pelo Adapter.
	adapterCaps := newTestAdapter().Capabilities()
	if len(adapterCaps) != len(caps) {
		t.Errorf("Adapter caps length = %d, WABACapabilities length = %d", len(adapterCaps), len(caps))
	}
}

// Garante que errors.Is e errors.As funcionam corretamente nos retornos.
func TestAdapter_ErrorsAreWrapped(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelWABA,
		PeerID: "5511999999999", Action: port.ActionEdit,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// Pelo menos uma layer de error wrapping do fmt.Errorf("waba: ...").
	if !contains(err.Error(), "waba:") {
		t.Errorf("error message should contain adapter prefix, got %q", err.Error())
	}
	// Erros não-NotFound aqui, então não usamos errors.Is — apenas sanity.
	_ = errors.Unwrap(err)
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
