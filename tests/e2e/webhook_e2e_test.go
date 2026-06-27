// Package e2e — testes E2E do pipeline inbound via Meta webhook.
//
// Estes testes montam o handler Meta real, um ingestor fake, e o bus
// in-memory, e exercitam o caminho HTTP completo via httptest.NewServer
// (com router chi para suportar r.PathValue).
package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/meta"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

// ingestorRecorder é um meta.Ingestor que captura todas as chamadas
// e propaga para o bus como InboundEvent.
type ingestorRecorder struct {
	mu   sync.Mutex
	seen []event.InboundEvent
	bus  *broker.Bus
	err  error
}

func (r *ingestorRecorder) Ingest(_ context.Context, evt event.InboundEvent) (domain.MessageID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	r.seen = append(r.seen, evt)
	if r.bus != nil {
		r.bus.PublishInbound(evt)
	}
	return domain.MessageID(evt.MessageID), nil
}

func (r *ingestorRecorder) Calls() []event.InboundEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.InboundEvent, len(r.seen))
	copy(out, r.seen)
	return out
}

type metaSecretResolver struct {
	secret []byte
	err    error
}

func (m *metaSecretResolver) ResolveMetaSecret(_ context.Context, _ domain.TenantID, _ domain.Channel, _ string) ([]byte, error) {
	return m.secret, m.err
}

type metaChannelResolver struct {
	channel domain.Channel
	tenant  domain.TenantID
	err     error
}

func (m *metaChannelResolver) ResolveChannel(_ string) (domain.Channel, domain.TenantID, error) {
	return m.channel, m.tenant, m.err
}

// newMetaServer monta um *httptest.NewServer com o handler Meta real
// montado em um router chi que extrai app_id da URL.
func newMetaServer(t *testing.T, ing meta.Ingestor, sec meta.AppSecretResolver, ch meta.ChannelFromAppID) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/webhooks/meta/{app_id}", meta.New(ing, sec, ch, meta.Config{}, zerolog.Nop()).ServeHTTP)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func signBody(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestE2E_MetaWebhook_HappyPath(t *testing.T) {
	t.Parallel()

	bus := broker.NewBus(broker.BusConfig{InboundBuffer: 16}, zerolog.Nop(), metrics.NewRegistry())
	t.Cleanup(func() { _ = bus.Drain(context.Background()) })

	var (
		gotEvt event.InboundEvent
		mu     sync.Mutex
		done   = make(chan struct{}, 1)
	)
	bus.SubscribeInbound(func(evt event.InboundEvent) {
		mu.Lock()
		gotEvt = evt
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	ing := &ingestorRecorder{bus: bus}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("tenant-1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"E1","messaging":[{"sender":{"id":"P1"},"recipient":{"id":"B1"},"timestamp":1700000000,"message":{"mid":"m-123","text":{"body":"olá"}}}]}]}`)

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

	// Ingestor foi chamado uma vez com o evento certo.
	if len(ing.Calls()) != 1 {
		t.Fatalf("ingestor called %d times, want 1", len(ing.Calls()))
	}
	if ing.Calls()[0].MessageID != "m-123" {
		t.Errorf("message_id = %q, want m-123", ing.Calls()[0].MessageID)
	}

	// Evento chegou no bus.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for bus event")
	}
	mu.Lock()
	defer mu.Unlock()
	if gotEvt.TenantID != "tenant-1" {
		t.Errorf("tenant = %q, want tenant-1", gotEvt.TenantID)
	}
	if gotEvt.Channel != event.ChannelWABA {
		t.Errorf("channel = %q, want waba", gotEvt.Channel)
	}
}

func TestE2E_MetaWebhook_RejectsInvalidSignature(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("real-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("tenant-1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"entry":[]}`)

	// Assinatura com secret errado.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signBody([]byte("wrong-secret"), body))

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

func TestE2E_MetaWebhook_RejectsMissingSignature(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("tenant-1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"entry":[]}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestE2E_MetaWebhook_AppIDNotFound(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{err: errors.New("not configured")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"entry":[]}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/unknown-app", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signBody([]byte("app-secret"), body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestE2E_MetaWebhook_SecretNotConfigured(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{err: errors.New("no secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("t1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{"entry":[]}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestE2E_MetaWebhook_InvalidJSON(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("t1")}
	srv := newMetaServer(t, ing, sec, ch)

	body := []byte(`{ not json `)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signBody([]byte("app-secret"), body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestE2E_MetaWebhook_EmptyPayload_Returns200(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("t1")}
	srv := newMetaServer(t, ing, sec, ch)

	// Payload sem mensagens (Meta manda isso para acks delivery/read).
	body := []byte(`{"object":"whatsapp_business_account","entry":[]}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/meta/app-1", strings.NewReader(string(body)))
	req.Header.Set("X-Hub-Signature-256", signBody([]byte("app-secret"), body))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	// Meta espera 200 mesmo em ack vazio.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (Meta ack)", resp.StatusCode)
	}
	if len(ing.Calls()) != 0 {
		t.Error("ingestor should not be called for empty payload")
	}
}

func TestE2E_MetaWebhook_WrongMethod(t *testing.T) {
	t.Parallel()

	ing := &ingestorRecorder{}
	sec := &metaSecretResolver{secret: []byte("app-secret")}
	ch := &metaChannelResolver{channel: domain.ChannelWABA, tenant: domain.TenantID("t1")}
	srv := newMetaServer(t, ing, sec, ch)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/webhooks/meta/app-1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
