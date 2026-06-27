// Package adminweb — handlers_admin.go: handlers /admin/* (services, channels, agents).
package adminweb

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

// AdminHandlers agrupa dependências dos handlers admin-only.
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
	checks := make([]templates.HealthCheck, 0, len(health))
	for name, err := range health {
		hc := templates.HealthCheck{Name: name}
		if err == nil {
			hc.Status = "ok"
		} else {
			hc.Status = "down"
			hc.Detail = err.Error()
		}
		checks = append(checks, hc)
	}
	p := templates.PageData{Title: "Services", Now: time.Now()}
	component := templates.Health(templates.HealthData{Page: p, Checks: checks})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
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
	channels := []templates.ChannelRow{
		{ID: "waba", Name: "WhatsApp Business", Implemented: true, Capabilities: []templates.ChannelCapability{"text", "media", "reactions", "delete", "templates"}},
		{ID: "whatsmeow", Name: "WhatsApp (informal)", Implemented: true, Capabilities: []templates.ChannelCapability{"text", "media", "reactions", "edit", "delete", "groups", "presence", "typing", "mark_read"}},
		{ID: "instagram", Name: "Instagram Direct", Implemented: true, Capabilities: []templates.ChannelCapability{"text", "media", "reactions"}},
		{ID: "messenger", Name: "Facebook Messenger", Implemented: true, Capabilities: []templates.ChannelCapability{"text", "media", "reactions", "mark_read", "typing"}},
		{ID: "telegram_bot", Name: "Telegram Bot", Implemented: true, Capabilities: []templates.ChannelCapability{"text", "media", "reactions", "edit", "delete", "typing", "groups", "inline_keyboard"}},
	}
	p := templates.PageData{Title: "Channels", Now: time.Now()}
	component := templates.Channels(templates.ChannelsData{Page: p, TenantID: tenantID, Channels: channels})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
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
	p := templates.PageData{Title: "Agents", Now: time.Now()}
	component := templates.Agents(templates.AgentsData{Page: p, TenantID: tenantID, Agents: []templates.AgentRow{}})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
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
