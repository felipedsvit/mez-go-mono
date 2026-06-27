package adminweb

import (
	"context"
	"html/template"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/auth/argon2"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// SetupHandler serves GET /setup and POST /setup (issue #31, replaces Fase 0's
// placeholder). The HTTP wizard is the only bootstrap path; there is no
// env-var-based fallback.
//
// GET  /setup — renders the form if and only if admin_users has zero rows.
//
//	Once any admin exists, the route is disabled and the handler
//	returns 404 (criterion in issue #16/#31).
//
// POST /setup — validates the form, hashes the password with Argon2id
//
//	(OWASP 2024, from the adapter package), inserts the user
//	into admin_users, assigns role-platform-superadmin, and
//	redirects to /admin/login.
type SetupHandler struct {
	pool *pgxpool.Pool
	log  zerolog.Logger
}

func NewSetupHandler(pool *pgxpool.Pool, log zerolog.Logger) *SetupHandler {
	return &SetupHandler{pool: pool, log: log}
}

func (h *SetupHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /setup", h.Get)
	mux.HandleFunc("POST /setup", h.Post)
}

func (h *SetupHandler) Get(w http.ResponseWriter, r *http.Request) {
	exists, err := h.adminExists(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("setup: count admins")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists {
		http.NotFound(w, r)
		return
	}
	csrfToken := ""
	if c, err := r.Cookie("XSRF-TOKEN"); err == nil {
		csrfToken = c.Value
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupTpl.Execute(w, setupPageData{CSRFToken: csrfToken, Error: ""}); err != nil {
		h.log.Error().Err(err).Msg("setup: render")
	}
}

func (h *SetupHandler) Post(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, "invalid form")
		return
	}
	email := strings.TrimSpace(strings.ToLower(r.FormValue("email")))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	if _, err := mail.ParseAddress(email); err != nil {
		h.renderError(w, r, "invalid email")
		return
	}
	if len(password) < 8 {
		h.renderError(w, r, "password must be at least 8 characters")
		return
	}
	if password != confirm {
		h.renderError(w, r, "passwords do not match")
		return
	}

	// Race protection: if a parallel request created one, fail closed.
	exists, err := h.adminExists(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("setup: recount admins")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if exists {
		http.NotFound(w, r)
		return
	}

	hash, err := argon2.New(argon2.DefaultParams()).Hash(r.Context(), password)
	if err != nil {
		h.log.Error().Err(err).Msg("setup: hash password")
		h.renderError(w, r, "could not hash password")
		return
	}

	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		h.log.Error().Err(err).Msg("setup: begin tx")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	var userID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO admin_users (email, name, auth_kind, status, password_hash)
		 VALUES ($1, 'Platform Administrator', 'local', 'active', $2)
		 RETURNING id`,
		email, hash,
	).Scan(&userID); err != nil {
		h.log.Error().Err(err).Msg("setup: insert user")
		http.Error(w, "could not create admin", http.StatusInternalServerError)
		return
	}

	// Assign platform-superadmin role.
	if _, err := tx.Exec(r.Context(),
		`INSERT INTO admin_user_roles (user_id, role_id, tenant_id) VALUES ($1, '00000000-0000-0000-0000-000000000001', NULL)`,
		userID,
	); err != nil {
		h.log.Error().Err(err).Msg("setup: assign role")
		http.Error(w, "could not assign role", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		h.log.Error().Err(err).Msg("setup: commit")
		http.Error(w, "could not commit", http.StatusInternalServerError)
		return
	}

	h.log.Info().Str("admin_id", userID).Str("email", email).Msg("admin user created")
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (h *SetupHandler) adminExists(ctx context.Context) (bool, error) {
	var count int
	err := h.pool.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE status = 'active'`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *SetupHandler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	csrfToken := ""
	if c, err := r.Cookie("XSRF-TOKEN"); err == nil {
		csrfToken = c.Value
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	if err := setupTpl.Execute(w, setupPageData{CSRFToken: csrfToken, Error: msg}); err != nil {
		h.log.Error().Err(err).Msg("setup: render error")
	}
}

type setupPageData struct {
	CSRFToken string
	Error     string
}

var setupTpl = template.Must(template.New("setup").Parse(setupHTML))

const setupHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Setup — Mez Admin</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #f5f5f5; color: #333; display: flex; align-items: center; justify-content: center; min-height: 100vh; }
.card { background: white; border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); padding: 32px; max-width: 400px; width: 100%; }
h1 { margin-bottom: 8px; font-size: 24px; }
.subtitle { color: #666; margin-bottom: 24px; font-size: 14px; }
.error { background: #fee; color: #c00; padding: 10px 15px; border-radius: 4px; margin-bottom: 15px; font-size: 14px; }
.form-group { margin-bottom: 15px; }
.form-group label { display: block; margin-bottom: 5px; font-weight: 500; font-size: 14px; }
.form-group input { width: 100%; padding: 10px 12px; border: 1px solid #ddd; border-radius: 4px; font-size: 14px; }
.btn { display: inline-block; width: 100%; padding: 12px 16px; background: #1a1a2e; color: white; border: none; border-radius: 4px; font-size: 14px; cursor: pointer; }
.btn:hover { background: #2a2a3e; }
</style>
</head>
<body>
<div class="card">
<h1>Welcome to Mez Admin</h1>
<p class="subtitle">Create the first administrator account. This page will be disabled once any admin exists.</p>
{{if .Error}}<div class="error">{{.Error}}</div>{{end}}
<form method="POST" action="/setup">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<div class="form-group">
<label for="email">Email</label>
<input type="email" id="email" name="email" required>
</div>
<div class="form-group">
<label for="password">Password (≥ 8 chars)</label>
<input type="password" id="password" name="password" required minlength="8">
</div>
<div class="form-group">
<label for="password_confirm">Confirm password</label>
<input type="password" id="password_confirm" name="password_confirm" required minlength="8">
</div>
<button type="submit" class="btn">Create Administrator</button>
</form>
</div>
</body>
</html>`

var _ admin.AdminUserID // ensure core/admin is imported (used by tests in other files)
