// Package templates contém os components templ (codegen) que renderizam
// o painel admin e a área /app/*. A migração de html/template (Fase 2
// da 000_FIXES.md, decision revisto) troca o runtime de templates por
// templ puro, com props tipadas e zero funcmap. As funções antes
// registradas via template.FuncMap (now/truncate/hasPerm) são agora
// pré-computadas nos props ou expostas como métodos nos tipos.
package templates

import (
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// PageData carrega dados comuns a todas as páginas: identidade do
// principal, mensagens efêmeras (Error/Success), timestamp pré-computado
// e CSRFToken. Os props específicos de cada página (Tenants, Users, etc)
// ficam em structs dedicadas que embutem ou complementam PageData.
type PageData struct {
	Title      string
	Principal  admin.Principal
	Error      string
	Success    string
	Now        time.Time
	StaticBase string
	CSRFToken  string
}

// Truncate devolve s truncado em n caracteres com sufixo "...".
// Substitui a funcmap `truncate` do runtime html/template.
func Truncate(s string, n int) string {
	if n <= 3 {
		return s
	}
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

// FormatDate formata um timestamp pré-computado. Substitui format
// inline nos templates. Centralizar aqui evita inconsistência entre
// páginas.
func FormatDate(t time.Time, layout string) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(layout)
}

// HasPerm encapsula Principal.HasPermission com tratamento de nil.
func (p PageData) HasPerm(perm admin.Permission) bool {
	if p.Principal.Permissions == nil {
		return false
	}
	_, ok := p.Principal.Permissions[perm]
	return ok
}

// IsPlatform identifica principals com escopo plataforma, para mostrar
// links admin (users, audit, secrets) que não fazem sentido para
// tenant owners/agents.
func (p PageData) IsPlatform() bool {
	return p.Principal.IsPlatform()
}
