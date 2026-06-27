package adminweb

import (
	"net/http"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
)

func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	users, err := s.users.List(r.Context(), cdomain.UserFilter{})
	if err != nil {
		s.renderPage(w, "users.html", PageData{Title: "Users", Error: "Error loading users", Now: time.Now(), Principal: principal})
		return
	}

	data := PageData{
		Title:     "Users",
		Principal: principal,
		Data:      users,
		Now:       time.Now(),
	}
	s.renderPage(w, "users.html", data)
}

func (s *Server) handleUserInvite(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	roles, _ := s.roles.ListBuiltins(r.Context())

	data := PageData{
		Title:     "Invite User",
		Principal: principal,
		Data:      roles,
		Now:       time.Now(),
	}
	s.renderPage(w, "user_new.html", data)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	email := s.formValue(r, "email")
	name := s.formValue(r, "name")
	roleID := cdomain.RoleID(s.formValue(r, "role_id"))

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}

	_, _, err := s.users.Invite(r.Context(), email, name, roleID, actor)
	if err != nil {
		roles, _ := s.roles.ListBuiltins(r.Context())
		data := PageData{
			Title:     "Invite User",
			Error:     err.Error(),
			Principal: principal,
			Data:      roles,
			Now:       time.Now(),
		}
		s.renderPage(w, "user_new.html", data)
		return
	}

	s.redirect(w, r, "/admin/users")
}

func (s *Server) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	principal := principalOrEmpty(r)
	id := cdomain.AdminUserID(r.PathValue("id"))
	status := cdomain.UserStatus(r.FormValue("status"))

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}

	if err := s.users.SetStatus(r.Context(), id, status, actor); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.redirect(w, r, "/admin/users")
}
