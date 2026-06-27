// Package middleware contém middlewares HTTP para a API transport.
//
// BearerAuth valida JWT HS256, extrai tenant_id da claim "tenant_id" e
// injeta no contexto (lido por api.TenantFromContext).
//
// Issue #130 (C2 audit, DREAD 8.4): valida \`exp\` e \`nbf\` rigorosamente.
// Tokens sem \`exp\` (ou com \`exp=0\`, 1970) são rejeitados.
//
// A versão OIDC/JWKS chega na Fase 5.
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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

			tenantID, claims, err := parseAndValidateJWT(parts[1], cfg.Secret)
			if err != nil {
				log.Warn().Err(err).Msg("bearer auth: invalid token")
				http.Error(w, `{"error":"unauthorized","message":"invalid token"}`,
					http.StatusUnauthorized)
				return
			}

			ctx := api.ContextWithTenant(r.Context(), tenantID)
			// Issue #134 (C6 audit): injeta actor (sub/email) do JWT no
			// contexto para que handlers de backup/restore/reset possam
			// usar como Actor.ID/Actor.Email em audit logs (em vez do
			// header X-Admin-Email controlável pelo atacante).
			ctx = api.ContextWithActor(ctx, api.Actor{
				ID:    claims.Sub,
				Email: claims.Email,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Claims é o payload mínimo esperado.
type Claims struct {
	TenantID string `json:"tenant_id"`
	Sub      string `json:"sub,omitempty"`
	Email    string `json:"email,omitempty"`
	Exp      int64  `json:"exp,omitempty"`
	Nbf      int64  `json:"nbf,omitempty"`
	Iat      int64  `json:"iat,omitempty"`
}

// ParseResult contém o resultado de parseAndValidateJWT.
type ParseResult struct {
	TenantID domain.TenantID
	Claims   Claims
}

// parseAndValidateJWT decodifica e valida um JWT HS256.
//
// Validações:
//   - alg == HS256 (rejeita alg=none, RS256, etc)
//   - signature HMAC-SHA256
//   - tenant_id claim não-vazio
//   - exp claim presente e futuro (rejeita exp=0 / 1970)
//   - nbf claim, se presente, não é futuro
//   - iat sanity (não muito no futuro, ≤ 5min skew)
func parseAndValidateJWT(token string, secret []byte) (domain.TenantID, *Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", nil, errors.New("malformed jwt: expected 3 parts")
	}

	// Header (não validamos tipo/alg em profundidade — assumimos HS256).
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", nil, errors.New("malformed jwt: header decode")
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", nil, errors.New("malformed jwt: header parse")
	}
	if header.Alg != "HS256" {
		return "", nil, errors.New("unsupported alg: " + header.Alg)
	}

	// Signature.
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", nil, errors.New("malformed jwt: signature decode")
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", nil, errors.New("invalid signature")
	}

	// Payload.
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, errors.New("malformed jwt: payload decode")
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", nil, errors.New("malformed jwt: payload parse")
	}
	if claims.TenantID == "" {
		return "", nil, errors.New("missing tenant_id claim")
	}

	// exp: obrigatório e futuro. Issue #130 (C2 audit).
	now := time.Now().Unix()
	if claims.Exp == 0 {
		return "", nil, errors.New("missing exp claim")
	}
	if claims.Exp <= now {
		return "", nil, fmt.Errorf("token expired (exp=%d, now=%d)", claims.Exp, now)
	}
	// nbf: se presente, não pode ser futuro (com skew de 60s).
	if claims.Nbf > now+60 {
		return "", nil, fmt.Errorf("token not yet valid (nbf=%d, now=%d)", claims.Nbf, now)
	}
	// iat sanity: se presente, não pode ser muito no futuro (clock skew).
	if claims.Iat > 0 && claims.Iat > now+300 {
		return "", nil, fmt.Errorf("iat too far in future (iat=%d, now=%d)", claims.Iat, now)
	}

	return domain.TenantID(claims.TenantID), &claims, nil
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
