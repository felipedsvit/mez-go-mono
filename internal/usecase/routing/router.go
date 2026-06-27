// Package routing implementa o caso de uso de roteamento de conversas
// para o mez-go-mono (#37).
//
// O router da Fase 2 é SIMPLIFICADO vs mez-go (pai):
//   - sem ACD (queues, agents, skills, sticky, overflow, transbordo)
//   - sem configuração dinâmica de routing
//   - apenas defaultAgentID por tenant (campo em admin_users/scope tenant)
//
// A evolução para ACD completo acontece na Fase 5 (painel).
package routing

import (
	"context"
	"errors"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// Router atribui uma conversa a um agente (ou marca como unassigned).
type Router struct {
	log zerolog.Logger
}

// NewRouter cria o router.
func NewRouter(log zerolog.Logger) *Router {
	return &Router{log: log}
}

// Assign é a operação principal: dada uma mensagem, retorna o agentID
// responsável (ou vazio se unassigned). Como Fase 2 não tem ACD, retorna
// sempre string vazia — TODO Fase 5.
//
// A função é mantida na interface para que o reconciler e o bus consumer
// chamem o mesmo método; quando o ACD chegar, a lógica interna muda mas
// a assinatura fica.
func (r *Router) Assign(ctx context.Context, msg domain.Message) (string, error) {
	if msg.TenantID == "" {
		return "", errors.New("router: tenant_id required")
	}

	// Fase 2: unassigned. A Fase 5 lê defaultAgentID do tenant.
	// Mantemos log estruturado para o painel mostrar "unassigned" e
	// para detectar quando começar a popular via configuração.
	r.log.Debug().
		Str("tenant", string(msg.TenantID)).
		Str("message", string(msg.ID)).
		Str("channel", string(msg.Channel)).
		Msg("router: assign (fase 2: unassigned)")

	return "", nil
}
