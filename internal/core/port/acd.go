package port

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// AgentRepo persiste agentes e responde queries de ACD.
type AgentRepo interface {
	// Candidates devolve os agentes do pool da fila. Pode ser filtração
	// (ex.: por skill) — a implementação decide.
	Candidates(ctx context.Context, queueID domain.QueueID) ([]domain.Agent, error)
	// List devolve os agentes do tenant (limit/offset para paginação).
	List(ctx context.Context, limit, offset int) ([]domain.Agent, error)
	// IncLoad aplica delta na carga do agente (-1 ao liberar, +1 ao atribuir).
	// Piso em zero (negativo clamp).
	IncLoad(ctx context.Context, id domain.AgentID, delta int) error
}

// QueueRepo persiste filas de atendimento.
type QueueRepo interface {
	// GetByID devolve a fila pelo id.
	GetByID(ctx context.Context, id domain.QueueID) (domain.Queue, error)
	// DefaultQueue devolve a fila catch-all do tenant (ou ErrNotFound).
	DefaultQueue(ctx context.Context) (domain.Queue, bool, error)
	// RequiredSkills devolve as skills mínimas exigidas pela fila.
	RequiredSkills(ctx context.Context, id domain.QueueID) ([]domain.RequiredSkill, error)
}

// StickyStore mantém a afinidade contato→agente (best-effort).
type StickyStore interface {
	// GetSticky devolve o agente preferido para o contato (se houver).
	GetSticky(ctx context.Context, tenant domain.TenantID, contactID domain.ContactID) (domain.AgentID, bool, error)
	// SetSticky registra a afinidade contato→agente.
	SetSticky(ctx context.Context, tenant domain.TenantID, contactID domain.ContactID, agentID domain.AgentID) error
}
