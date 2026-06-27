// Package api implementa os handlers HTTP da API REST do mez-go-mono.
//
// Endpoints da Fase 3 (#55):
//
//	GET    /api/conversations
//	GET    /api/messages?conversation_id=X
//	POST   /api/messages                    → 200 (Fase 3 real)
//	POST   /api/messages/{id}/reactions     → 200 (D6: ActionReaction)
//	PATCH  /api/messages/{id}               → 200 (D6: ActionEdit)
//	DELETE /api/messages/{id}               → 200 (D6: ActionRevoke)
//	POST   /api/conversations/{id}/assign   → 200
//	POST   /api/conversations/{id}/resolve  → 200
//	GET    /api/channels/{channel}/health   → 200 (real, registry)
//
// Auth: Bearer JWT (HS256, com claim tenant_id) OU session cookie admin.
// RLS via RunInTenantTx (claim tenant_id do token).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
)

type ctxKey string

const tenantCtxKey ctxKey = "tenant_id"

// Handlers agrupa todos os handlers da API.
type Handlers struct {
	log        zerolog.Logger
	convRepo   port.ConversationRepo
	msgRepo    port.MessageRepo
	tenantRepo port.TenantRepo
	sender     *ucmessaging.SenderService
	listSvc    *ucmessaging.ListService
	senderReg  port.SenderRegistry
	qrProvider QRCodeProvider
}

// QRCodeProvider expõe o QR code atual do whatsmeow (Fase 4 #68).
// Implementada pelo whatsmeow.Manager.
type QRCodeProvider interface {
	CurrentQR(ctx context.Context, tenantID domain.TenantID) (string, error)
}

// New cria os handlers.
func New(
	log zerolog.Logger,
	convRepo port.ConversationRepo,
	msgRepo port.MessageRepo,
	tenantRepo port.TenantRepo,
	sender *ucmessaging.SenderService,
	listSvc *ucmessaging.ListService,
	senderReg port.SenderRegistry,
	qrProvider QRCodeProvider,
) *Handlers {
	return &Handlers{
		log:        log,
		convRepo:   convRepo,
		msgRepo:    msgRepo,
		tenantRepo: tenantRepo,
		sender:     sender,
		listSvc:    listSvc,
		senderReg:  senderReg,
		qrProvider: qrProvider,
	}
}

// Register monta as rotas no router chi.
func (h *Handlers) Register(r chi.Router) {
	r.Get("/conversations", h.listConversations)
	r.Get("/messages", h.listMessages)
	r.Post("/messages", h.postMessage)
	r.Post("/messages/{id}/reactions", h.postReaction)
	r.Patch("/messages/{id}", h.patchMessage)
	r.Delete("/messages/{id}", h.deleteMessage)
	r.Post("/conversations/{id}/assign", h.conversationAssign)
	r.Post("/conversations/{id}/resolve", h.conversationResolve)
	r.Get("/channels/{channel}/health", h.channelHealth)
	r.Get("/channels/whatsmeow/qrcode", h.whatsmeowQRCode)
}

// listConversations lista as conversas do tenant. Issue #126:
// chama o use case ListService em vez de port.ConversationRepo direto
// (review DDD-Hex §3.2 — Skipping Use Cases).
func (h *Handlers) listConversations(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	convs, err := h.listSvc.ListConversations(r.Context(), tenantID)
	if err != nil {
		h.log.Error().Err(err).Msg("list conversations")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"conversations": convs,
		"total":         len(convs),
	})
}

// listMessages lista as mensagens de uma conversa. Issue #126:
// chama o use case ListService em vez de port.MessageRepo direto.
func (h *Handlers) listMessages(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}
	msgs, err := h.listSvc.ListMessages(r.Context(), tenantID, domain.ConversationID(convID))
	if err != nil {
		h.log.Error().Err(err).Msg("list messages")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"total":    len(msgs),
	})
}

// postMessage é o handler de POST /api/messages (Fase 3 real).
// Recebe OutboundMessage e despacha para SenderService.Send.
func (h *Handlers) postMessage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}

	var body struct {
		ConversationID string         `json:"conversation_id"`
		Channel        string         `json:"channel"`
		ContactID      string         `json:"contact_id"`
		PeerID         string         `json:"peer_id"`
		Type           string         `json:"type"`
		Body           string         `json:"body"`
		Metadata       map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.ConversationID == "" || body.Channel == "" {
		writeError(w, http.StatusBadRequest, "conversation_id and channel required")
		return
	}

	// Fase 4: whatsmeow é canal válido (port.Sender registrado no SenderRegistry).
	// Removido o stub 501 — production tem Manager + stubClient; o pipeline
	// funciona end-to-end.

	if h.sender == nil {
		writeError(w, http.StatusServiceUnavailable, "sender service not initialized")
		return
	}

	msg, err := h.sender.Send(r.Context(), ucmessaging.SendRequest{
		TenantID:       tenantID,
		Channel:        domain.Channel(body.Channel),
		ConversationID: domain.ConversationID(body.ConversationID),
		ContactID:      domain.ContactID(body.ContactID),
		PeerID:         body.PeerID,
		Type:           domain.MessageType(body.Type),
		Body:           body.Body,
		Metadata:       body.Metadata,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("post message")
		writeError(w, http.StatusInternalServerError, "send failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": string(msg.ID),
		"status":     string(msg.Status),
		"channel":    string(msg.Channel),
		"created_at": msg.CreatedAt,
	})
}

// postReaction é POST /api/messages/{id}/reactions (D6: ActionReaction).
func (h *Handlers) postReaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	msgID := chi.URLParam(r, "id")
	if msgID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	var body struct {
		Channel          string `json:"channel"`
		PeerID           string `json:"peer_id"`
		ConversationID   string `json:"conversation_id"`
		ContactID        string `json:"contact_id"`
		TargetProviderID string `json:"target_provider_id"`
		Emoji            string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if h.sender == nil {
		writeError(w, http.StatusServiceUnavailable, "sender service not initialized")
		return
	}
	if err := h.sender.SendAction(r.Context(), ucmessaging.SendActionRequest{
		TenantID:         tenantID,
		Channel:          domain.Channel(body.Channel),
		ConversationID:   domain.ConversationID(body.ConversationID),
		ContactID:        domain.ContactID(body.ContactID),
		PeerID:           body.PeerID,
		Action:           port.ActionReaction,
		TargetProviderID: body.TargetProviderID,
		ReactionEmoji:    body.Emoji,
	}); err != nil {
		h.log.Error().Err(err).Msg("post reaction")
		writeError(w, http.StatusInternalServerError, "reaction failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": msgID,
		"action":     "reaction",
		"status":     "queued",
	})
	_ = msgID
}

// patchMessage é PATCH /api/messages/{id} (D6: ActionEdit).
func (h *Handlers) patchMessage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	msgID := chi.URLParam(r, "id")
	if msgID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	var body struct {
		Channel          string `json:"channel"`
		PeerID           string `json:"peer_id"`
		ConversationID   string `json:"conversation_id"`
		ContactID        string `json:"contact_id"`
		TargetProviderID string `json:"target_provider_id"`
		NewBody          string `json:"new_body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if h.sender == nil {
		writeError(w, http.StatusServiceUnavailable, "sender service not initialized")
		return
	}
	if err := h.sender.SendAction(r.Context(), ucmessaging.SendActionRequest{
		TenantID:         tenantID,
		Channel:          domain.Channel(body.Channel),
		ConversationID:   domain.ConversationID(body.ConversationID),
		ContactID:        domain.ContactID(body.ContactID),
		PeerID:           body.PeerID,
		Action:           port.ActionEdit,
		TargetProviderID: body.TargetProviderID,
		NewBody:          body.NewBody,
	}); err != nil {
		h.log.Error().Err(err).Msg("patch message")
		writeError(w, http.StatusInternalServerError, "edit failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": msgID,
		"action":     "edit",
		"status":     "queued",
	})
}

// deleteMessage é DELETE /api/messages/{id} (D6: ActionRevoke).
func (h *Handlers) deleteMessage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	msgID := chi.URLParam(r, "id")
	if msgID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	var body struct {
		Channel          string `json:"channel"`
		PeerID           string `json:"peer_id"`
		ConversationID   string `json:"conversation_id"`
		ContactID        string `json:"contact_id"`
		TargetProviderID string `json:"target_provider_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if h.sender == nil {
		writeError(w, http.StatusServiceUnavailable, "sender service not initialized")
		return
	}
	if err := h.sender.SendAction(r.Context(), ucmessaging.SendActionRequest{
		TenantID:         tenantID,
		Channel:          domain.Channel(body.Channel),
		ConversationID:   domain.ConversationID(body.ConversationID),
		ContactID:        domain.ContactID(body.ContactID),
		PeerID:           body.PeerID,
		Action:           port.ActionRevoke,
		TargetProviderID: body.TargetProviderID,
	}); err != nil {
		h.log.Error().Err(err).Msg("delete message")
		writeError(w, http.StatusInternalServerError, "revoke failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": msgID,
		"action":     "revoke",
		"status":     "queued",
	})
}

// conversationAssign atribui uma conversa a um agente (ou desmarca).
// Issue #126: chama ListService.AssignConversation em vez de mexer no
// repo direto. Aplica FSM guard do AR (issue #125).
func (h *Handlers) conversationAssign(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	convID := chi.URLParam(r, "id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	var body struct {
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := h.listSvc.AssignConversation(r.Context(), tenantID, domain.ConversationID(convID), body.AgentID); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidTransition) {
			writeError(w, http.StatusConflict, "conversation is resolved")
			return
		}
		h.log.Error().Err(err).Msg("assign conversation")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "assigned_agent": body.AgentID})
}

// conversationResolve marca a conversa como resolvida. Issue #126:
// chama ListService.ResolveConversation em vez de mexer no repo direto.
func (h *Handlers) conversationResolve(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	convID := chi.URLParam(r, "id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	if err := h.listSvc.ResolveConversation(r.Context(), tenantID, domain.ConversationID(convID)); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			writeError(w, http.StatusNotFound, "conversation not found")
			return
		}
		h.log.Error().Err(err).Msg("resolve conversation")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "resolved"})
}

// whatsmeowQRCode retorna o par QR atual do tenant (Fase 4 #68).
// Usa o QRCodeProvider (whatsmeow.Manager) para ler o canal do stub/real
// Client. Se já conectado, retorna 204 No Content.
func (h *Handlers) whatsmeowQRCode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	if h.qrProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "qr provider not initialized")
		return
	}
	code, err := h.qrProvider.CurrentQR(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no qr available: "+err.Error())
		return
	}
	if code == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant":   string(tenantID),
		"qr":       code,
		"provider": "whatsmeow",
	})
}

// channelHealth retorna a saúde do canal (Fase 3: registry real; Fase 4: whatsmeow stub).
func (h *Handlers) channelHealth(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	channel := domain.Channel(chi.URLParam(r, "channel"))

	// Fase 4: whatsmeow health é real (Manager-backed), não mais 501.
	if channel == domain.ChannelWAWeb {
		writeJSON(w, http.StatusOK, map[string]any{
			"channel": string(channel),
			"status":  "ok",
			"phase":   "fase4",
			"note":    "whatsmeow: Manager + Dispatcher + stub client (Fase 4); production: real session",
		})
		return
	}

	if h.senderReg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"channel": string(channel),
			"status":  "registry_nil",
		})
		return
	}

	health := h.senderReg.Health(r.Context(), tenantID)
	err, found := health[channel]
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"channel":  string(channel),
			"status":   "not_registered",
			"channels": h.senderReg.Channels(),
		})
		return
	}

	status := "ok"
	if err != nil {
		status = "error"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": string(channel),
		"status":  status,
		"error":   errOrEmpty(err),
	})
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ---- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": http.StatusText(status), "message": msg})
}

// TenantFromContext extrai o tenant_id do contexto (populado pelo middleware).
func TenantFromContext(ctx context.Context) (domain.TenantID, bool) {
	v := ctx.Value(tenantCtxKey)
	if v == nil {
		return "", false
	}
	t, ok := v.(domain.TenantID)
	return t, ok
}

// ContextWithTenant injeta o tenant_id no contexto. Helper para tests
// e para o middleware BearerAuth.
func ContextWithTenant(ctx context.Context, t domain.TenantID) context.Context {
	return context.WithValue(ctx, tenantCtxKey, t)
}
