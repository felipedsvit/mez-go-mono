// Package messaging — list.go: use cases de leitura (issue #126).
//
// Skipping Use Cases (review DDD-Hex §3.2): o transport estava chamando
// port.ConversationRepo e port.MessageRepo direto para GETs. Este package
// expõe o caminho correto (Clean Architecture): transport → use case → repo.
//
// ListService é a fachada de leitura. AssignConversation e
// ResolveConversation são as variantes de escrita que aplicam o AR
// (issue #125) e o domain event (futuro).
package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// ListService agrupa as operações de leitura do subdomínio thread.
type ListService struct {
	convRepo port.ConversationRepo
	msgRepo  port.MessageRepo
	log      zerolog.Logger
}

// NewListService cria o service. Recebe os repos diretamente (não via
// TxRunner porque leituras não exigem RLS — RLS é enforced pelas policies
// do Postgres e pelo `mez.tenant_id` setado por RunInTenantTx no caller).
func NewListService(
	convRepo port.ConversationRepo,
	msgRepo port.MessageRepo,
	log zerolog.Logger,
) *ListService {
	return &ListService{convRepo: convRepo, msgRepo: msgRepo, log: log}
}

// ListConversations retorna as conversas do tenant. Issue #126.
//
// O caller DEVE estar dentro de um RunInTenantTx(tenantID) — o RLS
// fail-closed garante que apenas conversas do tenant sejam visíveis.
func (s *ListService) ListConversations(ctx context.Context, tenantID domain.TenantID) ([]domain.Conversation, error) {
	if tenantID == "" {
		return nil, errors.New("list conversations: tenant_id required")
	}
	convs, err := s.convRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return convs, nil
}

// ListMessages retorna as mensagens de uma conversa. Issue #126.
//
// O caller DEVE estar dentro de um RunInTenantTx(tenantID) — o RLS
// fail-closed garante isolamento.
func (s *ListService) ListMessages(ctx context.Context, tenantID domain.TenantID, conversationID domain.ConversationID) ([]domain.Message, error) {
	if tenantID == "" {
		return nil, errors.New("list messages: tenant_id required")
	}
	if conversationID == "" {
		return nil, errors.New("list messages: conversation_id required")
	}
	msgs, err := s.msgRepo.ListByConversation(ctx, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return msgs, nil
}

// AssignConversation atribui uma conversa a um agentID. Issue #126.
//
// Lê o AR do repo, aplica Conversation.Assign (FSM guard), persiste.
// Erros:
//   - domain.ErrInvalidTransition se a conversa está resolvida.
//   - port.ErrNotFound se a conversa não existe para o tenant.
func (s *ListService) AssignConversation(ctx context.Context, tenantID domain.TenantID, conversationID domain.ConversationID, agentID string) error {
	if tenantID == "" {
		return errors.New("assign conversation: tenant_id required")
	}
	if conversationID == "" {
		return errors.New("assign conversation: conversation_id required")
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("assign conversation: get: %w", err)
	}
	if err := conv.Assign(agentID); err != nil {
		return fmt.Errorf("assign conversation: %w", err)
	}
	if err := s.convRepo.Upsert(ctx, conv); err != nil {
		return fmt.Errorf("assign conversation: upsert: %w", err)
	}
	s.log.Info().
		Str("tenant", string(tenantID)).
		Str("conversation", string(conversationID)).
		Str("agent", agentID).
		Msg("conversation assigned")
	return nil
}

// ResolveConversation marca a conversa como resolvida. Issue #126.
//
// Lê o AR do repo, aplica Conversation.Resolve (FSM guard), persiste.
// Idempotente — re-resolver é no-op.
func (s *ListService) ResolveConversation(ctx context.Context, tenantID domain.TenantID, conversationID domain.ConversationID) error {
	if tenantID == "" {
		return errors.New("resolve conversation: tenant_id required")
	}
	if conversationID == "" {
		return errors.New("resolve conversation: conversation_id required")
	}
	conv, err := s.convRepo.Get(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("resolve conversation: get: %w", err)
	}
	if err := conv.Resolve(); err != nil {
		return fmt.Errorf("resolve conversation: %w", err)
	}
	if err := s.convRepo.Upsert(ctx, conv); err != nil {
		return fmt.Errorf("resolve conversation: upsert: %w", err)
	}
	s.log.Info().
		Str("tenant", string(tenantID)).
		Str("conversation", string(conversationID)).
		Msg("conversation resolved")
	return nil
}
