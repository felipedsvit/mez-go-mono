// Package api — request_id.go: middleware que injeta UUID v4 por request.
//
// Issue #153 (Sprint 0C M3 audit): correlação log↔response para
// debugging sem expor info interna no body. Cada request recebe um
// request_id (UUID v4) injetado no context, exposto no response
// (header X-Request-ID + campo error.response_id) e logado.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type reqIDKey struct{}

// RequestID middleware: gera UUID v4 por request, injeta no context
// + response header X-Request-ID. Se o cliente envia X-Request-ID
// (até 64 chars alfanum), reusa para tracing cross-service.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if !isValidRequestID(reqID) {
			reqID = newRequestID()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := context.WithValue(r.Context(), reqIDKey{}, reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extrai o request_id do context. Retorna "" se
// não estiver presente (ex: em tests que não passam pelo middleware).
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(reqIDKey{}).(string); ok {
		return v
	}
	return ""
}

// newRequestID gera UUID v4 hex (32 chars sem hífens).
func newRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback improvável: timestamp-based ID
		return "fallback-00000000"
	}
	// Set version (4) and variant (10xx) per RFC 4122
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b)
}

// isValidRequestID valida se um X-Request-ID do cliente é aceitável.
// Max 64 chars, apenas alfanum + hífens (UUID pattern). Previne log
// injection e DoS via headers gigantes.
func isValidRequestID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '-') {
			return false
		}
	}
	return true
}
