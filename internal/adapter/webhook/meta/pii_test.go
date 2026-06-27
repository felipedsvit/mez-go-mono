package meta

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/rs/zerolog"
)

// TestHandler_DoesNotLogBodyOnInvalidJSON garante que o body (PII) nunca
// aparece nos logs do handler quando o JSON é inválido. Issue #136, audit C8.
func TestHandler_DoesNotLogBodyOnInvalidJSON(t *testing.T) {
	var logBuf bytes.Buffer
	log := zerolog.New(&logBuf).Level(zerolog.WarnLevel)

	ing := &fakeIngestor{}
	sec := &fakeSecrets{secret: []byte("k")}
	chs := &fakeChannels{channel: domain.ChannelWABA, tenant: domain.TenantID("t1")}

	h := New(ing, sec, chs, Config{}, log)

	// Body com JSON inválido (vai cair no caminho de erro que antes logava body).
	invalidBody := []byte(`not a json — contains PII: phone=+5511999 and text="secret msg"`)

	// Calcula uma assinatura HMAC válida (assim passa o CheckOrigin).
	sig := sign([]byte("k"), invalidBody)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/meta/app1", bytes.NewReader(invalidBody))
	req.SetPathValue("app_id", "app1")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (rec=%q)", rec.Code, rec.Body.String())
	}

	logged := logBuf.String()
	if logged == "" {
		t.Fatalf("log buffer vazio — handler não logou nada (rec=%q)", rec.Body.String())
	}
	if strings.Contains(logged, "phone=+5511999") {
		t.Errorf("PII vazou no log: %s", logged)
	}
	if strings.Contains(logged, "secret msg") {
		t.Errorf("body content vazou no log: %s", logged)
	}
	// Confirma que body_len foi logado (sinal de que logou *metadado*).
	if !strings.Contains(logged, "body_len") {
		t.Errorf("body_len não apareceu no log (esperado): %s", logged)
	}
}
