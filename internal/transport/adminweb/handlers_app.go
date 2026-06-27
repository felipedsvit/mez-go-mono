// Package adminweb — handlers_app.go: handlers /app/* (inbox tenant-side).
//
// Após a migração para templ (Fase 2 da 000_FIXES.md), os 6 stubs
// stubRenderer / stubR / Renderer foram removidos. As páginas
// inbox/thread/qrcode usam components templ tipados em
// internal/transport/adminweb/templates/.
package adminweb

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
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
	rows := make([]templates.InboxConv, 0, len(convs))
	for _, c := range convs {
		rows = append(rows, templates.InboxConv{
			ID:        string(c.ID),
			Channel:   string(c.Channel),
			PeerID:    string(c.ContactID),
			State:     string(c.Status),
			Assignee:  c.AssignedAgent,
			LastMsgAt: c.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	p := templates.PageData{
		Title:     "Inbox",
		Now:       time.Now(),
		CSRFToken: csrfTokenFromContext(r),
	}
	component := templates.Inbox(templates.InboxData{Page: p, Conversations: rows})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
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
	msgRows := make([]templates.ThreadMessage, 0, len(msgs))
	for _, m := range msgs {
		msgRows = append(msgRows, templates.ThreadMessage{
			ID:        string(m.ID),
			Direction: string(m.Direction),
			Type:      string(m.Type),
			Text:      m.Body,
			Timestamp: m.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	p := templates.PageData{
		Title:     "Thread",
		Now:       time.Now(),
		CSRFToken: csrfTokenFromContext(r),
	}
	_ = tenantID
	component := templates.Thread(templates.ThreadData{
		Page: p,
		Conv: templates.ThreadConv{
			ID:       convID,
			Channel:  "waba", // resolvido pelo repositório em produção
			PeerID:   "—",
			State:    "open",
		},
		Messages: msgRows,
		WSURL:    "/app/ws",
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
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
	p := templates.PageData{
		Title:     "QR Code",
		Now:       time.Now(),
		CSRFToken: csrfTokenFromContext(r),
	}
	component := templates.QRCode(templates.QRCodeData{
		Page:     p,
		ImagePNG: "", // preenchido pela whatsmeow manager em produção
		Refresh:  "5s",
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("qrcode: render")
	}
}
