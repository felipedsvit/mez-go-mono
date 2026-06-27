package adminweb

import (
	"context"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// SetupHandler serves GET /setup and POST /setup (issue #16).
//
// GET  /setup — renders the form if and only if the admin_globals table has
//               zero rows. Once any admin exists, the route is disabled and
//               the handler returns 404 (criterion in issue #16).
//
// POST /setup — validates the form, hashes the password with Argon2id
//               (OWASP 2024), inserts the admin, and redirects to /login.
//               Falls back to re-rendering the form on validation errors.
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupPage("").Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("setup: render")
	}
}

func (h *SetupHandler) Post(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderError(w, r, "invalid form")
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	// Validate.
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

	// Verify still no admin (race protection: if a parallel request created one,
	// fail closed).
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

	// Hash password.
	hash, err := admin.HashPassword(password)
	if err != nil {
		h.renderError(w, r, "could not hash password")
		return
	}

	// Insert.
	id := uuid.New()
	if _, err := h.pool.Exec(r.Context(),
		`INSERT INTO admin_globals (id, email, password_hash, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
		id, strings.ToLower(email), hash,
	); err != nil {
		h.log.Error().Err(err).Msg("setup: insert admin")
		http.Error(w, "could not create admin", http.StatusInternalServerError)
		return
	}

	h.log.Info().Str("admin_id", id.String()).Str("email", email).Msg("admin global created")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *SetupHandler) adminExists(ctx context.Context) (bool, error) {
	var count int
	err := h.pool.QueryRow(ctx, `SELECT count(*) FROM admin_globals`).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (h *SetupHandler) renderError(w http.ResponseWriter, r *http.Request, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	if err := setupPage(msg).Render(r.Context(), w); err != nil {
		h.log.Error().Err(err).Msg("setup: render error")
	}
}
