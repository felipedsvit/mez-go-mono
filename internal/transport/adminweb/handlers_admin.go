// Package adminweb — handlers_admin.go: handlers /admin/* (services, channels, agents).
//
// Fase 5: /admin/services (health + métricas) +
// /admin/tenants/:id/channels (5 canais UI) +
// /admin/tenants/:id/agents (CRUD).
package adminweb

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// AdminHandlers agrupa os handlers admin-only.
type AdminHandlers struct {
	log           zerolog.Logger
	healthChecker interface {
		All(ctx context.Context) map[string]error
	}
}

// NewAdminHandlers cria os handlers.
func NewAdminHandlers(log zerolog.Logger, hc interface {
	All(ctx context.Context) map[string]error
}) *AdminHandlers {
	return &AdminHandlers{log: log.With().Str("component", "admin.handlers").Logger(), healthChecker: hc}
}

// services exibe health + métricas.
func (h *AdminHandlers) services(w http.ResponseWriter, r *http.Request) {
	var health map[string]error
	if h.healthChecker != nil {
		health = h.healthChecker.All(r.Context())
	}
	data := map[string]any{
		"Health": health,
		"Checks": len(health),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ServicesPage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("services: render")
	}
}

// channels lista os 5 canais com status.
func (h *AdminHandlers) channels(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	// Lista hardcoded dos 5 canais (Fase 5: sem registry dinâmico na UI).
	canais := []map[string]any{
		{"id": "waba", "name": "WhatsApp Business", "implemented": true, "capabilities": []string{"text", "media", "reactions", "delete", "templates"}},
		{"id": "whatsmeow", "name": "WhatsApp (informal)", "implemented": true, "capabilities": []string{"text", "media", "reactions", "edit", "delete", "groups", "presence", "typing", "mark_read"}},
		{"id": "instagram", "name": "Instagram Direct", "implemented": true, "capabilities": []string{"text", "media", "reactions"}},
		{"id": "messenger", "name": "Facebook Messenger", "implemented": true, "capabilities": []string{"text", "media", "reactions", "mark_read", "typing"}},
		{"id": "telegram_bot", "name": "Telegram Bot", "implemented": true, "capabilities": []string{"text", "media", "reactions", "edit", "delete", "typing", "groups", "inline_keyboard"}},
	}
	data := map[string]any{
		"TenantID": tenantID,
		"Channels": canais,
		"Total":    len(canais),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ChannelsPage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("channels: render")
	}
}

// agents lista os agentes do tenant.
func (h *AdminHandlers) agents(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	// Fase 5: stub. Production lê do core/admin.AgentRepo.
	data := map[string]any{
		"TenantID": tenantID,
		"Agents":   []any{}, // vazio
		"Total":    0,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := AgentsPage(data).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("agents: render")
	}
}

// postAgent cria um novo agente (stub).
func (h *AdminHandlers) postAgent(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := r.PostFormValue("email")
	if email == "" {
		http.Error(w, "email required", http.StatusBadRequest)
		return
	}
	// Fase 5: stub. Production: cria em core/admin.AgentRepo + audit log (D17).
	h.log.Info().Str("tenant", tenantID).Str("email", email).Msg("agents: create (stub)")
	http.Redirect(w, r, "/admin/tenants/"+tenantID+"/agents", http.StatusSeeOther)
}
