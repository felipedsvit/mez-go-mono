package meta

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/rs/zerolog"
)

// ---- fakes ---------------------------------------------------------------

type fakeIngestor struct {
	called bool
	got    event.InboundEvent
}

func (f *fakeIngestor) Ingest(_ context.Context, evt event.InboundEvent) (domain.MessageID, error) {
	f.called = true
	f.got = evt
	return "msg-1", nil
}

type fakeSecrets struct {
	secret []byte
}

func (f *fakeSecrets) ResolveMetaSecret(_ context.Context, _ domain.TenantID, _ domain.Channel, _ string) ([]byte, error) {
	return f.secret, nil
}

type fakeChannels struct {
	channel domain.Channel
	tenant  domain.TenantID
}

func (f *fakeChannels) ResolveChannel(_ string) (domain.Channel, domain.TenantID, error) {
	return f.channel, f.tenant, nil
}

// ---- tests ---------------------------------------------------------------

func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidMetaSignature_AcceptsCorrectSignature(t *testing.T) {
	secret := []byte("app-secret")
	body := []byte(`{"hello":"world"}`)

	if !validMetaSignature(secret, body, sign(secret, body)) {
		t.Fatal("valid signature rejected")
	}
}

func TestValidMetaSignature_RejectsBadSignature(t *testing.T) {
	secret := []byte("app-secret")
	body := []byte(`{"hello":"world"}`)
	badSecret := []byte("wrong-secret")

	if validMetaSignature(secret, body, sign(badSecret, body)) {
		t.Fatal("bad signature accepted")
	}
}

func TestValidMetaSignature_RejectsMissingPrefix(t *testing.T) {
	secret := []byte("s")
	body := []byte(`x`)
	if validMetaSignature(secret, body, "deadbeef") {
		t.Fatal("missing prefix should reject")
	}
}

func TestHandler_HappyPath(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelWABA, tenant: "tenant-1"}
	log := zerolog.Nop()

	h := New(ing, sec, ch, Config{}, log)

	body := `{"object":"whatsapp_business_account","entry":[{"id":"E1","messaging":[{"sender":{"id":"P1"},"recipient":{"id":"B1"},"timestamp":1700000000,"message":{"mid":"m1","text":{"body":"hi"}}}]}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign([]byte("s"), []byte(body)))
	req.SetPathValue("app_id", "app1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !ing.called {
		t.Fatal("ingestor not called")
	}
	if ing.got.TenantID != "tenant-1" {
		t.Errorf("tenant = %q, want tenant-1", ing.got.TenantID)
	}
	if ing.got.MessageID != "m1" {
		t.Errorf("message_id = %q, want m1", ing.got.MessageID)
	}
}

func TestHandler_RejectsBadSignature(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelWABA, tenant: "tenant-1"}
	log := zerolog.Nop()

	h := New(ing, sec, ch, Config{}, log)

	body := `{"entry":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	req.SetPathValue("app_id", "app1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if ing.called {
		t.Error("ingestor should not be called")
	}
}

func TestHandler_RejectsMissingSignature(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelWABA, tenant: "tenant-1"}
	log := zerolog.Nop()

	h := New(ing, sec, ch, Config{}, log)

	body := `{"entry":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", strings.NewReader(body))
	req.SetPathValue("app_id", "app1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_RejectsBodyTooLarge(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelWABA, tenant: "tenant-1"}
	log := zerolog.Nop()

	h := New(ing, sec, ch, Config{MaxBodyBytes: 100}, log)

	body := strings.Repeat("A", 200)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", strings.NewReader(body))
	req.SetPathValue("app_id", "app1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestHandler_RejectsMethodNotAllowed(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{}
	log := zerolog.Nop()
	h := New(ing, sec, ch, Config{}, log)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/webhooks/meta/app1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandler_Returns200_OnEmptyPayload(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelWABA, tenant: "tenant-1"}
	log := zerolog.Nop()

	h := New(ing, sec, ch, Config{}, log)

	body := `{"object":"whatsapp_business_account","entry":[]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sign([]byte("s"), []byte(body)))
	req.SetPathValue("app_id", "app1")

	h.ServeHTTP(rec, req)

	// Sem mensagens, mas válido → 200 (Meta espera 200 para acks de delivery/read).
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ing.called {
		t.Error("ingestor should not be called for empty payload")
	}
}

// Smoke test that validMetaSignature + ToInboundEvent work together.
func TestHandler_EndToEnd_BodyIsReadCorrectly(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("s")}
	ch := &fakeChannels{channel: domain.ChannelIG, tenant: "tenant-2"}
	log := zerolog.Nop()
	h := New(ing, sec, ch, Config{}, log)

	body := `{"object":"instagram","entry":[{"id":"E2","messaging":[{"sender":{"id":"P2"},"recipient":{"id":"B2"},"timestamp":1700000000,"message":{"mid":"m2","text":{"body":"ola"}}}]}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app2", io.NopCloser(strings.NewReader(body)))
	req.Header.Set("X-Hub-Signature-256", sign([]byte("s"), []byte(body)))
	req.SetPathValue("app_id", "app2")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !ing.called {
		t.Fatal("ingestor not called")
	}
	if ing.got.Channel != event.ChannelIG {
		t.Errorf("channel = %q, want instagram", ing.got.Channel)
	}
}
