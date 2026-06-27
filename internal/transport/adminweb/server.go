package adminweb

import (
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/render"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucauth "github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
	"github.com/felipedsvit/mez-go-mono/pkg/health"
)

type Server struct {
	log     zerolog.Logger
	render  *render.Renderer
	tpls    fs.FS
	health  *health.Checker
	version string

	login        *ucauth.LoginService
	logout       *ucauth.LogoutService
	sessionCfg   middleware.SessionConfig
	stateStore   cdomain.StateStore
	idp          cdomain.IdP
	loginLimiter *ratelimit.Limiter

	tenants ucadmin.TenantUseCase
	users   ucadmin.UserUseCase
	roles   ucadmin.RoleUseCase
	audit   ucadmin.AuditQueryUseCase

	// Fase 6: backup service + admin verifier (para reset). Opcional —
	// se nil, rotas de backup/reset retornam 404.
	backup   *ucbackup.Service
	verifier ucbackup.AdminVerifier
}

func NewServer(
	log zerolog.Logger,
	health *health.Checker,
	version string,
	login *ucauth.LoginService,
	logout *ucauth.LogoutService,
	sessionCfg middleware.SessionConfig,
	stateStore cdomain.StateStore,
	idp cdomain.IdP,
	loginLimiter *ratelimit.Limiter,
	tenants ucadmin.TenantUseCase,
	users ucadmin.UserUseCase,
	roles ucadmin.RoleUseCase,
	audit ucadmin.AuditQueryUseCase,
) *Server {
	funcs := template.FuncMap{
		"now": time.Now,
		"truncate": func(s string, n int) string {
			if len(s) > n {
				return s[:n-3] + "..."
			}
			return s
		},
		"hasPerm": func(perms []cdomain.Permission, perm string) bool {
			for _, p := range perms {
				if string(p) == perm {
					return true
				}
			}
			return false
		},
	}

	return &Server{
		log:          log,
		render:       render.New("base", funcs),
		tpls:         TemplatesFS,
		health:       health,
		version:      version,
		login:        login,
		logout:       logout,
		sessionCfg:   sessionCfg,
		stateStore:   stateStore,
		idp:          idp,
		loginLimiter: loginLimiter,
		tenants:      tenants,
		users:        users,
		roles:        roles,
		audit:        audit,
	}
}

// SetBackupService injeta o backup service (Fase 6). Opcional — se não
// chamado, rotas de backup/reset não são registradas.
func (s *Server) SetBackupService(svc *ucbackup.Service, verifier ucbackup.AdminVerifier) {
	s.backup = svc
	s.verifier = verifier
}

func (s *Server) Router() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.SecurityHeaders(false))
	r.Use(middleware.Session(s.sessionCfg))

	r.Get("/login", s.handleLoginGET)
	r.Post("/login", s.handleLoginPOST)
	r.Post("/logout", s.handleLogout)

	if s.idp != nil {
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth("/admin/login"))
		// Fase 6 (#85, D16): CSRF middleware em todas as rotas autenticadas.
		// /login fica fora deste grupo (não precisa de CSRF no login).
		r.Use(middleware.CSRF(middleware.DefaultCSRFConfig()))

		r.Get("/", s.handleDashboard)
		r.Get("/tenants", s.handleTenantsList)
		r.Get("/tenants/new", s.handleTenantNew)
		r.Post("/tenants", s.handleTenantCreate)
		r.Post("/tenants/{id}/status", s.handleTenantStatus)

		r.Get("/users", s.handleUsersList)
		r.Get("/users/new", s.handleUserInvite)
		r.Post("/users", s.handleUserCreate)
		r.Post("/users/{id}/status", s.handleUserStatus)

		r.Get("/roles", s.handleRolesList)
		r.Get("/roles/{id}", s.handleRoleDetail)
		r.Post("/roles/{id}/permissions", s.handleRolePermissions)

		r.Get("/audit", s.handleAuditList)

		// Fase 6: backup/restore/reset UI (#86, #87).
		if s.backup != nil {
			r.Get("/tenants/{id}/backup", s.handleBackupPage)
			r.Post("/tenants/{id}/backup", s.handleBackupStart)
			r.Get("/tenants/{id}/backup/status", s.handleBackupStatus)
			r.Get("/tenants/{id}/backup/list", s.handleBackupList)
			r.Post("/tenants/{id}/restore", s.handleRestoreStart)

			r.Get("/tenants/{id}/reset", s.handleResetPage)
			r.Post("/tenants/{id}/reset", s.handleResetStart)
		}
	})

	return r
}

func (s *Server) renderPage(w http.ResponseWriter, page string, data PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.render.Render(w, s.tpls, page, data); err != nil {
		s.log.Error().Err(err).Str("page", page).Msg("render error")
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, http.StatusFound)
}

func (s *Server) formValue(r *http.Request, key string) string {
	return r.FormValue(key)
}

func principalOrEmpty(r *http.Request) cdomain.Principal {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return cdomain.Principal{}
	}
	return p
}
