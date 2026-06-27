// Package telegram implementa o webhook handler do Telegram Bot para o
// mez-go-mono (#41).
//
// O Telegram usa o header `X-Telegram-Bot-Api-Secret-Token` (configurável
// no bot via setWebhook) em vez de HMAC. Validação por constant-time
// compare. Fail-closed: sem secret configurado → 503; sem match → 403.
package telegram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
)

// Ingestor é o port que o handler chama.
type Ingestor interface {
	Ingest(ctx context.Context, evt event.InboundEvent) (domain.MessageID, error)
}

// SecretResolver resolve o secret token do bot Telegram. Retorna
// ErrNotConfigured se não houver.
type SecretResolver interface {
	ResolveTelegramSecret(ctx context.Context, tenantID domain.TenantID) (secret string, err error)
}

// Config configura o handler.
type Config struct {
	MaxBodyBytes int64 // default 1MiB (Telegram é menor que Meta)
}

// Handler é o http.Handler para /webhooks/telegram/:tenant_id.
type Handler struct {
	ingestor Ingestor
	secrets  SecretResolver
	cfg      Config
	log      zerolog.Logger
}

// New cria o handler.
func New(ing Ingestor, sec SecretResolver, cfg Config, log zerolog.Logger) *Handler {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 1 << 20 // 1 MiB
	}
	return &Handler{ingestor: ing, secrets: sec, cfg: cfg, log: log}
}

// ServeHTTP implementa http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tenantID := r.PathValue("tenant_id")
	if tenantID == "" {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}

	// Lê body com limite.
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Resolve secret. Fail-closed.
	expected, err := h.secrets.ResolveTelegramSecret(r.Context(), domain.TenantID(tenantID))
	if err != nil {
		h.log.Warn().Err(err).Str("tenant", tenantID).Msg("telegram webhook: secret not configured")
		http.Error(w, "secret not configured", http.StatusServiceUnavailable)
		return
	}

	// Verifica Secret-Token. Fail-closed: ausente ou inválido → 403.
	got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if got == "" {
		h.log.Warn().Str("tenant", tenantID).Msg("telegram webhook: missing secret token")
		http.Error(w, "secret token required", http.StatusForbidden)
		return
	}
	if !validSecretToken(expected, got) {
		h.log.Warn().Str("tenant", tenantID).Msg("telegram webhook: invalid secret token")
		http.Error(w, "invalid secret token", http.StatusForbidden)
		return
	}

	// Parse Update.
	var upd TelegramUpdate
	if err := json.Unmarshal(body, &upd); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Mapeia para InboundEvent.
	evt, ok := upd.ToInboundEvent(domain.TenantID(tenantID))
	if !ok {
		// Updates sem mensagem (edited_message, callback_query puro, etc.)
		// retornam 200 sem ingerir.
		w.WriteHeader(http.StatusOK)
		return
	}

	if _, err := h.ingestor.Ingest(r.Context(), evt); err != nil {
		h.log.Error().Err(err).Msg("telegram webhook: ingest failed")
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// validSecretToken compara com constant-time.
func validSecretToken(expected, got string) bool {
	if len(expected) == 0 || len(got) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// TelegramUpdate é a forma simplificada do Update do Telegram Bot API.
// Apenas os campos necessários para a Fase 2 estão declarados.
type TelegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		MessageID int64 `json:"message_id"`
		From      *struct {
			ID        int64  `json:"id"`
			FirstName string `json:"first_name"`
			Username  string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
		Date int64  `json:"date"`
	} `json:"message"`
	EditedMessage *struct {
		MessageID int64 `json:"message_id"`
	} `json:"edited_message"`
	CallbackQuery *struct {
		ID   string `json:"id"`
		Data string `json:"data"`
	} `json:"callback_query"`
}

// ToInboundEvent converte TelegramUpdate em event.InboundEvent.
// Retorna ok=false se o update não tiver uma mensagem processável.
func (u *TelegramUpdate) ToInboundEvent(tenantID domain.TenantID) (event.InboundEvent, bool) {
	if u.Message == nil {
		return event.InboundEvent{}, false
	}
	// ID sintético estável: chat_id + message_id garante dedup.
	id := "tg:" + intToStr(u.Message.Chat.ID) + ":" + intToStr(u.Message.MessageID)
	return event.InboundEvent{
		TenantID:  string(tenantID),
		Channel:   event.ChannelTGBot,
		MessageID: id,
	}, true
}

func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
