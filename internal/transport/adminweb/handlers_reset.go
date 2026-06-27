// Package adminweb — handlers_reset.go: UI de reset por tenant (#87).
//
// Confirmação dupla (D16 do PLAN):
//   1. Texto literal "RESET" (não pode estar vazio).
//   2. Senha do admin re-checada (Argon2 contra admin_users.password_hash).
//
// UX: formulário com aviso vermelho, ambos os inputs obrigatórios, botão
// desabilitado via JS até ambos preenchidos + texto correto.

package adminweb

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
)

func (s *Server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	principal := principalOrEmpty(r)

	data := PageData{
		Title:     "Reset — " + tenantID,
		Principal: principal,
		Now:       time.Now(),
		Data:      map[string]any{"TenantID": tenantID},
	}
	s.renderPage(w, "reset.html", data)
}

func (s *Server) handleResetStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	principal := principalOrEmpty(r)

	confirmText := r.FormValue("confirm_text")
	adminPassword := r.FormValue("admin_password")

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}
	_, err := s.backup.Reset(r.Context(), ucbackup.ResetRequest{
		TenantID:      tenantID,
		Actor:         cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		ConfirmText:   confirmText,
		AdminPassword: adminPassword,
	}, s.verifier)

	if err != nil {
		// Mapear erros específicos para mensagens mais claras.
		msg := err.Error()
		s.renderPage(w, "reset.html", PageData{
			Title:     "Reset — " + tenantID,
			Principal: principal,
			Now:       time.Now(),
			Error:     msg,
			Data:      map[string]any{"TenantID": tenantID},
		})
		return
	}

	s.redirect(w, r, "/admin/tenants/"+tenantID+"/backup?reset=done")
}
