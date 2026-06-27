// Package adminweb — handlers_app.go: handlers /app/* (inbox tenant-side).
//
// Fase 5: /app/conversations (lista) + /app/conversations/:id (thread) +
// POST /app/conversations/:id/messages (send).
package adminweb

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
)

// AppHandlers agrupa dependências dos handlers /app/*.
type AppHandlers struct {
	convRepo  ConversationLister
	msgRepo   MessageLister
	sender    SenderService
	tenantCtx func(context.Context) (domain.TenantID, bool)
	log       zerolog.Logger
}

// ConversationLister lista conversations do tenant.
type ConversationLister interface {
	ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]domain.Conversation, error)
}

// MessageLister lista messages de uma conversation.
type MessageLister interface {
	ListByConversation(ctx context.Context, convID domain.ConversationID) ([]domain.Message, error)
}

// SenderService é o SenderService de Fase 3.
type SenderService interface {
	Send(ctx context.Context, req ucmessaging.SendRequest) (domain.Message, error)
}

// NewAppHandlers cria os handlers.
func NewAppHandlers(convRepo ConversationLister, msgRepo MessageLister, sender SenderService, tenantCtx func(context.Context) (domain.TenantID, bool), log zerolog.Logger) *AppHandlers {
	return &AppHandlers{
		convRepo:  convRepo,
		msgRepo:   msgRepo,
		sender:    sender,
		tenantCtx: tenantCtx,
		log:       log.With().Str("component", "app.handlers").Logger(),
	}
}

// inbox lista as conversas do tenant.
func (h *AppHandlers) inbox(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantCtx(r.Context())
	if !ok {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	convs, err := h.convRepo.ListByTenant(r.Context(), tenantID)
	if err != nil {
		h.log.Error().Err(err).Msg("inbox: list")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"Conversations": convs,
		"Total":         len(convs),
		"TenantID":      string(tenantID),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := InboxPage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("inbox: render")
	}
}

// thread exibe a thread de mensagens.
func (h *AppHandlers) thread(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantCtx(r.Context())
	if !ok {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	convID := chi.URLParam(r, "id")
	if convID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	msgs, err := h.msgRepo.ListByConversation(r.Context(), domain.ConversationID(convID))
	if err != nil {
		h.log.Error().Err(err).Msg("thread: list")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	data := map[string]any{
		"ConversationID": convID,
		"Messages":       msgs,
		"Total":          len(msgs),
		"TenantID":       string(tenantID),
		"WSURL":          "/app/ws",
		"CSRFToken":      csrfTokenFromCtx(r.Context()),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ThreadPage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("thread: render")
	}
}

// sendMessage envia uma mensagem outbound via SenderService.
func (h *AppHandlers) sendMessage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantCtx(r.Context())
	if !ok {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	convID := chi.URLParam(r, "id")
	if convID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	body := r.PostFormValue("body")
	channelStr := r.PostFormValue("channel")
	if body == "" || channelStr == "" {
		http.Error(w, "body and channel required", http.StatusBadRequest)
		return
	}
	contactID := r.PostFormValue("contact_id")

	// CSRF check (D16) — handled by middleware.
	_, err := h.sender.Send(r.Context(), ucmessaging.SendRequest{
		TenantID:       tenantID,
		Channel:        domain.Channel(channelStr),
		ConversationID: domain.ConversationID(convID),
		ContactID:      domain.ContactID(contactID),
		PeerID:         contactID,
		Type:           domain.MessageTypeText,
		Body:           body,
	})
	if err != nil {
		h.log.Error().Err(err).Msg("sendMessage")
		http.Error(w, "send failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/app/conversations/"+convID, http.StatusSeeOther)
}

// qrcode exibe o QR-code do whatsmeow (PNG inline) com htmx refresh.
func (h *AppHandlers) qrcode(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantCtx(r.Context())
	if !ok {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	_ = tenantID
	data := map[string]any{
		"TenantID":  string(tenantID),
		"Refreshed": time.Now().Format("15:04:05"),
		"CSRFToken": csrfTokenFromCtx(r.Context()),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := QRCodePage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("qrcode: render")
	}
}

// InboxPage é o template da inbox (stub — Fase 5: HTML inline).
var InboxPage = func(data map[string]any) Renderer { return stubRenderer("inbox", data) }

// ThreadPage é o template da thread.
var ThreadPage = func(data map[string]any) Renderer { return stubRenderer("thread", data) }

// QRCodePage é o template do QR-code.
var QRCodePage = func(data map[string]any) Renderer { return stubRenderer("qrcode", data) }

// ServicesPage é o template de /admin/services.
var ServicesPage = func(data map[string]any) Renderer { return stubRenderer("services", data) }

// ChannelsPage é o template de /admin/tenants/:id/channels.
var ChannelsPage = func(data map[string]any) Renderer { return stubRenderer("channels", data) }

// AgentsPage é o template de /admin/tenants/:id/agents.
var AgentsPage = func(data map[string]any) Renderer { return stubRenderer("agents", data) }

// Renderer é a interface mínima (template + htmx).
type Renderer interface {
	Render(ctx context.Context, w interface{ Write([]byte) (int, error) }) error
}

// stubRenderer é um fallback para Fase 5 build verde. Production substitui
// por `templ` gerado.
func stubRenderer(name string, data map[string]any) Renderer {
	return &stubR{name: name, data: data}
}

type stubR struct {
	name string
	data map[string]any
}

func (s *stubR) Render(_ context.Context, w interface{ Write([]byte) (int, error) }) error {
	body := "<!DOCTYPE html><html><body><h1>" + s.name + "</h1><pre>" + strconv.Quote("Fase 5 build verde — templ stub") + "</pre></body></html>"
	_, err := w.Write([]byte(body))
	return err
}

// csrfTokenFromCtx extrai o CSRF token do context (populado pelo middleware).
func csrfTokenFromCtx(_ context.Context) string {
	// Fase 5: stub. Production lê do context.
	return ""
}
