package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

type CSRFConfig struct {
	CookieName string
	HeaderName string
	FormField  string
	Exempt     []string
	Secure     bool
	Path       string
}

func DefaultCSRFConfig() CSRFConfig {
	return CSRFConfig{
		CookieName: "XSRF-TOKEN",
		HeaderName: "X-CSRF-Token",
		FormField:  "csrf_token",
		Secure:     false,
		Path:       "/",
	}
}

func CSRF(cfg CSRFConfig) func(http.Handler) http.Handler {
	exempt := make(map[string]bool)
	for _, p := range cfg.Exempt {
		exempt[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if exempt[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				if _, err := r.Cookie(cfg.CookieName); err != nil {
					token := generateCSRFToken()
					http.SetCookie(w, &http.Cookie{
						Name:     cfg.CookieName,
						Value:    token,
						Path:     cfg.Path,
						Secure:   cfg.Secure,
						HttpOnly: false,
						SameSite: http.SameSiteStrictMode,
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(cfg.CookieName)
			if err != nil {
				http.Error(w, "CSRF cookie missing", http.StatusForbidden)
				return
			}

			// Token can come from header (XHR/fetch) or form field (HTML form POST).
			tokenValue := r.Header.Get(cfg.HeaderName)
			if tokenValue == "" && cfg.FormField != "" {
				tokenValue = r.FormValue(cfg.FormField)
			}
			if tokenValue == "" {
				http.Error(w, "CSRF token missing", http.StatusForbidden)
				return
			}

			if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(tokenValue)) != 1 {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("x", 43)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
