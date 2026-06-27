// Package adminweb — server.go: wire-up do servidor HTTP admin e
// renderização de páginas. Após a migração para templ (Fase 2 da
// 000_FIXES.md, decisão revisto), o servidor não usa mais o wrapper
// html/template em render/. As páginas são components templ tipados
// declarados em internal/transport/adminweb/templates/, e a função
// renderTempl(component, w) escreve a saída em w.
package adminweb

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucauth "github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
	"github.com/felipedsvit/mez-go-mono/pkg/health"
)

// PageData é re-exportado do package templates para os handlers
// poderem usá-lo como templates.PageData sem importar o package
// de templates em todo lugar.
type PageData = templates.PageData

type Server struct {
	log     zerolog.Logger
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
	// auditRepo é o port de escrita (Record). Usado pelo middleware
	// RequireScope para logar negações. Issue #132 (Sprint 0A C4 audit).
	auditRepo cdomain.AuditRepo

	// Fase 6: backup service + admin verifier (para reset). Opcional —
	// se nil, rotas de backup/reset retornam 404.
	backup   *ucbackup.Service
	verifier ucbackup.AdminVerifier

	// Fase 10 (#177): system settings (substitui env vars app-level).
	settings *SettingsHandlers
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
	auditRepo cdomain.AuditRepo,
) *Server {
	return &Server{
		log:          log,
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
		auditRepo:    auditRepo,
	}
}

// SetBackupService injeta o backup service (Fase 6). Opcional — se não
// chamado, rotas de backup/reset não são registradas.
func (s *Server) SetBackupService(svc *ucbackup.Service, verifier ucbackup.AdminVerifier) {
	s.backup = svc
	s.verifier = verifier
}

// SetSettingsHandlers injeta o handler de system settings (Fase 10 #177).
// Opcional — se não chamado, rotas /admin/settings não são registradas.
func (s *Server) SetSettingsHandlers(h *SettingsHandlers) {
	s.settings = h
}

func (s *Server) Router() chi.Router {
	r := chi.NewRouter()

	// Issue #146 (Sprint 0B H13): HSTS condicional ao TLS ativo.
	// Antes era hardcoded false → HSTS nunca emitido. Agora segue
	// cfg.SessionCookieSecure (já true por default em prod, ver #131).
	r.Use(middleware.SecurityHeaders(s.sessionCfg.Secure))
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

		// Issue #132 (Sprint 0A C4 audit): gate de autorização por
		// (Permission, Scope). Cada handler state-changing tem o seu.
		// GETs públicos para o próprio user (dashboard, list, detail)
		// ficam sem gate (já passaram RequireAuth).
		platformUsers := middleware.RequireScope(cdomain.PermCreateUsers, cdomain.ScopePlatform, s.auditRepo)
		platformUpdateUsers := middleware.RequireScope(cdomain.PermUpdateUsers, cdomain.ScopePlatform, s.auditRepo)
		platformTenants := middleware.RequireScope(cdomain.PermCreateTenants, cdomain.ScopePlatform, s.auditRepo)
		platformUpdateTenants := middleware.RequireScope(cdomain.PermUpdateTenants, cdomain.ScopePlatform, s.auditRepo)
		platformUpdateRoles := middleware.RequireScope(cdomain.PermUpdateRoles, cdomain.ScopePlatform, s.auditRepo)
		platformManageChannels := middleware.RequireScope(cdomain.PermManageChannels, cdomain.ScopePlatform, s.auditRepo)

		r.Get("/", s.handleDashboard)
		r.Get("/tenants", s.handleTenantsList)
		r.Get("/tenants/new", s.handleTenantNew)
		r.With(platformTenants).Post("/tenants", s.handleTenantCreate)
		r.With(platformUpdateTenants).Post("/tenants/{id}/status", s.handleTenantStatus)

		r.Get("/users", s.handleUsersList)
		r.Get("/users/new", s.handleUserInvite)
		r.With(platformUsers).Post("/users", s.handleUserCreate)
		r.With(platformUpdateUsers).Post("/users/{id}/status", s.handleUserStatus)

		r.Get("/roles", s.handleRolesList)
		r.Get("/roles/{id}", s.handleRoleDetail)
		r.With(platformUpdateRoles).Post("/roles/{id}/permissions", s.handleRolePermissions)

		r.Get("/audit", s.handleAuditList)

		// Fase 6: backup/restore/reset UI (#86, #87).
		if s.backup != nil {
			r.Get("/tenants/{id}/backup", s.handleBackupPage)
			r.With(platformManageChannels).Post("/tenants/{id}/backup", s.handleBackupStart)
			r.Get("/tenants/{id}/backup/status", s.handleBackupStatus)
			r.Get("/tenants/{id}/backup/list", s.handleBackupList)
			r.With(platformManageChannels).Post("/tenants/{id}/restore", s.handleRestoreStart)

			r.Get("/tenants/{id}/reset", s.handleResetPage)
			r.With(platformManageChannels).Post("/tenants/{id}/reset", s.handleResetStart)
		}

		// Fase 10 (#177): system settings UI (substitui env vars app-level).
		if s.settings != nil {
			r.Get("/settings", s.settings.listSettings)
			r.With(platformManageChannels).Post("/settings", s.settings.postSetting)
			r.Get("/settings/{key}", s.settings.jsonValue)
			r.With(platformManageChannels).Post("/settings/{key}/delete", s.settings.deleteSetting)
		}
	})

	return r
}

// renderTempl escreve o component templ em w com Content-Type text/html.
// Substitui o antigo s.renderPage(w, "name.html", data). Em caso de
// erro de renderização (improvável — templates são type-checked em
// build via templ generate), loga e responde 500.
func (s *Server) renderTempl(w http.ResponseWriter, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(renderContext(), w); err != nil {
		s.log.Error().Err(err).Msg("render templ")
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
	if ok {
		return p
	}
	return cdomain.Principal{}
}

// csrfTokenFromContext extrai o CSRF token do cookie XSRF-TOKEN. Substitui
// o stub csrfTokenFromCtx de handlers_app.go que retornava string vazia.
// O cookie é setado pelo middleware CSRF no primeiro GET.
func csrfTokenFromContext(r *http.Request) string {
	if c, err := r.Cookie("XSRF-TOKEN"); err == nil {
		return c.Value
	}
	return ""
}

// basePageData monta um PageData com Principal e CSRFToken populados a
// partir do request. Os handlers preenchem Title e Error/Success.
func (s *Server) basePageData(r *http.Request) PageData {
	return PageData{
		Principal: principalOrEmpty(r),
		CSRFToken: csrfTokenFromContext(r),
		Now:       time.Now(),
	}
}

// renderContext devolve um context.Context com timeout razoável para
// renderização. Hoje é o request context diretamente; reservado para
// futuro timeout independente da request (ex.: renderização pesada).
func renderContext() context.Context {
	return context.Background()
}
