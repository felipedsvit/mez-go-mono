package adminweb

import (
	"net/http"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	data := PageData{
		Title:     "Dashboard",
		Principal: principal,
		Now:       time.Now(),
	}
	s.renderPage(w, "dashboard.html", data)
}

func (s *Server) handleTenantsList(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	tenants, err := s.tenants.List(r.Context(), cdomain.TenantFilter{})
	if err != nil {
		s.renderPage(w, "tenants.html", PageData{Title: "Tenants", Error: "Error loading tenants", Now: time.Now(), Principal: principal})
		return
	}

	data := PageData{
		Title:     "Tenants",
		Principal: principal,
		Data:      tenants,
		Now:       time.Now(),
	}
	s.renderPage(w, "tenants.html", data)
}

func (s *Server) handleTenantNew(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	data := PageData{
		Title:     "New Tenant",
		Principal: principal,
		Now:       time.Now(),
	}
	s.renderPage(w, "tenant_new.html", data)
}

func (s *Server) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	name := s.formValue(r, "name")
	slug := s.formValue(r, "slug")

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}

	_, err := s.tenants.Provision(r.Context(), name, slug, actor)
	if err != nil {
		data := PageData{
			Title:     "New Tenant",
			Error:     err.Error(),
			Principal: principal,
			Now:       time.Now(),
		}
		s.renderPage(w, "tenant_new.html", data)
		return
	}

	s.redirect(w, r, "/admin/tenants")
}

func (s *Server) handleTenantStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	id := r.PathValue("id")
	status := cdomain.TenantStatus(r.FormValue("status"))

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.tenants.SetStatus(r.Context(), id, status, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/tenants")
}
