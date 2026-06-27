// Package adminweb — handlers_tenants.go: handlers /admin/tenants/*.
package adminweb

import (
	"net/http"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Dashboard"
	s.renderTempl(w, templates.Dashboard(p))
}

func (s *Server) handleTenantsList(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Tenants"
	tenants, err := s.tenants.List(r.Context(), cdomain.TenantFilter{})
	if err != nil {
		p.Error = "Error loading tenants"
		s.renderTempl(w, templates.Tenants(templates.TenantsData{Page: p, Tenants: nil}))
		return
	}
	rows := make([]cdomain.Tenant, len(tenants))
	for i, t := range tenants {
		rows[i] = t
	}
	s.renderTempl(w, templates.Tenants(templates.TenantsData{Page: p, Tenants: rows}))
}

func (s *Server) handleTenantNew(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "New Tenant"
	s.renderTempl(w, templates.TenantNew(templates.TenantNewData{Page: p}))
}

func (s *Server) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "New Tenant"
	name := s.formValue(r, "name")
	slug := s.formValue(r, "slug")

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}

	_, err := s.tenants.Provision(r.Context(), name, slug, actor)
	if err != nil {
		p.Error = err.Error()
		s.renderTempl(w, templates.TenantNew(templates.TenantNewData{Page: p}))
		return
	}

	s.redirect(w, r, "/admin/tenants")
}

func (s *Server) handleTenantStatus(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	id := r.PathValue("id")
	status := cdomain.TenantStatus(r.FormValue("status"))

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.tenants.SetStatus(r.Context(), id, status, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/tenants")
}
