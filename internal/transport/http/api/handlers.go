// Package api implementa os handlers HTTP da API REST do mez-go-mono.
//
// Endpoints da Fase 2 (#42):
//
//	GET    /api/conversations
//	GET    /api/messages?conversation_id=X
//	POST   /api/messages                  → 501 (Fase 3)
//	POST   /api/conversations/:id/assign  → 200
//	POST   /api/conversations/:id/resolve → 200
//	GET    /api/channels/:channel/health  → 200
//
// Auth: Bearer JWT (HS256, com claim tenant_id) OU session cookie admin.
// RLS via RunInTenantTx (claim tenant_id do token).
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

type ctxKey string

const tenantCtxKey ctxKey = "tenant_id"

// Handlers agrupa todos os handlers da API.
type Handlers struct {
	log        zerolog.Logger
	convRepo   port.ConversationRepo
	msgRepo    port.MessageRepo
	tenantRepo port.TenantRepo
}

// New cria os handlers.
func New(log zerolog.Logger, convRepo port.ConversationRepo, msgRepo port.MessageRepo, tenantRepo port.TenantRepo) *Handlers {
	return &Handlers{
		log:        log,
		convRepo:   convRepo,
		msgRepo:    msgRepo,
		tenantRepo: tenantRepo,
	}
}

// Register monta as rotas no router chi.
func (h *Handlers) Register(r chi.Router) {
	r.Get("/conversations", h.listConversations)
	r.Get("/messages", h.listMessages)
	r.Post("/messages", h.postMessageNotImplemented)
	r.Post("/conversations/{id}/assign", h.conversationAssign)
	r.Post("/conversations/{id}/resolve", h.conversationResolve)
	r.Get("/channels/{channel}/health", h.channelHealth)
}

// listConversations lista as conversas do tenant.
func (h *Handlers) listConversations(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := TenantFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "tenant required")
		return
	}
	convs, err := h.convRepo.ListByTenant(r.Context(), tenantID)
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

// listMessages lista as mensagens de uma conversa.
func (h *Handlers) listMessages(w http.ResponseWriter, r *http.Request) {
	convID := r.URL.Query().Get("conversation_id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "conversation_id required")
		return
	}
	msgs, err := h.msgRepo.ListByConversation(r.Context(), domain.ConversationID(convID))
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

// postMessageNotImplemented é o handler de POST /api/messages.
// Retorna 501 com mensagem explicativa: envio real é Fase 3.
func (h *Handlers) postMessageNotImplemented(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error":   "not_implemented",
		"message": "POST /api/messages está documentado no OpenAPI mas o envio real é implementado na Fase 3",
		"phase":   "fase3",
	})
}

// conversationAssign atribui uma conversa a um agente (ou desmarca).
// Fase 2: marca assigned_agent; lógica de ACD real é Fase 5.
func (h *Handlers) conversationAssign(w http.ResponseWriter, r *http.Request) {
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
	conv, err := h.convRepo.Get(r.Context(), domain.ConversationID(convID))
	if err != nil {
		writeError(w, http.StatusNotFound, "conversation not found")
		return
	}
	conv.AssignedAgent = body.AgentID
	if err := h.convRepo.Upsert(r.Context(), conv); err != nil {
		h.log.Error().Err(err).Msg("assign conversation")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "assigned_agent": body.AgentID})
}

// conversationResolve marca a conversa como resolvida.
func (h *Handlers) conversationResolve(w http.ResponseWriter, r *http.Request) {
	convID := chi.URLParam(r, "id")
	if convID == "" {
		writeError(w, http.StatusBadRequest, "id required")
		return
	}
	if err := h.convRepo.UpdateStatus(r.Context(), domain.ConversationID(convID), domain.ConvStatusResolved); err != nil {
		h.log.Error().Err(err).Msg("resolve conversation")
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "resolved"})
}

// channelHealth retorna a saúde do canal. Fase 2: sempre ok (sem healthcheck
// real; providers plugam na Fase 3).
func (h *Handlers) channelHealth(w http.ResponseWriter, r *http.Request) {
	channel := chi.URLParam(r, "channel")
	writeJSON(w, http.StatusOK, map[string]any{
		"channel": channel,
		"status":  "ok",
		"phase":   "fase2",
		"note":    "healthcheck real por provider chega na Fase 3",
	})
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
