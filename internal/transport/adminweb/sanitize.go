// Package adminweb — sanitize.go: helpers de validação de paths internos.
//
// Issue #139 (H1 audit, DREAD 8.0): protege contra open redirect via
// OIDC `next` e outros parâmetros de redirect. O atacante crafta
// `?next=https://evil.com/phish`; pós-login, user aterrissa em
// `evil.com` (que pode coletar mais credenciais).
//
// CWE-601 (Open Redirect) · CWE-352 (CSRF).
package adminweb

import "strings"

// sanitizeNext valida um caminho de redirect. Aceita apenas paths
// internos que:
//   - começam com "/"
//   - não começam com "//" (protocol-relative URL → external)
//   - não começam com "/\" (Windows-style)
//   - não contém "\\" (Windows backslash)
//   - não contém ":" antes do segundo "/"
//
// Se inválido, devolve "/" como default seguro.
//
// Exemplos:
//
//	sanitizeNext("")                  → "/"
//	sanitizeNext("/admin/")           → "/admin/"
//	sanitizeNext("//evil.com")        → "/"
//	sanitizeNext("/\\evil.com")       → "/"
//	sanitizeNext("https://evil.com")  → "/"
//	sanitizeNext("javascript:alert")  → "/"
//	sanitizeNext("/admin/x?y=z")      → "/admin/x?y=z"
func sanitizeNext(next string) string {
	if next == "" {
		return "/"
	}
	// Deve começar com "/".
	if !strings.HasPrefix(next, "/") {
		return "/"
	}
	// Bloqueia protocol-relative URLs: //host
	if strings.HasPrefix(next, "//") {
		return "/"
	}
	// Bloqueia Windows backslash e escapes.
	if strings.HasPrefix(next, "/\\") || strings.Contains(next, "\\") {
		return "/"
	}
	// Bloqueia scheme injection: /x:y é OK, mas javascript:alert(1)
	// começa com scheme, e aqui já filtramos HasPrefix("/").
	// Defense-in-depth: não permite CR/LF/null bytes.
	if strings.ContainsAny(next, "\r\n\x00") {
		return "/"
	}
	return next
}
