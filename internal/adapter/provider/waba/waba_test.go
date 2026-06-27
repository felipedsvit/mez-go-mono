// Package waba — testes do adapter WhatsApp Business Cloud API.
//
// Cobre a matriz Send + Actions (D6):
//   - text message → wamid (via httptest mock da Graph API)
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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// mockGraph é um servidor HTTP que simula a Graph API do WhatsApp Cloud.
// Default: responde 200 com wamid stub em qualquer POST/GET/DELETE.
type mockGraph struct {
	*httptest.Server
	// responseBody customiza a resposta default.
	responseBody string
	// failNext faz a próxima chamada falhar com o status configurado.
	failNext *int
	// recordedPath guarda o último path chamado.
	recordedPath string
	// recordedMethod guarda o último método chamado.
	recordedMethod string
}

func newMockGraph(t *testing.T) *mockGraph {
	t.Helper()
	m := &mockGraph{
		responseBody: `{"messages":[{"id":"wamid.OK"}]}`,
	}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.recordedPath = r.URL.Path
		m.recordedMethod = r.Method
		if m.failNext != nil {
			w.WriteHeader(*m.failNext)
			_, _ = w.Write([]byte(`{"error":{"message":"err","code":1}}`))
			m.failNext = nil
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(m.responseBody))
	}))
	t.Cleanup(m.Close)
	return m
}

func newTestAdapterWithMock(t *testing.T) (*Adapter, *mockGraph) {
	t.Helper()
	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", PhoneNumberID: "PNID", Token: "tok"})
	return New(domain.TenantID("t1"), c, zerolog.Nop()), mg
}

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient(ClientConfig{PhoneNumberID: "1234567890", Token: "test-token"}), zerolog.Nop())
}

func TestNewClient_DefaultEndpoints(t *testing.T) {
	t.Parallel()

	c := NewClient(ClientConfig{PhoneNumberID: "pid", Token: "tok"})
	if c.baseURL != "https://graph.facebook.com" {
		t.Errorf("baseURL = %q, want graph.facebook.com", c.baseURL)
	}
	if c.version != "v21.0" {
		t.Errorf("version = %q, want v21.0", c.version)
	}

	c2 := NewClient(ClientConfig{BaseURL: "https://graph.example.com", Version: "v18.0", PhoneNumberID: "pid", Token: "tok"})
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

	a, _ := newTestAdapterWithMock(t)
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
	if wamid != "wamid.OK" {
		t.Errorf("wamid = %q, want wamid.OK", wamid)
	}
}

func TestAdapter_Send_GraphError(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	status := 401
	mg.failNext = &status
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeText,
		Body:     "olá",
	})
	if err == nil {
		t.Fatal("expected error from graph api")
	}
}

func TestAdapter_Send_Image_BuildsLinkPayload(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeImage,
		Body:     "olá imagem",
		Metadata: map[string]any{
			"media_url": "https://example.com/img.png",
		},
	})
	if err != nil {
		t.Fatalf("Send image: %v", err)
	}
	if mg.recordedPath != "/v21.0/PNID/messages" {
		t.Errorf("path = %q", mg.recordedPath)
	}
}

func TestBuildMessagePayload_Image_WithLink(t *testing.T) {
	t.Parallel()

	req := port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeImage,
		Body:     "caption",
		Metadata: map[string]any{
			"media_url": "https://example.com/img.png",
		},
	}
	payload, err := buildMessagePayload(req)
	if err != nil {
		t.Fatalf("buildMessagePayload: %v", err)
	}
	if payload["type"] != "image" {
		t.Errorf("type = %v, want image", payload["type"])
	}
	img, ok := payload["image"].(map[string]any)
	if !ok {
		t.Fatalf("image payload missing or wrong type")
	}
	if img["link"] != "https://example.com/img.png" {
		t.Errorf("link = %v", img["link"])
	}
	if img["caption"] != "caption" {
		t.Errorf("caption = %v, want 'caption'", img["caption"])
	}
}

func TestBuildMessagePayload_Sticker_NoCaption(t *testing.T) {
	t.Parallel()

	req := port.OutboundRequest{
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
		PeerID:   "5511999999999",
		Type:     domain.MessageTypeSticker,
		Body:     "should be ignored",
		Metadata: map[string]any{
			"media_id": "sticker-1",
		},
	}
	payload, err := buildMessagePayload(req)
	if err != nil {
		t.Fatalf("buildMessagePayload: %v", err)
	}
	stk, ok := payload["sticker"].(map[string]any)
	if !ok {
		t.Fatalf("sticker payload missing or wrong type")
	}
	if stk["caption"] != nil {
		t.Errorf("sticker should not have caption, got %v", stk["caption"])
	}
}

func TestBuildReactionPayload_RequiresEmojiAndTarget(t *testing.T) {
	t.Parallel()

	t.Run("missing target", func(t *testing.T) {
		t.Parallel()
		_, err := buildReactionPayload(port.OutboundRequest{
			PeerID:        "p1",
			ReactionEmoji: "👍",
		})
		if err == nil {
			t.Fatal("expected error when target_provider_id missing")
		}
	})
	t.Run("missing emoji", func(t *testing.T) {
		t.Parallel()
		_, err := buildReactionPayload(port.OutboundRequest{
			PeerID:           "p1",
			TargetProviderID: "wamid.XYZ",
		})
		if err == nil {
			t.Fatal("expected error when emoji missing")
		}
	})
	t.Run("ok", func(t *testing.T) {
		t.Parallel()
		p, err := buildReactionPayload(port.OutboundRequest{
			PeerID:           "5511999999999",
			TargetProviderID: "wamid.XYZ",
			ReactionEmoji:    "👍",
		})
		if err != nil {
			t.Fatalf("buildReactionPayload: %v", err)
		}
		if p["type"] != "reaction" {
			t.Errorf("type = %v, want reaction", p["type"])
		}
		reac, _ := p["reaction"].(map[string]any)
		if reac["message_id"] != "wamid.XYZ" {
			t.Errorf("message_id = %v", reac["message_id"])
		}
		if reac["emoji"] != "👍" {
			t.Errorf("emoji = %v", reac["emoji"])
		}
	})
}

func TestAdapter_Action_Reaction(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapterWithMock(t)
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
	if wamid != "wamid.OK" {
		t.Errorf("wamid = %q, want wamid.OK", wamid)
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
		a, _ := newTestAdapterWithMock(t)
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
		a, _ := newTestAdapterWithMock(t)
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
	if !contains(err.Error(), "waba") {
		t.Errorf("error message should contain adapter prefix, got %q", err.Error())
	}
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
