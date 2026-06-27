// Package inbox é o usecase de atendimento (inbox de agente) consumido pelo
// painel admin. É uma camada fina de leitura sobre o data plane (conversas +
// mensagens, escopadas por tenant via RunInTenantTx/RLS) que reaproveita o
// routing.Router para as ações de atribuição/transbordo/encerramento.
//
// As leituras rodam dentro de uma tenant-tx (RLS isola por tenant); as
// mutações delegam ao Router (que já abre sua própria tx com FOR UPDATE).
package inbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/routing"
)

// Erros sentinela exportados para erros.Is do transport.
var (
	// ErrConvNotFound: a conversa não existe (ou não é deste tenant — RLS
	// devolve ErrNotFound para isolar).
	ErrConvNotFound = errors.New("conversa não encontrada")
)

// Service expõe as operações da inbox para o transport. O router carrega a
// lógica de atribuição (manual e, se ligado com WithACD, automática); aqui
// usamos só as ações manuais no caso padrão.
type Service struct {
	tx     port.TxRunner
	convs  port.ConversationRepo
	msgs   port.MessageRepo
	agents port.AgentRepo
	router *routing.Router
}

// NewService monta o serviço.
func NewService(tx port.TxRunner, convs port.ConversationRepo, msgs port.MessageRepo, agents port.AgentRepo, router *routing.Router) *Service {
	return &Service{tx: tx, convs: convs, msgs: msgs, agents: agents, router: router}
}

// ListConversations devolve uma página das conversas do tenant (mais recentes
// primeiro, conforme o repo). A filtragem por estado fica no transport.
func (s *Service) ListConversations(ctx context.Context, tenant domain.TenantID, limit, offset int) ([]domain.Conversation, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}
	var out []domain.Conversation
	err := s.tx.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		c, err := s.convs.ListByTenant(ctx, tenant)
		if err != nil {
			return err
		}
		out = paginate(c, limit, offset)
		return nil
	})
	return out, err
}

// Thread devolve a conversa e suas mensagens (histórico paginado, mais
// recentes primeiro).
func (s *Service) Thread(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID, limit, offset int) (domain.Conversation, []domain.Message, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	var (
		conv domain.Conversation
		msgs []domain.Message
	)
	err := s.tx.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		c, err := s.convs.Get(ctx, convID)
		if err != nil {
			if errors.Is(err, port.ErrNotFound) {
				return ErrConvNotFound
			}
			return err
		}
		if c.TenantID != tenant {
			// RLS deve impedir isso; defesa em profundidade.
			return ErrConvNotFound
		}
		conv = *c
		m, err := s.msgs.ListByConversation(ctx, convID)
		if err != nil {
			return err
		}
		msgs = paginateMessages(m, limit, offset)
		return nil
	})
	return conv, msgs, err
}

// ListAgents devolve os agentes do tenant (para o seletor de atribuição).
// Inerte sem ACD ligado (agents == nil).
func (s *Service) ListAgents(ctx context.Context, tenant domain.TenantID) ([]domain.Agent, error) {
	if s.agents == nil {
		return nil, nil
	}
	var out []domain.Agent
	err := s.tx.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		a, err := s.agents.List(ctx, 200, 0)
		if err != nil {
			return err
		}
		out = a
		return nil
	})
	return out, err
}

// Assign atribui (ou transfere) a conversa a um agente.
func (s *Service) Assign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID, agentID string) error {
	if agentID == "" {
		return errors.New("inbox: agentID required")
	}
	return s.router.Assign(ctx, tenant, convID, agentID)
}

// Unassign devolve a conversa à fila (sem agente atribuído).
func (s *Service) Unassign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) error {
	return s.router.Unassign(ctx, tenant, convID)
}

// Resolve encerra a conversa.
func (s *Service) Resolve(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) error {
	return s.router.Resolve(ctx, tenant, convID)
}

// AutoAssign distribui a conversa ao melhor agente da fila (se ACD estiver
// ligado). Inerte sem dependências.
func (s *Service) AutoAssign(ctx context.Context, tenant domain.TenantID, convID domain.ConversationID) (string, error) {
	agent, err := s.router.AutoAssign(ctx, tenant, convID)
	if err != nil {
		return "", fmt.Errorf("inbox auto_assign: %w", err)
	}
	return agent, nil
}

// --- Helpers ---

func paginate(items []domain.Conversation, limit, offset int) []domain.Conversation {
	// Ordena por UpdatedAt desc (mais recente primeiro).
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].UpdatedAt.After(items[i].UpdatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if offset >= len(items) {
		return []domain.Conversation{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func paginateMessages(items []domain.Message, limit, offset int) []domain.Message {
	// Ordena por CreatedAt desc (mais recente primeiro).
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
	if offset >= len(items) {
		return []domain.Message{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
