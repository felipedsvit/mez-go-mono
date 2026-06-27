package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/rs/zerolog"
)

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
	secret string
	err    error
}

func (f *fakeSecrets) ResolveTelegramSecret(_ context.Context, _ domain.TenantID) (string, error) {
	return f.secret, f.err
}

func TestHandler_HappyPath(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: "tg-secret-abc"}
	log := zerolog.Nop()

	h := New(ing, sec, Config{}, log)

	body := `{"update_id":12345,"message":{"message_id":42,"from":{"id":99,"first_name":"Joao"},"chat":{"id":42,"type":"private"},"text":"oi","date":1700000000}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/tenant-1", strings.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "tg-secret-abc")
	req.SetPathValue("tenant_id", "tenant-1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !ing.called {
		t.Fatal("ingestor not called")
	}
	if ing.got.TenantID != "tenant-1" {
		t.Errorf("tenant = %q", ing.got.TenantID)
	}
	if ing.got.Channel != event.ChannelTGBot {
		t.Errorf("channel = %q, want telegram_bot", ing.got.Channel)
	}
}

func TestHandler_RejectsBadSecret(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: "tg-secret-abc"}
	log := zerolog.Nop()
	h := New(ing, sec, Config{}, log)

	body := `{"update_id":1,"message":{"message_id":1,"chat":{"id":1,"type":"private"},"text":"x","date":1}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/tenant-1", strings.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	req.SetPathValue("tenant_id", "tenant-1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if ing.called {
		t.Error("ingestor should not be called")
	}
}

func TestHandler_RejectsMissingSecret(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: "tg-secret-abc"}
	log := zerolog.Nop()
	h := New(ing, sec, Config{}, log)

	body := `{}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/tenant-1", strings.NewReader(body))
	req.SetPathValue("tenant_id", "tenant-1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestHandler_SecretNotConfigured(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{err: errNotConfigured}
	log := zerolog.Nop()
	h := New(ing, sec, Config{}, log)

	body := `{}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/tenant-1", strings.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "anything")
	req.SetPathValue("tenant_id", "tenant-1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandler_EmptyUpdateReturns200(t *testing.T) {
	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: "tg-secret-abc"}
	log := zerolog.Nop()
	h := New(ing, sec, Config{}, log)

	body := `{"update_id":1}` // sem message
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram/tenant-1", strings.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "tg-secret-abc")
	req.SetPathValue("tenant_id", "tenant-1")

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ing.called {
		t.Error("ingestor should not be called for empty update")
	}
}

var errNotConfigured = &configErr{}

// configErr é um erro stub.
type configErr struct{}

func (c *configErr) Error() string { return "not configured" }
