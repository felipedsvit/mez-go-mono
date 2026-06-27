// Package meta implementa o webhook handler unificado para canais Meta
// (WABA + Instagram + Messenger) do mez-go-mono (#40).
//
// Roteamento:
//   - X-Hub-Signature-256: HMAC-SHA256 do body com app secret (per-app).
//   - Fail-closed: sem app secret configurado → 503; assinatura inválida → 403.
//   - maxBody = 2MiB (defesa contra payload bombs).
//   - Mappers por canal (WABA/IG/MSG) → event.InboundEvent canônico.
//
// Cada app Meta tem seu próprio app secret. O handler recebe o app_id
// na URL e resolve o secret do banco. Se a credencial não estiver
// cadastrada, retorna 404.
//
// PII safety (issue #136, audit C8): o body nunca é logado; apenas
// \`body_len\`. Full body é gating atrás de MEZ_DEBUG_WEBHOOK_BODY=true
// (dev only). Inbound Meta webhooks carregam PII (text.body, contatos,
// template params) que vazaria em Loki/ELK se logado.
package meta

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
)

// Ingestor é o port que o handler chama. Definado aqui para evitar
// ciclo com usecase/messaging.
type Ingestor interface {
	Ingest(ctx context.Context, evt event.InboundEvent) (domain.MessageID, error)
}

// AppSecretResolver resolve o app secret do canal Meta a partir do
// (channel_credentials). Retorna ErrNotConfigured se não houver.
//
// A descriptografia é responsabilidade do caller (envelope encryption);
// o handler recebe o plaintext via este port.
type AppSecretResolver interface {
	ResolveMetaSecret(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, appID string) (secret []byte, err error)
}

// ChannelFromAppID infere o canal a partir do app_id. Na prática isto
// virá de uma tabela de mapeamento (app_id → channel); aqui simplificamos
// para waba por default. Hook para Fase 5.
type ChannelFromAppID interface {
	ResolveChannel(appID string) (domain.Channel, domain.TenantID, error)
}

// Config configura o handler.
type Config struct {
	MaxBodyBytes int64 // default 2MiB
}

// Handler é o http.Handler para /webhooks/meta/:app_id.
type Handler struct {
	ingestor Ingestor
	secrets  AppSecretResolver
	channels ChannelFromAppID
	cfg      Config
	log      zerolog.Logger
}

// New cria o handler.
func New(ing Ingestor, sec AppSecretResolver, ch ChannelFromAppID, cfg Config, log zerolog.Logger) *Handler {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 2 << 20 // 2 MiB
	}
	return &Handler{ingestor: ing, secrets: sec, channels: ch, cfg: cfg, log: log}
}

// ServeHTTP implementa http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extrai app_id da URL (chi: r.PathValue).
	appID := r.PathValue("app_id")
	if appID == "" {
		http.Error(w, "app_id required", http.StatusBadRequest)
		return
	}

	// Resolve canal e tenant a partir do app_id.
	channel, tenantID, err := h.channels.ResolveChannel(appID)
	if err != nil {
		h.log.Warn().Err(err).Str("app_id", appID).Msg("meta webhook: app_id not found")
		http.Error(w, "app not configured", http.StatusNotFound)
		return
	}

	// Lê body com limite (fail-fast).
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.log.Warn().Int64("limit", h.cfg.MaxBodyBytes).Msg("meta webhook: body too large")
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.log.Error().Err(err).Msg("meta webhook: read body")
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	// Resolve app secret. Fail-closed: sem secret, não processa.
	secret, err := h.secrets.ResolveMetaSecret(r.Context(), tenantID, channel, appID)
	if err != nil {
		h.log.Warn().Err(err).Str("app_id", appID).Msg("meta webhook: secret not configured")
		http.Error(w, "app secret not configured", http.StatusServiceUnavailable)
		return
	}

	// Verifica assinatura. Fail-closed: assinatura ausente ou inválida → 403.
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		h.log.Warn().Str("app_id", appID).Msg("meta webhook: missing signature")
		http.Error(w, "signature required", http.StatusForbidden)
		return
	}
	if !validMetaSignature(secret, body, sig) {
		h.log.Warn().Str("app_id", appID).Msg("meta webhook: invalid signature")
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	// Parse payload canônico. PII safety: log apenas o tamanho do body
	// (nunca o conteúdo). Inbound Meta webhooks carregam PII real
	// (text.body, contatos, template params) — log do body vazaria em
	// Loki/ELK. Issue #136, audit C8.
	var payload MetaPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		ev := h.log.Warn().Err(err).Int("body_len", len(body))
		if os.Getenv("MEZ_DEBUG_WEBHOOK_BODY") == "true" {
			ev = ev.Bytes("body", body)
		}
		ev.Msg("meta webhook: invalid json")
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Mapeia para InboundEvent canônico.
	evt, err := payload.ToInboundEvent(tenantID, channel)
	if err != nil {
		h.log.Warn().Err(err).Str("app_id", appID).Msg("meta webhook: no messages in payload")
		// Mesmo sem mensagens, Meta espera 200 OK (acks de delivery/read).
		w.WriteHeader(http.StatusOK)
		return
	}

	// Ingere.
	if _, err := h.ingestor.Ingest(r.Context(), evt); err != nil {
		h.log.Error().Err(err).Msg("meta webhook: ingest failed")
		// Retornar 500 força Meta a retentar; alternativa é 200 e logar.
		// Para Fase 2, retornamos 200 e logamos — Meta não retenta após 200.
		// A garantia de durabilidade está no reconciler.
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// validMetaSignature verifica HMAC-SHA256 com constant-time compare.
func validMetaSignature(secret, body []byte, sigHeader string) bool {
	const prefix = "sha256="
	if len(sigHeader) < len(prefix) {
		return false
	}
	if sigHeader[:len(prefix)] != prefix {
		return false
	}
	got, err := hex.DecodeString(sigHeader[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := mac.Sum(nil)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// MetaPayload é a forma unificada do payload Meta (WABA/IG/MSG).
// Simplificado: a versão real tem variations por canal; o mapper faz
// o trabalho de normalização por canal.
type MetaPayload struct {
	Object string      `json:"object"`
	Entry  []MetaEntry `json:"entry"`
	// Para IG/MSG, há mais campos; o mapper cuida.
}

// MetaEntry é uma entrada do payload.
type MetaEntry struct {
	ID        string      `json:"id"`
	Time      int64       `json:"time"`
	Messaging []Messaging `json:"messaging,omitempty"`
	Changes   []Change    `json:"changes,omitempty"`
}

// Messaging é o formato WABA/IG.
type Messaging struct {
	Sender    Sender    `json:"sender"`
	Recipient Recipient `json:"recipient"`
	Timestamp int64     `json:"timestamp"`
	Message   *Message  `json:"message,omitempty"`
	Postback  *Postback `json:"postback,omitempty"`
}

// Change é o formato alternativo (Messenger, IG alguns tipos).
type Change struct {
	Field string `json:"field"`
	Value struct {
		From     string `json:"from,omitempty"`
		Text     string `json:"text,omitempty"`
		Item     string `json:"item,omitempty"`
		Reaction string `json:"reaction,omitempty"`
	} `json:"value"`
}

// Sender identifica quem enviou.
type Sender struct {
	ID string `json:"id"`
}

// Recipient identifica quem recebeu.
type Recipient struct {
	ID string `json:"id"`
}

// Message é o conteúdo.
type Message struct {
	Mid  string `json:"mid"`
	Text *struct {
		Body string `json:"body"`
	} `json:"text,omitempty"`
	Type string `json:"type,omitempty"`
}

// Postback é o callback de botão.
type Postback struct {
	Title   string `json:"title"`
	Payload string `json:"payload"`
	Mid     string `json:"mid,omitempty"`
}

// ToInboundEvent converte MetaPayload em event.InboundEvent canônico.
// Retorna erro se não houver mensagens processáveis.
func (p *MetaPayload) ToInboundEvent(tenantID domain.TenantID, channel domain.Channel) (event.InboundEvent, error) {
	for _, entry := range p.Entry {
		// Formato WABA/IG (messaging array).
		for _, m := range entry.Messaging {
			if m.Message == nil {
				continue
			}
			return event.InboundEvent{
				TenantID:  string(tenantID),
				Channel:   event.Channel(channel),
				MessageID: m.Message.Mid,
			}, nil
		}
		// Formato Messenger/IG (changes array).
		for _, ch := range entry.Changes {
			if ch.Field != "messages" {
				continue
			}
			if ch.Value.From == "" {
				continue
			}
			// Para changes, derivamos um ID sintético: from+timestamp.
			// Em produção, viria o mid real.
			id := fmt.Sprintf("change:%s:%d:%s", ch.Value.From, entry.Time, ch.Value.Text)
			return event.InboundEvent{
				TenantID:  string(tenantID),
				Channel:   event.Channel(channel),
				MessageID: id,
			}, nil
		}
	}
	return event.InboundEvent{}, errors.New("no processable messages in payload")
}
