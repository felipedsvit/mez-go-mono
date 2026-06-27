// Package instagram — testes do adapter Instagram Direct.
//
// Cobre a matriz Send + Actions (D6):
//   - text message → mid (via httptest mock da Graph API)
//   - image/video/audio/document → attachment payload
//   - reaction → reaction payload
//   - mark_read → no-op silencioso (IG não tem read endpoint)
//   - edit/revoke → erro (API não suporta)
//   - typing/presence → erro
//   - action desconhecida → erro
//   - Capabilities: text/media/reactions/story_reply.
//   - ThreadStore: get/set/apply.
package instagram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// mockGraph é um servidor HTTP que simula a Graph API do Instagram.
type mockGraph struct {
	*httptest.Server
	lastBody    atomic.Value
	lastPath    atomic.Value
	lastMethod  atomic.Value
	failNext    *int
	uploadID    string
}

func newMockGraph(t *testing.T) *mockGraph {
	t.Helper()
	m := &mockGraph{uploadID: "att-123"}
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
		_, _ = w.Write([]byte(`{"recipient_id":"IGSID-XYZ","message_id":"mid.MOCK"}`))
	}))
	t.Cleanup(m.Close)
	return m
}

func newTestAdapterWithMock(t *testing.T) (*Adapter, *mockGraph) {
	t.Helper()
	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", PageID: "page-1", Token: "tok"})
	return New(domain.TenantID("t1"), c, zerolog.Nop()), mg
}

func newTestAdapter() *Adapter {
	return New(domain.TenantID("t1"), NewClient(ClientConfig{PageID: "page-1", Token: "tok"}), zerolog.Nop())
}

func TestNewClient_Defaults(t *testing.T) {
	t.Parallel()

	c := NewClient(ClientConfig{PageID: "p1", Token: "tok"})
	if c.baseURL != "https://graph.facebook.com" {
		t.Errorf("baseURL default missing")
	}
	if c.version != "v21.0" {
		t.Errorf("version default missing")
	}
	if c.pageID != "p1" {
		t.Errorf("pageID = %q, want p1", c.pageID)
	}

	c2 := NewClient(ClientConfig{Token: "tok"})
	if c2.pageID != "me" {
		t.Errorf("pageID default = %q, want me", c2.pageID)
	}
}

func TestAdapter_ChannelAndCapabilities(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	if got := a.Channel(); got != domain.ChannelIG {
		t.Errorf("Channel() = %q, want instagram", got)
	}
	caps := a.Capabilities()
	for _, c := range []port.Capability{port.CapText, port.CapMedia, port.CapReactions, port.CapStoryReply} {
		if !caps.Supports(c) {
			t.Errorf("IG should support %q", c)
		}
	}
	for _, c := range []port.Capability{
		port.CapEdit, port.CapDelete, port.CapTyping, port.CapPresence,
		port.CapGroups, port.CapPayments, port.CapTyping, port.CapEdit, port.CapHandover,
	} {
		if caps.Supports(c) {
			t.Errorf("IG should NOT support %q", c)
		}
	}
}

func TestAdapter_Send_Text(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
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
	if mid != "mid.MOCK" {
		t.Errorf("mid = %q, want mid.MOCK", mid)
	}
	if mg.lastPath.Load() != "/v21.0/page-1/messages" {
		t.Errorf("path = %v", mg.lastPath.Load())
	}
}

func TestAdapter_Send_Image(t *testing.T) {
	t.Parallel()

	a, mg := newTestAdapterWithMock(t)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Type: domain.MessageTypeImage, Body: "olha",
		Metadata: map[string]any{"media_url": "https://example.com/x.png"},
	})
	if err != nil {
		t.Fatalf("Send image: %v", err)
	}
	// Verifica que o body contém o attachment type=image.
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
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Type: domain.MessageType("weird"),
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
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Type: domain.MessageTypeText, Body: "olá",
	})
	if err == nil {
		t.Fatal("expected error from graph api")
	}
}

func TestAdapter_Action_Reaction(t *testing.T) {
	t.Parallel()

	a, _ := newTestAdapterWithMock(t)
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
	if mid != "mid.MOCK" {
		t.Errorf("mid = %q, want mid.MOCK", mid)
	}
}

func TestAdapter_Action_Reaction_RequiresTarget(t *testing.T) {
	t.Parallel()

	a := newTestAdapter()
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelIG,
		PeerID: "IGSID-XYZ", Action: port.ActionReaction, ReactionEmoji: "❤️",
	})
	if err == nil {
		t.Fatal("expected error when target_provider_id missing")
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
		port.CapText:       true,
		port.CapMedia:      true,
		port.CapReactions:  true,
		port.CapStoryReply: true,
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("InstagramCapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
}

// --- ThreadStore (Handover) ---

func TestThreadStore_ApplyGetSet(t *testing.T) {
	t.Parallel()

	s := NewThreadStore()
	ev := HandoverEvent{
		TenantID: domain.TenantID("t1"),
		PeerID:   "IGSID-1",
		Owner:    OwnerPageInbox,
		AppID:    pageInboxAppID,
	}
	s.Apply(ev)

	got, ok := s.Get(domain.TenantID("t1"), "IGSID-1")
	if !ok {
		t.Fatal("expected to find config")
	}
	if got.Owner != OwnerPageInbox {
		t.Errorf("owner = %q", got.Owner)
	}
	if got.OwnerAppID != pageInboxAppID {
		t.Errorf("appID = %q", got.OwnerAppID)
	}
	if time.Since(got.UpdatedAt) > 5*time.Second {
		t.Errorf("updatedAt not recent: %v", got.UpdatedAt)
	}
}

func TestThreadStore_Overwrite(t *testing.T) {
	t.Parallel()

	s := NewThreadStore()
	s.Set(domain.TenantID("t1"), "p1", OwnerApp, "app-1")
	s.Set(domain.TenantID("t1"), "p1", OwnerPageInbox, pageInboxAppID)
	got, _ := s.Get(domain.TenantID("t1"), "p1")
	if got.Owner != OwnerPageInbox {
		t.Errorf("owner = %q, want page_inbox", got.Owner)
	}
}

func TestThreadStore_Missing(t *testing.T) {
	t.Parallel()

	s := NewThreadStore()
	if _, ok := s.Get(domain.TenantID("t1"), "nope"); ok {
		t.Error("expected missing to return ok=false")
	}
}

func TestOwnerFromAppID(t *testing.T) {
	t.Parallel()

	if got := ownerFromAppID(pageInboxAppID); got != OwnerPageInbox {
		t.Errorf("pageInboxAppID: owner = %q", got)
	}
	if got := ownerFromAppID("other-app"); got != OwnerSecondary {
		t.Errorf("other app: owner = %q, want secondary", got)
	}
}

// --- Handover via mock ---

func TestClient_PassTakeRequestThreadControl(t *testing.T) {
	t.Parallel()

	mg := newMockGraph(t)
	c := NewClient(ClientConfig{BaseURL: mg.URL, Version: "v21.0", Token: "tok"})

	if err := c.PassThreadControl(context.Background(), "IGSID-1", pageInboxAppID, ""); err != nil {
		t.Fatalf("PassThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/pass_thread_control" {
		t.Errorf("PassThreadControl path = %v", got)
	}

	if err := c.TakeThreadControl(context.Background(), "IGSID-1", ""); err != nil {
		t.Fatalf("TakeThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/take_thread_control" {
		t.Errorf("TakeThreadControl path = %v", got)
	}

	if err := c.RequestThreadControl(context.Background(), "IGSID-1", ""); err != nil {
		t.Fatalf("RequestThreadControl: %v", err)
	}
	if got := mg.lastPath.Load(); got != "/v21.0/me/request_thread_control" {
		t.Errorf("RequestThreadControl path = %v", got)
	}
}

func TestClient_UploadOwnedMedia_OK(t *testing.T) {
	t.Parallel()

	// Server que devolve attachment_id para POST /owned_media.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v21.0/page-1/owned_media" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"att-999"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(ClientConfig{BaseURL: srv.URL, Version: "v21.0", PageID: "page-1", Token: "tok"})
	id, err := c.UploadOwnedMedia(context.Background(), "image", []byte("PNG-BYTES"), "x.png")
	if err != nil {
		t.Fatalf("UploadOwnedMedia: %v", err)
	}
	if id != "att-999" {
		t.Errorf("id = %q", id)
	}
}
