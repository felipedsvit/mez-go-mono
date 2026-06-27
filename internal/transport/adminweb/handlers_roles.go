package adminweb

import (
	"net/http"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
)

func (s *Server) handleRolesList(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	roles, err := s.roles.ListBuiltins(r.Context())
	if err != nil {
		s.renderPage(w, "roles.html", PageData{Title: "Roles", Error: "Error loading roles", Now: time.Now(), Principal: principal})
		return
	}

	data := PageData{
		Title:     "Roles",
		Principal: principal,
		Data:      roles,
		Now:       time.Now(),
	}
	s.renderPage(w, "roles.html", data)
}

func (s *Server) handleRoleDetail(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	id := cdomain.RoleID(r.PathValue("id"))

	role, err := s.roles.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "Role not found", http.StatusNotFound)
		return
	}

	data := PageData{
		Title:     "Role: " + role.Name,
		Principal: principal,
		Data:      role,
		Now:       time.Now(),
	}
	s.renderPage(w, "role_detail.html", data)
}

func (s *Server) handleRolePermissions(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	id := cdomain.RoleID(r.PathValue("id"))

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	var permissions []cdomain.Permission
	for _, p := range r.Form["permissions"] {
		permissions = append(permissions, cdomain.Permission(p))
	}

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.roles.SetPermissions(r.Context(), id, permissions, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/roles/"+string(id))
}
