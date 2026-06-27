package adminweb

import (
	"net/http"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
)

func (s *Server) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.PrincipalFromContext(r.Context())
	if ok {
		s.redirect(w, r, "/admin/")
		return
	}

	// Read the CSRF cookie set by the CSRF middleware on this GET; the
	// template embeds it as a hidden field so the POST can validate.
	csrfToken := ""
	if c, err := r.Cookie("XSRF-TOKEN"); err == nil {
		csrfToken = c.Value
	}

	data := PageData{
		Title:     "Login",
		Now:       time.Now(),
		CSRFToken: csrfToken,
	}
	s.renderPage(w, "login.html", data)
}

func (s *Server) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	// Rate limit by client IP. Defense-in-depth with the per-email lockout
	// in usecase/auth/lockout: this one stops HTTP-level bursts before they
	// reach the password store.
	if s.loginLimiter != nil {
		key := ratelimit.ClientIP(r)
		if !s.loginLimiter.Allow(key) {
			w.Header().Set("Retry-After", "1")
			data := PageData{
				Title: "Login",
				Error: "Too many login attempts. Please try again in a minute.",
				Now:   time.Now(),
			}
			s.renderPage(w, "login.html", data)
			return
		}
	}

	email := s.formValue(r, "email")
	password := s.formValue(r, "password")

	result, err := s.login.LoginLocal(r.Context(), auth.LoginInput{
		Email:     email,
		Password:  password,
		IP:        r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		// Map each sentinel error to a user-facing message. Never reveal
		// whether the email exists (user enumeration guardrail).
		msg := "Invalid email or password"
		switch err {
		case cdomain.ErrTooManyAttempts:
			msg = "Too many attempts. Please try again later."
		case cdomain.ErrUserDisabled:
			msg = "Account disabled. Contact the administrator."
		}
		data := PageData{
			Title: "Login",
			Error: msg,
			Now:   time.Now(),
		}
		s.renderPage(w, "login.html", data)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCfg.Cookie,
		Value:    string(result.SessionID),
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionCfg.TTL.Seconds()),
	})

	s.redirect(w, r, "/admin/")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.sessionCfg.Cookie)
	if err == nil {
		_ = s.logout.Logout(r.Context(), cdomain.SessionID(cookie.Value))
	}

	http.SetCookie(w, &http.Cookie{
		Name:   s.sessionCfg.Cookie,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	s.redirect(w, r, "/admin/login")
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	// Issue #139 (H1 audit, DREAD 8.0): sanitiza `next` antes de passar
	// para StartOIDC. Sem isso, atacante crafta
	// `?next=https://evil.com/phish` e o user aterrissa em evil.com
	// pós-login (open redirect, CWE-601).
	next := sanitizeNext(r.URL.Query().Get("next"))
	authURL, _, err := s.login.StartOIDC(r.Context(), next)
	if err != nil {
		data := PageData{Title: "Login", Error: "OIDC not available", Now: time.Now()}
		s.renderPage(w, "login.html", data)
		return
	}
	s.redirect(w, r, authURL)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		data := PageData{Title: "Login", Error: "Missing authorization code", Now: time.Now()}
		s.renderPage(w, "login.html", data)
		return
	}

	state := r.URL.Query().Get("state")
	if state == "" {
		data := PageData{Title: "Login", Error: "Missing state", Now: time.Now()}
		s.renderPage(w, "login.html", data)
		return
	}

	oidcState, err := s.stateStore.LoadState(r.Context(), state)
	if err != nil {
		data := PageData{Title: "Login", Error: "Invalid state", Now: time.Now()}
		s.renderPage(w, "login.html", data)
		return
	}

	result, err := s.login.LoginOIDC(r.Context(), code, oidcState.CodeVerifier, r.RemoteAddr, r.UserAgent())
	if err != nil {
		data := PageData{Title: "Login", Error: "OIDC login failed", Now: time.Now()}
		s.renderPage(w, "login.html", data)
		return
	}

	_ = s.stateStore.DeleteState(r.Context(), state)

	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCfg.Cookie,
		Value:    string(result.SessionID),
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.sessionCfg.TTL.Seconds()),
	})

	redirectAfter := sanitizeNext(oidcState.RedirectAfter)
	if redirectAfter == "/" {
		redirectAfter = "/admin/"
	}
	s.redirect(w, r, redirectAfter)
}

// cdomain.SessionID is used for session ID values
