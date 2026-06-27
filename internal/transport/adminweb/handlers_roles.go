// Package adminweb — handlers_roles.go: handlers /admin/roles/*.
package adminweb

import (
	"net/http"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleRolesList(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Roles"
	roles, err := s.roles.ListBuiltins(r.Context())
	if err != nil {
		p.Error = "Error loading roles"
		s.renderTempl(w, templates.Roles(templates.RolesData{Page: p, Roles: nil}))
		return
	}
	s.renderTempl(w, templates.Roles(templates.RolesData{Page: p, Roles: roles}))
}

func (s *Server) handleRoleDetail(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	id := cdomain.RoleID(r.PathValue("id"))

	role, err := s.roles.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Role not found", http.StatusNotFound)
		return
	}

	// As permissões já vêm no role.Permissions — RoleRepo.GetByID carrega.
	s.renderTempl(w, templates.RoleDetail(templates.RoleDetailData{
		Page:     p,
		Role:     role,
		AllPerms: allPermissions(),
		HasPerms: role.Permissions,
	}))
}

func (s *Server) handleRolePermissions(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	id := cdomain.RoleID(r.PathValue("id"))

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	var permissions []cdomain.Permission
	for _, perm := range r.Form["permissions"] {
		permissions = append(permissions, cdomain.Permission(perm))
	}

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.roles.SetPermissions(r.Context(), id, permissions, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/roles/"+string(id))
}

// allPermissions devolve a lista canônica de permissions disponíveis
// (espelha os const em core/admin/role.go). Usado pelo template de
// detalhe para renderizar o checklist de grant/deny.
func allPermissions() []cdomain.Permission {
	return []cdomain.Permission{
		cdomain.PermReadTenants,
		cdomain.PermCreateTenants,
		cdomain.PermUpdateTenants,
		cdomain.PermDeleteTenants,
		cdomain.PermReadUsers,
		cdomain.PermCreateUsers,
		cdomain.PermUpdateUsers,
		cdomain.PermDeleteUsers,
		cdomain.PermReadRoles,
		cdomain.PermCreateRoles,
		cdomain.PermUpdateRoles,
		cdomain.PermReadAudit,
		cdomain.PermReadSecrets,
		cdomain.PermCreateSecrets,
		cdomain.PermManageChannels,
	}
}
