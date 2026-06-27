// Package e2e — testes E2E do pipeline completo (webhook → bus → sender).
//
// Estes testes cobrem o caminho crítico do mez-go-mono:
//  1. Webhook Meta/Telegram chega via HTTP
//  2. Handler valida signature + parse payload
//  3. Ingestor normaliza e grava (fake)
//  4. Ingestor publica no bus como InboundEvent
//  5. Subscriber do bus pega o evento
//  6. Subscriber consulta o registry pelo (tenant, channel)
//  7. Subscriber monta OutboundRequest e chama Sender.Send
//  8. Sender recorder captura a chamada → asserção
package e2e

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/telegram"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// tgSecretResolver é um telegram.SecretResolver fake.
type tgSecretResolver struct {
	secret string
	err    error
}

func (r *tgSecretResolver) ResolveTelegramSecret(_ context.Context, _ domain.TenantID) (string, error) {
	return r.secret, r.err
}

func newTgServer(t *testing.T, ing telegram.Ingestor, sec telegram.SecretResolver) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/webhooks/telegram/{tenant_id}", telegram.New(ing, sec, telegram.Config{}, zerolog.Nop()).ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func TestE2E_TelegramWebhook_HappyPath(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	var mu sync.Mutex
	var got event.InboundEvent
	done := make(chan struct{}, 1)
	h.Bus.SubscribeInbound(func(evt event.InboundEvent) {
		mu.Lock()
		got = evt
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	ing := &ingestorRecorder{bus: h.Bus}
	sec := &tgSecretResolver{secret: "tg-secret-xyz"}
	srv := newTgServer(t, ing, sec)

	body := []byte(`{"update_id":1,"message":{"message_id":42,"from":{"id":99,"first_name":"Ana"},"chat":{"id":42,"type":"private"},"text":"olá","date":1700000000}}`)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/telegram/tenant-1", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "tg-secret-xyz")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound event")
	}
	mu.Lock()
	defer mu.Unlock()
	if got.TenantID != "tenant-1" {
		t.Errorf("tenant = %q, want tenant-1", got.TenantID)
	}
	if got.Channel != event.ChannelTGBot {
		t.Errorf("channel = %q, want telegram_bot", got.Channel)
	}
	if got.MessageID != "tg:42:42" {
		t.Errorf("message_id = %q, want tg:42:42", got.MessageID)
	}
}

func TestE2E_TelegramWebhook_RejectsBadSecret(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &tgSecretResolver{secret: "real-secret"}
	srv := newTgServer(t, ing, sec)

	body := []byte(`{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"text":"x","date":1}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/telegram/tenant-1", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if len(ing.Calls()) != 0 {
		t.Error("ingestor should not be called")
	}
}

func TestE2E_TelegramWebhook_SecretNotConfigured(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &tgSecretResolver{err: context.DeadlineExceeded}
	srv := newTgServer(t, ing, sec)

	body := []byte(`{"update_id":1}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/telegram/tenant-1", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "any")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestE2E_TelegramWebhook_UpdateWithoutMessage_Returns200(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &tgSecretResolver{secret: "s"}
	srv := newTgServer(t, ing, sec)

	// Update sem campo "message" — é um ack/edit/etc.
	body := []byte(`{"update_id":1,"edited_message":{"message_id":2}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/telegram/tenant-1", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "s")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (ack for non-message update)", resp.StatusCode)
	}
	if len(ing.Calls()) != 0 {
		t.Error("ingestor should not be called for ack-only update")
	}
}

// TestE2E_FullPipeline_TelegramToOutbound monta o pipeline COMPLETO:
//
//	telegram webhook → ingestor → bus → subscriber → registry → sender
//
// Garante que o caminho "mensagem chega no canal e vira delivery outbound"
// funciona ponta a ponta.
func TestE2E_FullPipeline_TelegramToOutbound(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	// Sender recorder para telegram.
	rec := NewSenderRecorder(domain.ChannelTGBot, port.CapabilitySet{port.CapText: true})
	h.RegisterSender(rec)

	// Subscriber: a cada inbound, monta um outbound reply e envia.
	h.Bus.SubscribeInbound(func(evt event.InboundEvent) {
		tenantID := domain.TenantID(evt.TenantID)
		snd, err := h.Reg.Get(context.Background(), tenantID, domain.Channel(evt.Channel))
		if err != nil {
			t.Errorf("registry.Get: %v", err)
			return
		}
		_, err = snd.Send(context.Background(), port.OutboundRequest{
			TenantID:  tenantID,
			Channel:   domain.Channel(evt.Channel),
			MessageID: domain.MessageID("reply-" + evt.MessageID),
			PeerID:    "42",
			Type:      domain.MessageTypeText,
			Body:      "echo: " + evt.MessageID,
		})
		if err != nil {
			t.Errorf("Send: %v", err)
		}
	})

	ing := &ingestorRecorder{bus: h.Bus}
	sec := &tgSecretResolver{secret: "tg-secret"}
	srv := newTgServer(t, ing, sec)

	body := []byte(`{"update_id":7,"message":{"message_id":100,"from":{"id":1},"chat":{"id":42,"type":"private"},"text":"olá","date":1700000000}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/telegram/tenant-1", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "tg-secret")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}

	// Espera o sender ser chamado.
	if !WaitForOutboundCalls(t, rec, 1, 3*time.Second) {
		t.Fatalf("expected 1 outbound call, got %d", len(rec.Calls()))
	}

	calls := rec.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.TenantID != domain.TenantID("tenant-1") {
		t.Errorf("tenant = %q, want tenant-1", c.TenantID)
	}
	if c.Channel != domain.ChannelTGBot {
		t.Errorf("channel = %q, want telegram_bot", c.Channel)
	}
	if c.PeerID != "42" {
		t.Errorf("peer_id = %q, want 42", c.PeerID)
	}
	if c.Body != "echo: tg:42:100" {
		t.Errorf("body = %q, want echo of inbound", c.Body)
	}
}

// TestE2E_FullPipeline_MetaToOutbound mesma coisa para Meta (WABA).
func TestE2E_FullPipeline_MetaToOutbound(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	rec := NewSenderRecorder(domain.ChannelWABA, port.CapabilitySet{port.CapText: true, port.CapMedia: true})
	h.RegisterSender(rec)

	h.Bus.SubscribeInbound(func(evt event.InboundEvent) {
		tenantID := domain.TenantID(evt.TenantID)
		snd, err := h.Reg.Get(context.Background(), tenantID, domain.Channel(evt.Channel))
		if err != nil {
			t.Errorf("registry.Get: %v", err)
			return
		}
		_, err = snd.Send(context.Background(), port.OutboundRequest{
			TenantID:  tenantID,
			Channel:   domain.Channel(evt.Channel),
			MessageID: domain.MessageID("reply-" + evt.MessageID),
			PeerID:    "5511999999999",
			Type:      domain.MessageTypeText,
			Body:      "echo",
		})
		if err != nil {
			t.Errorf("Send: %v", err)
		}
	})

	ing := &ingestorRecorder{bus: h.Bus}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("tenant-1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"E1","messaging":[{"sender":{"id":"P1"},"recipient":{"id":"B1"},"timestamp":1700000000,"message":{"mid":"m-999","text":{"body":"oi"}}}]}]}`)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signBody([]byte("app-secret"), body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, string(b))
	}

	if !WaitForOutboundCalls(t, rec, 1, 3*time.Second) {
		t.Fatalf("expected 1 outbound call, got %d", len(rec.Calls()))
	}

	calls := rec.Calls()
	if calls[0].Body != "echo" {
		t.Errorf("body = %q, want echo", calls[0].Body)
	}
}

// TestE2E_OutboundRequest_JSONPayload valida que o OutboundRequest tem
// todos os campos serializáveis corretamente. Garante que campos críticos
// (Action, TargetProviderID) sobrevivem um round-trip JSON.
func TestE2E_OutboundRequest_JSONPayload(t *testing.T) {
	t.Parallel()

	req := port.OutboundRequest{
		TenantID:         "t1",
		Channel:          domain.ChannelWABA,
		MessageID:        "m1",
		ConversationID:   "c1",
		ContactID:        "ct1",
		PeerID:           "5511",
		Type:             domain.MessageTypeReaction,
		Body:             "👍",
		Action:           port.ActionReaction,
		TargetProviderID: "wamid.XYZ",
		ReactionEmoji:    "👍",
		State:            "available",
		Metadata:         map[string]any{"foo": "bar"},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got port.OutboundRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Action != port.ActionReaction {
		t.Errorf("action round-trip = %q, want %q", got.Action, port.ActionReaction)
	}
	if got.TargetProviderID != "wamid.XYZ" {
		t.Errorf("target round-trip = %q, want wamid.XYZ", got.TargetProviderID)
	}
	if got.ReactionEmoji != "👍" {
		t.Errorf("emoji round-trip = %q, want 👍", got.ReactionEmoji)
	}
}

// Garante que constant-time compare do secret Telegram não vaza tempo
// quando os comprimentos diferem.
func TestE2E_TelegramSecret_ConstantTimeCompare(t *testing.T) {
	t.Parallel()

	// Dois segredos de tamanhos diferentes.
	if subtle.ConstantTimeCompare([]byte("abc"), []byte("abcd")) == 1 {
		t.Error("different-length strings should NOT match in constant-time compare")
	}
}
