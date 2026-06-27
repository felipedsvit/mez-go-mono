// Package adminweb — handlers_settings.go: handlers /admin/settings (Fase 10 #177).
//
// App-level config (whatsmeow, ffmpeg, bus) é gerenciado via DB em
// system_settings. O admin panel expõe:
//
//   - GET  /admin/settings       — listar todas
//   - GET  /admin/settings/:key  — ler uma
//   - POST /admin/settings       — atualizar (form-encoded key+value+actor)
//
// Auth: requer platform role (mez_platform). RLS protege em DB layer
// (mez_app só lê; mez_platform ALL).
package adminweb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/settings"
)

// SettingsHandlers agrupa dependências do handler /admin/settings.
type SettingsHandlers struct {
	svc *settings.Service
	log zerolog.Logger
}

// NewSettingsHandlers cria os handlers.
func NewSettingsHandlers(svc *settings.Service, log zerolog.Logger) *SettingsHandlers {
	return &SettingsHandlers{
		svc: svc,
		log: log.With().Str("component", "admin.settings").Logger(),
	}
}

// listSettings exibe a tabela de settings (admin).
func (h *SettingsHandlers) listSettings(w http.ResponseWriter, r *http.Request) {
	views, err := h.svc.List(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("settings: list")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]templates.SettingRow, 0, len(views))
	for _, v := range views {
		rows = append(rows, templates.SettingRow{
			Key:         v.Key,
			Value:       v.Value,
			Description: v.Description,
			UpdatedAt:   v.UpdatedAt,
			UpdatedBy:   v.UpdatedBy,
		})
	}
	p := templates.PageData{Title: "System Settings", Now: time.Now()}
	component := templates.Settings(templates.SettingsData{Page: p, Settings: rows})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("settings: render")
	}
}

// postSetting atualiza uma setting (form-encoded).
//
// POST /admin/settings
//   key=<string>
//   value=<json|string|bool|int>
//   actor=<email>
//   description=<optional>
func (h *SettingsHandlers) postSetting(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	key := r.PostFormValue("key")
	rawValue := r.PostFormValue("value")
	actor := r.PostFormValue("actor")
	description := r.PostFormValue("description")

	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if actor == "" {
		http.Error(w, "actor required", http.StatusBadRequest)
		return
	}

	// Detecta o tipo do value via JSON unmarshal.
	value, err := parseJSONValue(rawValue)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid value: %v", err), http.StatusBadRequest)
		return
	}

	if description != "" {
		if err := h.svc.SetWithDescription(r.Context(), key, description, value, actor); err != nil {
			h.log.Error().Err(err).Msg("settings: set")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	} else {
		if err := h.svc.Set(r.Context(), key, value, actor); err != nil {
			h.log.Error().Err(err).Msg("settings: set")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	// Redirect de volta à lista.
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// deleteSetting remove uma setting (form-encoded).
func (h *SettingsHandlers) deleteSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	actor := r.PostFormValue("actor")
	if key == "" || actor == "" {
		http.Error(w, "key+actor required", http.StatusBadRequest)
		return
	}
	if err := h.svc.Delete(r.Context(), key, actor); err != nil {
		h.log.Error().Err(err).Msg("settings: delete")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// jsonValue devolve a setting (qualquer tipo) decifrada.
func (h *SettingsHandlers) jsonValue(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	views, err := h.svc.List(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	for _, v := range views {
		if v.Key == key {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key":         v.Key,
				"value":       v.Value,
				"description": v.Description,
				"updated_at":  v.UpdatedAt,
				"updated_by":  v.UpdatedBy,
			})
			return
		}
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// parseJSONValue detecta o tipo de value:
//   - "true"/"false" → bool
//   - "123" → int
//   - "12.5" → float
//   - "\"foo\"" → string
//   - "null" → nil
//   - outros → tenta JSON unmarshal; fallback para string crua.
func parseJSONValue(s string) (any, error) {
	if s == "" {
		return nil, nil
	}
	// Tenta JSON direto.
	var v any
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v, nil
	}
	// Fallback: string crua.
	return s, nil
}

// Compile-time guard.
var _ = context.Background
