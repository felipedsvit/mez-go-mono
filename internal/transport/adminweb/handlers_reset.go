// Package adminweb — handlers_reset.go: UI de reset por tenant (#87).
//
// Confirmação dupla (D16 do PLAN):
//  1. Texto literal "RESET" (não pode estar vazio).
//  2. Senha do admin re-checada (Argon2 contra admin_users.password_hash).
//
// UX: formulário com aviso vermelho, ambos os inputs obrigatórios, botão
// desabilitado via JS até ambos preenchidos + texto correto.
package adminweb

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleResetPage(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	p := s.basePageData(r)
	p.Title = "Reset — " + tenantID
	s.renderTempl(w, templates.Reset(templates.ResetData{Page: p, TenantID: tenantID}))
}

func (s *Server) handleResetStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	p := s.basePageData(r)
	p.Title = "Reset — " + tenantID

	confirmText := r.FormValue("confirm_text")
	adminPassword := r.FormValue("admin_password")

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}
	_, err := s.backup.Reset(r.Context(), ucbackup.ResetRequest{
		TenantID:      tenantID,
		Actor:         cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		ConfirmText:   confirmText,
		AdminPassword: adminPassword,
	}, s.verifier)

	if err != nil {
		p.Error = err.Error()
		s.renderTempl(w, templates.Reset(templates.ResetData{Page: p, TenantID: tenantID}))
		return
	}

	s.redirect(w, r, "/admin/tenants/"+tenantID+"/backup?reset=done")
}
