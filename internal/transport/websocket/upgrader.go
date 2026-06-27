// Package websocket — upgrader.go: factory do gorilla Upgrader com
// CheckOrigin config-driven (issue #129, audit C1 — DREAD 8.6).
//
// Por que uma factory (e não a `var Upgrader` global):
//
//   - O CheckOrigin precisa ser configurável por deploy (allowlist de
//     origens em `MEZ_WS_ALLOWED_ORIGINS`).
//   - Diferentes rotas podem precisar de regras diferentes (admin vs app).
//   - Testes querem bypass determinístico.
//
// A factory devolve um *websocket.Upgrader novo a cada chamada; o handler
// armazena o resultado e usa em ServeHTTP. O Upgrader global antigo foi
// removido — qualquer referência passa a vir via `Handler.upgrader`.
package websocket

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
)

// UpgraderConfig configura o factory de Upgrader.
type UpgraderConfig struct {
	// AllowedOrigins: lista de origens (scheme://host[:port]) aceitas.
	// Apenas scheme http/https é aceito. Comparação case-insensitive
	// no host. Origem ausente é rejeitada exceto se a request for
	// same-origin (Host == Origin host).
	// Vazio = nenhuma origem cross-origin aceita (production hardening).
	AllowedOrigins []string

	// AllowSameOrigin: se true, requests sem Origin (curl, Postman,
	// algumas versões do go client) são aceitas se Host == Origin
	// ausente (não cross-origin). Default: false em production.
	AllowSameOrigin bool

	// TrustedProxy: se true, headers X-Forwarded-Proto/-Host/-Origin
	// são honrados (atrás de reverse proxy confiável).
	TrustedProxy bool
}

// NewUpgrader cria um *websocket.Upgrader com CheckOrigin baseado em cfg.
//
// Comportamento de CheckOrigin (r):
//  1. Se TrustedProxy, normaliza Origin contra X-Forwarded-Origin.
//  2. Se r.Header.Get("Origin") == "" e AllowSameOrigin: aceita.
//  3. Se Origin == "" e !AllowSameOrigin: rejeita.
//  4. Parse Origin. Se inválido: rejeita.
//  5. Compara scheme+host (case-insensitive) contra AllowedOrigins. Match: aceita.
//  6. Caso contrário: rejeita.
//
// Same-origin: se Origin.host == r.Host (e scheme coincide), aceita
// mesmo sem AllowSameOrigin explícito, pois o Origin é verificável
// contra o request target.
func NewUpgrader(cfg UpgraderConfig) *websocket.Upgrader {
	allowed := normalizeOrigins(cfg.AllowedOrigins)
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     makeCheckOrigin(allowed, cfg.AllowSameOrigin, cfg.TrustedProxy),
	}
}

// makeCheckOrigin devolve a função CheckOrigin fechada sobre os parâmetros
// normalizados. Mantida interna para facilitar testes.
func makeCheckOrigin(allowed map[string]struct{}, allowSameOrigin, trustedProxy bool) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if trustedProxy {
			if v := r.Header.Get("X-Forwarded-Origin"); v != "" {
				origin = v
			}
		}

		// Sem Origin: depende de AllowSameOrigin.
		if origin == "" {
			return allowSameOrigin
		}

		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.Scheme == "" {
			return false
		}

		// Apenas http/https. Bloqueia javascript:, data:, file:, etc.
		if u.Scheme != "http" && u.Scheme != "https" {
			return false
		}

		key := originKey(u.Scheme, u.Host)

		// Same-origin (Origin.host == r.Host) é sempre aceito se o
		// scheme bater (request target é https em production). Isso
		// evita bloquear o admin que abre o painel na mesma origem.
		if strings.EqualFold(u.Host, r.Host) {
			// Para same-origin, exigimos que o scheme do Origin
			// coincida com o scheme inferido de r.TLS.
			originScheme := "http"
			if r.TLS != nil {
				originScheme = "https"
			}
			if trustedProxy {
				if v := r.Header.Get("X-Forwarded-Proto"); v == "https" || v == "http" {
					originScheme = v
				}
			}
			if u.Scheme == originScheme {
				return true
			}
		}

		// Cross-origin: precisa estar na allowlist (scheme+host).
		_, ok := allowed[key]
		return ok
	}
}

// originKey produz a chave canônica para o mapa de allowlist.
// "https" + "app.example.com" → "https://app.example.com".
func originKey(scheme, host string) string {
	return strings.ToLower(scheme) + "://" + strings.ToLower(host)
}

// normalizeOrigins parseia cada origem e mantém apenas as que têm
// scheme http/https e host não-vazio. Esquemas não-web (file://,
// javascript:, data:, ftp://, ws://, wss://) são descartados — esses
// nunca são origens válidas para upgrade WS.
func normalizeOrigins(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, o := range in {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || u.Host == "" {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		out[originKey(u.Scheme, u.Host)] = struct{}{}
	}
	return out
}
