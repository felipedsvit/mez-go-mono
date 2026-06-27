package port

import (
	"context"
	"errors"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

// ErrCredentialsNotFound é o sentinel unificado para "(tenant, channel)
// não tem credencial configurada". Vive em port porque é referenciado por:
//   - usecase/secrets (Keyring.ResolveCredentials)
//   - adapter/webhook/secrets (EnvCredentials, EnvMetaSecrets, EnvTelegramSecrets)
//   - tests
//
// Carryover: era declarado em 3 lugares (#119). Consolidado.
var ErrCredentialsNotFound = errors.New("credenciais não configuradas")

// TxRunner executes a function inside a tenant-scoped transaction.
// The transaction is propagated via context; repositories extract it with the
// package-level helper in the postgres adapter.
type TxRunner interface {
	RunInTenantTx(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context) error) error
	RunAsPlatform(ctx context.Context, actor string, fn func(ctx context.Context) error) error
}

// Convenção de repositórios (issue #124, review DDD-Hex §3.9):
//
// Por pragmatismo Go/SQL, mantemos 1 repo por entidade (ContactRepo,
// ConversationRepo, MessageRepo). Isso diverge do skill "Repository per
// Aggregate" mas é justificado pelas queries SQL distintas por tabela.
//
// O agregado (issue #125) é `Conversation` — raiz que carrega Messages.
// A consequência é:
//
//   - Repos fornecem operações CRUD coarse-grained por entidade.
//   - Comportamento de domínio (Open/NewInboundMessage/Assign/Resolve) é
//     método do agregado, não do repo (issue #125).
//   - Operações que cruzam agregado (e.g., inserir Contact + Conversation
//     + Message) ficam no use case via RunInTenantTx, NÃO no repo.

// TenantEnumerator itera os tenants ativos do platform context. É o port
// cross-context usado por messaging.OutboxRepo.ForEachTenant e por
// reconcile.Reconciler (issue #122).
//
// Implementações vivem em adapter/repository/postgres. A interface está
// no port para que o usecase (messaging, reconcile) não importe o
// adapter diretamente.
type TenantEnumerator interface {
	// ForEachActive invoca fn para cada tenant ativo. Stream — não
	// materializa a lista. Para it error, a iteração para e o erro propaga.
	ForEachActive(ctx context.Context, fn func(tenantID domain.TenantID) error) error
}

type TenantRepo interface {
	List(ctx context.Context) ([]domain.Tenant, error)
	Get(ctx context.Context, id domain.TenantID) (*domain.Tenant, error)
	Create(ctx context.Context, t *domain.Tenant) error
	Update(ctx context.Context, t *domain.Tenant) error
	Delete(ctx context.Context, id domain.TenantID) error
}

type ContactRepo interface {
	ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]domain.Contact, error)
	Get(ctx context.Context, id domain.ContactID) (*domain.Contact, error)
	Upsert(ctx context.Context, c *domain.Contact) error
}

type ConversationRepo interface {
	ListByTenant(ctx context.Context, tenantID domain.TenantID) ([]domain.Conversation, error)
	Get(ctx context.Context, id domain.ConversationID) (*domain.Conversation, error)
	Upsert(ctx context.Context, c *domain.Conversation) error
	UpdateStatus(ctx context.Context, id domain.ConversationID, status domain.ConversationStatus) error
}

type MessageRepo interface {
	ListByConversation(ctx context.Context, conversationID domain.ConversationID) ([]domain.Message, error)
	Get(ctx context.Context, id domain.MessageID) (*domain.Message, error)
	Insert(ctx context.Context, m *domain.Message) error
	UpdateStatus(ctx context.Context, id domain.MessageID, status domain.MessageStatus) error
	SelectUnroutedMessages(ctx context.Context, batchSize int) ([]domain.Message, error)
	MarkRouted(ctx context.Context, id domain.MessageID) error
}

type OutboxWriter interface {
	// Enqueue persiste um OutboxMessage no outbox. Issue #126: usa
	// domain.OutboxMessage (que referencia Message por ID) em vez de
	// receber domain.Message cru. A semântica da fila fica no domain
	// (FSM guards em OutboxMessage.MarkClaimed/MarkSent/MarkFailed/MarkDLQ).
	Enqueue(ctx context.Context, m *domain.OutboxMessage) error

	// Insert é deprecated (issue #126). Mantido como wrapper que cria
	// um OutboxMessage a partir de um domain.Message — usar Enqueue em
	// código novo. Será removido quando consolidarmos a tabela (issue
	// 3.3 — domain events).
	//
	// TODO(#126): remover após migração completa dos callers.
	Insert(ctx context.Context, m *domain.Message) error
}

type OutboxRelay interface {
	PendingCount(ctx context.Context) (int, error)
	ClaimNext(ctx context.Context, batchSize int) ([]domain.Message, error)
	MarkSent(ctx context.Context, id domain.MessageID) error
	MarkFailed(ctx context.Context, id domain.MessageID, err error) error
	// GetAttempts retorna o contador de tentativas para o outbox row. Usado
	// pelo relay para decidir MaxAttempts → DLQ.
	GetAttempts(ctx context.Context, id domain.MessageID) (int, error)
	// MarkDLQ move a mensagem para a dead-letter queue. Idempotente (no-op
	// se já está em dlq).
	MarkDLQ(ctx context.Context, id domain.MessageID, lastErr error) error
}

// CredentialRow é a view (cross-tenant ou não) de uma credencial de canal.
// É a forma canônica do tipo — substitui domain.ChannelCredentials
// (issue #118). Usado por:
//   - adapter/repository/postgres.ChannelCredentialsRepo (Get/Upsert/Delete)
//   - usecase/secrets.Keyring (CredentialsRepository)
//   - adapter/repository/postgres.ChannelCredentialsRepo.ForEachTenant (cross-tenant)
//
// Campos RotationWindowUntil, CreatedAt, UpdatedAt são opcionais na visão
// cross-tenant (ForEachTenant não os popula — só lê o necessário para
// re-wrap). Na visão tenant-scoped (Get) todos são populados.
type CredentialRow struct {
	TenantID            domain.TenantID
	Channel             domain.Channel
	WrappedDEK          []byte
	Encrypted           []byte
	KEKVersion          int
	RotationWindowUntil *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
