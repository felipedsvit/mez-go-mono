// Package middleware — https_redirect.go: redirect HTTP→HTTPS.
//
// Issue #151 (Sprint 0B H12 audit): se TLS está ativo, requests HTTP
// plain devem retornar 301 para a versão HTTPS. Cobre 2 cenários:
//  1. TLS nativo no binário (MEZ_HTTP_TLS_CERT_FILE + _KEY_FILE)
//  2. Proxy reverso com X-Forwarded-Proto: https
//
// NOTA: este middleware NÃO faz TLS termination. Ele só redireciona.
// A terminação fica no proxy (recomendado em prod) ou no Go net/http
// (fallback).
package middleware

import "net/http"

// HTTPSRedirect retorna middleware que, se force=true, redireciona
// qualquer request HTTP para HTTPS via 301. Caso o request já esteja
// em HTTPS (TLS nativo ou X-Forwarded-Proto=https), passa direto.
//
// `host` é o hostname usado na URL HTTPS. Se vazio, usa r.Host.
func HTTPSRedirect(force bool, host string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !force {
				next.ServeHTTP(w, r)
				return
			}
			// Detecta se request já é HTTPS (via TLS nativo ou proxy header)
			if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
				next.ServeHTTP(w, r)
				return
			}
			// 301 Moved Permanently
			targetHost := host
			if targetHost == "" {
				targetHost = r.Host
			}
			target := "https://" + targetHost + r.URL.RequestURI()
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})
	}
}
