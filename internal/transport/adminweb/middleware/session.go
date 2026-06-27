package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
)

type contextKey string

const principalKey contextKey = "principal"

type SessionConfig struct {
	Resolver auth.SessionUseCase
	Cookie   string
	TTL      time.Duration
}

func Session(cfg SessionConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cfg.Cookie)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			session, err := cfg.Resolver.Resolve(r.Context(), admin.SessionID(cookie.Value))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			principal := admin.Principal{
				UserID: session.UserID,
				Email:  session.Email,
			}

			ctx := context.WithValue(r.Context(), principalKey, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func PrincipalFromContext(ctx context.Context) (admin.Principal, bool) {
	p, ok := ctx.Value(principalKey).(admin.Principal)
	return p, ok
}

func RequireAuth(loginPath string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, ok := PrincipalFromContext(r.Context())
			if !ok {
				http.Redirect(w, r, loginPath+"?next="+r.URL.Path, http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
