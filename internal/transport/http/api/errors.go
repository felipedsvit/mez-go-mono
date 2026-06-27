// Package api — errors.go: error response writer que não vaza detalhes
// internos + request_id middleware para correlação.
//
// Issue #153 (Sprint 0C M3 audit, DREAD 4.4): handlers retornavam
// err.Error() direto no body. Atacante aprende schema do DB ("pq:
// relation 'mez.users' does not exist"), paths internos, stack traces.
//
// Fix: WriteError(w, status, code, err) retorna JSON com apenas
// {error, request_id}. err.Error() vai pro log (server-side) com
// request_id, e para audit row em 5xx.
//
// Catálogo de codes (não expor info interna):
//
//	code_not_found       — 404
//	code_validation      — 400
//	code_unauthorized    — 401
//	code_forbidden       — 403
//	code_rate_limited    — 429
//	code_idor            — 403 (tentativa cross-tenant)
//	code_tenant_mismatch — 403
//	code_internal        — 500 (genérico)
package api

import (
	"encoding/json"
	"log"
	"net/http"
)

// ErrorCode é o code público que vai no body do response. Não inclui
// info do erro original.
type ErrorCode string

const (
	CodeNotFound       ErrorCode = "code_not_found"
	CodeValidation     ErrorCode = "code_validation"
	CodeUnauthorized   ErrorCode = "code_unauthorized"
	CodeForbidden      ErrorCode = "code_forbidden"
	CodeRateLimited    ErrorCode = "code_rate_limited"
	CodeIDOR           ErrorCode = "code_idor"
	CodeTenantMismatch ErrorCode = "code_tenant_mismatch"
	CodeInternal       ErrorCode = "code_internal"
	CodeConflict       ErrorCode = "code_conflict"
	CodeGone           ErrorCode = "code_gone"
)

// ErrorResponse é o shape JSON retornado ao cliente.
type ErrorResponse struct {
	Error     string    `json:"error"`
	RequestID string    `json:"request_id,omitempty"`
	Code      ErrorCode `json:"code"`
}

// WriteError serializa o erro em JSON seguro e loga internamente.
// request_id vem do context (injetado por RequestID middleware).
//
// Regra: nunca inclua err.Error() no body. Loga server-side com
// request_id para correlação.
func WriteError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, err error) {
	reqID := RequestIDFromContext(r.Context())

	resp := ErrorResponse{
		Error:     messageForCode(code, status),
		RequestID: reqID,
		Code:      code,
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		// Last-resort: se JSON encode falhar, retorna texto simples.
		// Não vaza info do erro original.
		http.Error(w, `{"error":"internal"}`, status)
	}

	// Log server-side com err real + request_id
	if err != nil {
		log.Printf("api error: request_id=%s status=%d code=%s error=%q",
			reqID, status, code, err.Error())
	}
}

// messageForCode retorna mensagem genérica baseada no code. Nunca inclui
// err.Error() — isso fica no log.
func messageForCode(code ErrorCode, status int) string {
	switch code {
	case CodeNotFound:
		return "resource not found"
	case CodeValidation:
		return "invalid request"
	case CodeUnauthorized:
		return "unauthorized"
	case CodeForbidden:
		return "forbidden"
	case CodeRateLimited:
		return "rate limited"
	case CodeIDOR:
		return "forbidden"
	case CodeTenantMismatch:
		return "tenant mismatch"
	case CodeConflict:
		return "conflict"
	case CodeGone:
		return "resource gone"
	default:
		// 5xx → "internal error"; 4xx → "bad request"
		if status >= 500 {
			return "internal error"
		}
		return "bad request"
	}
}
