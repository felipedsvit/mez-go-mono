// Package messenger — testes do adapter Facebook Messenger.
//
// Cobre a matriz Send + Actions (D6) + persistent menu + handover.
//   - text message → mid (via httptest mock da Graph API)
//   - image/video/audio/document → attachment payload
//   - reaction → no-op (log)
//   - mark_read → sender_action mark_seen
//   - typing → sender_action typing_on/typing_off
//   - edit/revoke/presence → erro (MSG não suporta)
//   - action desconhecida → erro
//   - Capabilities: text/media/reactions/mark_read/typing/groups/persistent_menu.
package messenger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// mockGraph é um servidor HTTP que simula a Send API do Messenger.
type mockGraph struct {
	*httptest.Server
	lastBody   atomic.Value
	lastPath   atomic.Value
	lastMethod atomic.Value
	failNext   *int
}

func newMockGraph(t *testing.T) *mockGraph {
	t.Helper()
	m := &mockGraph{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.lastPath.Store(r.URL.Path)
		m.lastMethod.Store(r.Method)
		raw, _ := io.ReadAll(r.Body)
		m.lastBody.Store(raw)
		if m.failNext != nil {
			w.WriteHeader(*m.failNext)
			_, _ = w.Write([]byte(`{"error":{"message":"err","code":1}}`))
			m.failNext = nil
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"recipient_id":"PSID-XYZ","message_id":"mid.MOCK"}`))
	}))
	t.Cleanup(m.Close)
	return m
}

func newTestAdapterWithMock(t *testing.T) (*Adapter, *mockGraph) {
	t.Helper()
	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", Token: "tok"})
	return New(domain.TenantID("t1"), c, zerolog.Nop()), mg
}

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient(ClientConfig{Token: "tok"}), zerolog.Nop())
}

func TestNewClient_Defaults(t *testing.T) {
	t.Parallel()

	c := NewClient(ClientConfig{Token: "tok"})
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

	a, mg := newTestAdapterWithMock(t)
	mid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Type: domain.MessageTypeText, Body: "olá",
	})
	if err != nil {
		t.Fatalf("Send text: %v", err)
	}
	if mid != "mid.MOCK" {
		t.Errorf("mid = %q, want mid.MOCK", mid)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/messages" {
		t.Errorf("path = %v", got)
	}
}

func TestAdapter_Send_Image(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Type: domain.MessageTypeImage, Body: "olha",
		Metadata: map[string]any{"media_url": "https://example.com/x.png"},
	})
	if err != nil {
		t.Fatalf("Send image: %v", err)
	}
	body := mg.lastBody.Load().([]byte)
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msg, _ := parsed["message"].(map[string]any)
	att, _ := msg["attachment"].(map[string]any)
	if att["type"] != "image" {
		t.Errorf("attachment.type = %v, want image", att["type"])
	}
}

func TestAdapter_Send_UnsupportedType(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Type: domain.MessageType("weird"),
	})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestAdapter_Send_GraphError(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	status := 401
	mg.failNext = &status
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Type: domain.MessageTypeText, Body: "olá",
	})
	if err == nil {
		t.Fatal("expected error from graph api")
	}
}

func TestAdapter_Action_Reaction_Stub(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.ActionReaction,
		TargetProviderID: "mid.1", ReactionEmoji: "👍",
	})
	if err != nil {
		t.Errorf("reaction stub: %v", err)
	}
}

func TestAdapter_Action_MarkRead_SenderAction(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.ActionMarkRead,
	})
	if err != nil {
		t.Fatalf("mark_read: %v", err)
	}
	body := mg.lastBody.Load().([]byte)
	if !contains(string(body), `"sender_action":"mark_seen"`) {
		t.Errorf("body = %s, want mark_seen sender_action", body)
	}
}

func TestAdapter_Action_Typing_DefaultOn(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.ActionTyping,
	})
	if err != nil {
		t.Fatalf("typing: %v", err)
	}
	body := mg.lastBody.Load().([]byte)
	if !contains(string(body), `"sender_action":"typing_on"`) {
		t.Errorf("body = %s, want typing_on sender_action", body)
	}
}

func TestAdapter_Action_Typing_Off(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelMSG,
		PeerID: "PSID-XYZ", Action: port.ActionTyping, State: "off",
	})
	if err != nil {
		t.Fatalf("typing off: %v", err)
	}
	body := mg.lastBody.Load().([]byte)
	if !contains(string(body), `"sender_action":"typing_off"`) {
		t.Errorf("body = %s, want typing_off sender_action", body)
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

// --- Persistent menu (httptest) ---

func TestAdapter_SetGetDeletePersistentMenu(t *testing.T) {
	t.Parallel()

	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", Token: "tok"})
	a := New(domain.TenantID("t1"), c, zerolog.Nop())

	items := []PersistentMenuItem{
		{Type: "postback", Title: "Help", Payload: "HELP"},
		{Type: "web_url", Title: "Web", URL: "https://example.com"},
	}
	if err := a.SetPersistentMenu(context.Background(), "default", items, false); err != nil {
		t.Fatalf("SetPersistentMenu: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/messenger_profile" {
		t.Errorf("SetPersistentMenu path = %v", got)
	}

	if _, err := a.GetPersistentMenu(context.Background()); err != nil {
		t.Fatalf("GetPersistentMenu: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/messenger_profile" {
		t.Errorf("GetPersistentMenu path = %v", got)
	}

	if err := a.DeletePersistentMenu(context.Background()); err != nil {
		t.Fatalf("DeletePersistentMenu: %v", err)
	}
}

// --- Handover (httptest) ---

func TestAdapter_Handover(t *testing.T) {
	t.Parallel()

	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", Token: "tok"})
	a := New(domain.TenantID("t1"), c, zerolog.Nop())

	if err := a.PassThreadControl(context.Background(), "PSID-1", "app-X", ""); err != nil {
		t.Fatalf("PassThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/pass_thread_control" {
		t.Errorf("PassThreadControl path = %v", got)
	}

	if err := a.TakeThreadControl(context.Background(), "PSID-1", ""); err != nil {
		t.Fatalf("TakeThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/take_thread_control" {
		t.Errorf("TakeThreadControl path = %v", got)
	}

	if err := a.RequestThreadControl(context.Background(), "PSID-1", ""); err != nil {
		t.Fatalf("RequestThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/request_thread_control" {
		t.Errorf("RequestThreadControl path = %v", got)
	}
}

// --- Helpers ---

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		indexOfStr(haystack, needle) >= 0)
}

func indexOfStr(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
