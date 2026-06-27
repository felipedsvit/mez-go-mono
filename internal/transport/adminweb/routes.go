// Package adminweb — routes.go: wire-up das rotas Fase 5.
//
// Adiciona ao Server existente:
//   - GET  /app/conversations                  (inbox)
//   - GET  /app/conversations/{id}             (thread)
//   - POST /app/conversations/{id}/messages    (send via SenderService)
//   - GET  /admin/services                     (health + métricas)
//   - GET  /admin/tenants/{id}/channels        (5 canais UI)
//   - GET  /admin/tenants/{id}/qrcode          (whatsmeow PNG, htmx refresh 5s)
//   - GET  /admin/tenants/{id}/agents          (CRUD agentes)
//   - GET  /admin/settings                     (system_settings — Fase 10 #177)
//   - POST /admin/settings                     (criar/atualizar)
//   - GET  /admin/settings/{key}              (ler uma, JSON)
//   - POST /admin/settings/{key}/delete        (deletar)
//
// CSRF middleware (D16) é wired no wire.go.
package adminweb

import (
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// Routes agrupa os handlers para registro.
type Routes struct {
	App      *AppHandlers
	Admin    *AdminHandlers
	Settings *SettingsHandlers
	Log      zerolog.Logger
}

// Register monta as rotas Fase 5 no router chi.
func (r *Routes) Register(router chi.Router) {
	// /app/* — tenant-scoped (inbox).
	if r.App != nil {
		router.Get("/app/conversations", r.App.inbox)
		router.Get("/app/conversations/{id}", r.App.thread)
		router.Post("/app/conversations/{id}/messages", r.App.sendMessage)
		router.Get("/app/qrcode", r.App.qrcode)
	}
	// /admin/* — admin only.
	if r.Admin != nil {
		router.Get("/admin/services", r.Admin.services)
		router.Get("/admin/tenants/{id}/channels", r.Admin.channels)
		// qrcode do admin reusa o do app (whatsmeow Manager.CurrentQR).
		router.Get("/admin/tenants/{id}/qrcode", r.App.qrcode)
		router.Get("/admin/tenants/{id}/agents", r.Admin.agents)
		router.Post("/admin/tenants/{id}/agents", r.Admin.postAgent)
	}
	// /admin/settings/* — Fase 10 (#177): system-level config.
	if r.Settings != nil {
		router.Get("/admin/settings", r.Settings.listSettings)
		router.Post("/admin/settings", r.Settings.postSetting)
		router.Get("/admin/settings/{key}", r.Settings.jsonValue)
		router.Post("/admin/settings/{key}/delete", r.Settings.deleteSetting)
	}
}
