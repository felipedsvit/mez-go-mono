// Package domain implementa os modelos de domínio do mez-go-mono.
//
// Aggregate root (issue #125, review DDD-Hex §3.7): Conversation é o AR
// do subdomínio "thread". Message é entidade dentro do AR. Contact é
// reference-by-ID (cross-aggregate, eventual consistency).
//
// Comportamento no domain (issue #125, review DDD-Hex §3.1): o domain
// deixou de ser anêmico. Os métodos abaixo carregam FSM guards e são a
// única forma de mutar Message/Conversation a partir do usecase.
package domain

import (
	"errors"
	"regexp"
)

// ErrInvalidTransition é retornado quando uma transição de status não é
// permitida pela FSM. Issue #125.
var ErrInvalidTransition = errors.New("transição de status inválida")

// ErrInvalidInput é retornado quando os parâmetros de uma factory ou
// método não passam validação coarse-grained.
var ErrInvalidInput = errors.New("parâmetro inválido")

// slugRegex valida slugs no padrão DNS-like (espelha core/admin/tenant.go).
// Movido para cá porque o domain.Tenant tem o mesmo invariante.
var slugRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)
