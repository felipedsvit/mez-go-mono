// Package middleware contém middlewares HTTP para a API transport.
//
// BearerAuth valida JWT HS256, extrai tenant_id da claim "tenant_id" e
// injeta no contexto (lido por api.TenantFromContext).
//
// NOTA Fase 2: implementação simplificada (HS256 com MEZ_API_JWT_SECRET).
// A versão OIDC/JWKS chega na Fase 5.
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/transport/http/api"
)

// BearerAuthConfig configura o middleware.
type BearerAuthConfig struct {
	Secret []byte // MEZ_API_JWT_SECRET
}

// BearerAuth retorna um middleware que valida Authorization: Bearer <jwt>.
// Em caso de sucesso, injeta tenant_id no contexto. Em falha, 401.
func BearerAuth(cfg BearerAuthConfig, log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if authz == "" {
				http.Error(w, `{"error":"unauthorized","message":"missing Authorization header"}`,
					http.StatusUnauthorized)
				return
			}
			parts := strings.SplitN(authz, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, `{"error":"unauthorized","message":"invalid Authorization scheme"}`,
					http.StatusUnauthorized)
				return
			}

			tenantID, err := parseAndValidateJWT(parts[1], cfg.Secret)
			if err != nil {
				log.Warn().Err(err).Msg("bearer auth: invalid token")
				http.Error(w, `{"error":"unauthorized","message":"invalid token"}`,
					http.StatusUnauthorized)
				return
			}

			ctx := api.ContextWithTenant(r.Context(), tenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Claims é o payload mínimo esperado.
type Claims struct {
	TenantID string `json:"tenant_id"`
	Sub      string `json:"sub,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
}

// parseAndValidateJWT decodifica e valida um JWT HS256.
func parseAndValidateJWT(token string, secret []byte) (domain.TenantID, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed jwt: expected 3 parts")
	}

	// Header (não validamos tipo/alg em profundidade — assumimos HS256).
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("malformed jwt: header decode")
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", errors.New("malformed jwt: header parse")
	}
	if header.Alg != "HS256" {
		return "", errors.New("unsupported alg: " + header.Alg)
	}

	// Signature.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errors.New("malformed jwt: signature decode")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", errors.New("invalid signature")
	}

	// Payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("malformed jwt: payload decode")
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", errors.New("malformed jwt: payload parse")
	}
	if claims.TenantID == "" {
		return "", errors.New("missing tenant_id claim")
	}
	return domain.TenantID(claims.TenantID), nil
}

// RequireScope é um middleware RBAC simples: verifica se a claim "scope"
// contém a permissão necessária. Para Fase 2, a claim é uma string
// space-separated (formato OAuth2).
type RequireScopeConfig struct {
	Scope string
}

// RequireScope retorna um middleware que exige o scope na claim "scope".
// NOTA: a claim "scope" é parseada do JWT no BearerAuth; como a versão
// atual só injeta tenant_id, este middleware é um placeholder para Fase 5.
// Por ora, sempre passa.
func RequireScope(cfg RequireScopeConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO Fase 5: extrair scope do JWT e validar.
			next.ServeHTTP(w, r)
		})
	}
}
