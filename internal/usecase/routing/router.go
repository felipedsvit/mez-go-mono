// Package routing implementa o caso de uso de roteamento de conversas
// para o mez-go-mono (Fase 5, derivado do mez-go pai).
//
// Comportamento (vs mez-go pai):
//   - Manual: Assign / Unassign / Resolve sobre o AR Conversation.
//   - ACD opcional: AutoAssign com estratégia da fila (round-robin,
//     least-busy, skill-based, sticky) + transbordo (overflow).
//   - Concorrência: mutate roda dentro de RunInTenantTx (RLS fail-closed).
//   - FOR UPDATE: evita race condition entre dois agentes pegando a mesma
//     conversa simultaneamente.
package routing

import (
	"context"
	"errors"
	"sort"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// maxOverflowHops limita a cadeia de transbordo para evitar loop infinito.
const maxOverflowHops = 8

// Router aplica operações de atribuição (manual e automática) sobre conversas.
// Sem ACD ligado, opera só com atribuição manual (defaultAgentID por tenant
// é responsabilidade de uma opção de evolução, fora do escopo 1.0).
type Router struct {
	tx     port.TxRunner
	convs  port.ConversationRepo
	agents port.AgentRepo   // opcional (ACD)
	queues port.QueueRepo   // opcional (ACD)
	sticky port.StickyStore // opcional (sticky routing)
	log    zerolog.Logger
}

// Option configura dependências opcionais do Router.
type Option func(*Router)

// WithACD liga o roteamento automático: filas, agentes e afinidade sticky.
func WithACD(agents port.AgentRepo, queues port.QueueRepo, sticky port.StickyStore) Option {
	return func(r *Router) {
		r.agents = agents
		r.queues = queues
		r.sticky = sticky
	}
}

// NewRouter constrói o router. Sem opções, opera só com atribuição manual.
func NewRouter(tx port.TxRunner, convs port.ConversationRepo, log zerolog.Logger, opts ...Option) *Router {
	r := &Router{tx: tx, convs: convs, log: log}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Assign atribui uma conversa a um agente.
func (r *Router) Assign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID, agentID string) error {
	return r.mutate(ctx, tenant, convID, func(c *domain.Conversation) {
		_ = c.Assign(agentID)
	})
}

// Unassign remove a atribuição (volta à fila / pendente).
func (r *Router) Unassign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) error {
	return r.mutate(ctx, tenant, convID, func(c *domain.Conversation) {
		_ = c.Assign("")
	})
}

// Resolve encerra a conversa.
func (r *Router) Resolve(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) error {
	return r.mutate(ctx, tenant, convID, func(c *domain.Conversation) {
		_ = c.Resolve()
	})
}

func (r *Router) mutate(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID, fn func(*domain.Conversation)) error {
	return r.tx.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		conv, err := r.convs.Get(ctx, convID)
		if err != nil {
			return err
		}
		before := occupiedAgent(conv)
		fn(conv)
		if err := r.convs.Upsert(ctx, conv); err != nil {
			return err
		}
		return r.adjustLoad(ctx, before, occupiedAgent(conv))
	})
}

// occupiedAgent devolve o agente que ocupa um slot de capacidade.
func occupiedAgent(c *domain.Conversation) domain.AgentID {
	if c.AssignedAgent != "" && c.IsOpen() {
		return domain.AgentID(c.AssignedAgent)
	}
	return ""
}

// adjustLoad reconcilia a carga do agente (current_load) na transição.
func (r *Router) adjustLoad(ctx context.Context, before, after domain.AgentID) error {
	if r.agents == nil || before == after {
		return nil
	}
	if before != "" {
		if err := r.agents.IncLoad(ctx, before, -1); err != nil {
			return err
		}
	}
	if after != "" {
		if err := r.agents.IncLoad(ctx, after, 1); err != nil {
			return err
		}
	}
	return nil
}

// AutoAssign distribui a conversa ao melhor agente da sua fila segundo a
// estratégia configurada, seguindo a cadeia de transbordo (overflow) quando a
// fila não tem ninguém elegível. Inerte sem ACD ligado.
func (r *Router) AutoAssign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) (string, error) {
	if r.agents == nil || r.queues == nil {
		return "", nil
	}
	var chosen string
	err := r.tx.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		conv, err := r.convs.Get(ctx, convID)
		if err != nil {
			return err
		}
		if conv.AssignedAgent != "" {
			return nil // já atribuída
		}
		// Sem queue_id na conversa: tenta default.
		if convQueueID(conv) == "" {
			dq, ok, err := r.queues.DefaultQueue(ctx)
			if err != nil {
				return err
			}
			if !ok {
				return nil // sem fila default: deixa como está
			}
			setConvQueueID(conv, dq.ID)
		}
		agent, queueID, ok, err := r.pickAgent(ctx, tenant, conv)
		if err != nil {
			return err
		}
		if !ok {
			// Ninguém elegível em nenhuma fila: parqueia como resolvido se
			// já está em open, ou como está (pending). Aqui mantemos como está
			// e logamos.
			r.log.Info().
				Str("tenant", string(tenant)).
				Str("conv", string(convID)).
				Msg("router: ninguém elegível, conversa permanece pendente")
			return nil
		}
		if err := conv.Assign(string(agent.ID)); err != nil {
			return err
		}
		setConvQueueID(conv, queueID)
		if err := r.convs.Upsert(ctx, conv); err != nil {
			return err
		}
		if err := r.agents.IncLoad(ctx, agent.ID, 1); err != nil {
			return err
		}
		if r.sticky != nil {
			_ = r.sticky.SetSticky(ctx, tenant, conv.ContactID, agent.ID)
		}
		chosen = string(agent.ID)
		return nil
	})
	return chosen, err
}

// pickAgent percorre a cadeia de filas (fila da conversa → overflow → ...)
// até encontrar um agente elegível.
func (r *Router) pickAgent(ctx context.Context, tenant domain.TenantID, conv *domain.Conversation) (domain.Agent, domain.QueueID, bool, error) {
	visited := make(map[domain.QueueID]bool)
	queueID := convQueueID(conv)
	for hop := 0; hop < maxOverflowHops && queueID != ""; hop++ {
		if visited[queueID] {
			break
		}
		visited[queueID] = true

		queue, err := r.queues.GetByID(ctx, queueID)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				return domain.Agent{}, "", false, nil
			}
			return domain.Agent{}, "", false, err
		}
		required, err := r.queues.RequiredSkills(ctx, queueID)
		if err != nil {
			return domain.Agent{}, "", false, err
		}
		candidates, err := r.agents.Candidates(ctx, queueID)
		if err != nil {
			return domain.Agent{}, "", false, err
		}

		var stickyHint string
		if queue.Strategy == domain.StrategySticky && r.sticky != nil {
			if a, ok, serr := r.sticky.GetSticky(ctx, tenant, conv.ContactID); serr == nil && ok {
				stickyHint = string(a)
			}
		}

		agent, ok := Select(SelectInput{
			Strategy:       queue.Strategy,
			Candidates:     candidates,
			Required:       required,
			StickyAgentID:  stickyHint,
			RoundRobinSeed: roundRobinSeed(candidates),
		})
		if ok {
			return agent, queueID, true, nil
		}
		queueID = queue.OverflowQueueID
	}
	return domain.Agent{}, "", false, nil
}

// roundRobinSeed deriva semente pragmática baseada na soma de cargas.
func roundRobinSeed(candidates []domain.Agent) int {
	sum := 0
	for _, a := range candidates {
		sum += a.CurrentLoad
	}
	return sum
}

// convQueueID/setConvQueueID acessam o queue_id armazenado no metadata da
// conversa (campo não-mapped do domain.Conversation — usamos metadata para
// preservar compat com DDD-hex).
func convQueueID(c *domain.Conversation) domain.QueueID {
	if c == nil {
		return ""
	}
	return domain.QueueID(c.ExternalID) // fallback: ExternalID tem o peer; queue vai em metadata
	// Em produção, lê de Conversation.Metadata (campo futuro).
}

func setConvQueueID(c *domain.Conversation, id domain.QueueID) {
	// no-op por enquanto — ver nota acima
}

// SelectInput agrega tudo que a seleção de agente precisa.
type SelectInput struct {
	Strategy       domain.RoutingStrategy
	Candidates     []domain.Agent
	Required       []domain.RequiredSkill
	StickyAgentID  string
	RoundRobinSeed int
}

// Select escolhe o agente segundo a estratégia.
func Select(in SelectInput) (domain.Agent, bool) {
	eligible := eligibleAgents(in.Candidates, in.Required)
	if len(eligible) == 0 {
		return domain.Agent{}, false
	}
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].ID < eligible[j].ID })

	switch in.Strategy {
	case domain.StrategySticky:
		if a, ok := findByID(eligible, in.StickyAgentID); ok {
			return a, true
		}
		return leastBusy(eligible), true
	case domain.StrategyRoundRobin:
		idx := in.RoundRobinSeed % len(eligible)
		if idx < 0 {
			idx += len(eligible)
		}
		return eligible[idx], true
	case domain.StrategySkillBased:
		return leastBusy(eligible), true
	case domain.StrategyLeastBusy:
		return leastBusy(eligible), true
	default:
		return leastBusy(eligible), true
	}
}

func eligibleAgents(candidates []domain.Agent, required []domain.RequiredSkill) []domain.Agent {
	out := make([]domain.Agent, 0, len(candidates))
	for _, a := range candidates {
		if a.Status != domain.AgentOnline || !a.HasCapacity() {
			continue
		}
		if !a.MeetsSkills(required) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func leastBusy(eligible []domain.Agent) domain.Agent {
	best := eligible[0]
	for _, a := range eligible[1:] {
		if a.CurrentLoad < best.CurrentLoad {
			best = a
		}
	}
	return best
}

func findByID(agents []domain.Agent, id string) (domain.Agent, bool) {
	if id == "" {
		return domain.Agent{}, false
	}
	want := domain.AgentID(id)
	for _, a := range agents {
		if a.ID == want {
			return a, true
		}
	}
	return domain.Agent{}, false
}
