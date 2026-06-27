// Package adminweb — handlers_users.go: handlers /admin/users/*.
package adminweb

import (
	"net/http"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Users"
	users, err := s.users.List(r.Context(), cdomain.UserFilter{})
	if err != nil {
		p.Error = "Error loading users"
		s.renderTempl(w, templates.Users(templates.UsersData{Page: p, Users: nil}))
		return
	}
	rows := make([]templates.UserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, templates.UserRow{
			ID:     string(u.ID),
			Email:  u.Email,
			Name:   u.Name,
			Status: u.Status,
		})
	}
	s.renderTempl(w, templates.Users(templates.UsersData{Page: p, Users: rows}))
}

func (s *Server) handleUserInvite(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Invite User"
	roles, _ := s.roles.ListBuiltins(r.Context())
	s.renderTempl(w, templates.UserNew(templates.UserNewData{Page: p, Roles: roles}))
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	p.Title = "Invite User"
	email := s.formValue(r, "email")
	name := s.formValue(r, "name")
	roleID := cdomain.RoleID(s.formValue(r, "role_id"))

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}

	_, _, err := s.users.Invite(r.Context(), email, name, roleID, actor)
	if err != nil {
		roles, _ := s.roles.ListBuiltins(r.Context())
		p.Error = err.Error()
		s.renderTempl(w, templates.UserNew(templates.UserNewData{Page: p, Roles: roles}))
		return
	}

	s.redirect(w, r, "/admin/users")
}

func (s *Server) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	p := s.basePageData(r)
	id := cdomain.AdminUserID(r.PathValue("id"))
	status := cdomain.UserStatus(r.FormValue("status"))

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.users.SetStatus(r.Context(), id, status, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/users")
}
