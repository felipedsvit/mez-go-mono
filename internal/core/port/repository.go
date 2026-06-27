package port

import (
	"context"
	"errors"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
)

// TxRunner executes a function inside a tenant-scoped transaction.
// The transaction is propagated via context; repositories extract it with the
// package-level helper in the postgres adapter.
type TxRunner interface {
	RunInTenantTx(ctx context.Context, tenantID domain.TenantID, fn func(ctx context.Context) error) error
	RunAsPlatform(ctx context.Context, actor string, fn func(ctx context.Context) error) error
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

// CredentialRow é a view cross-tenant de uma credencial de canal. Usada pelo
// ChannelCredentialsCrossTenant.ForEachTenant para iterar todas as linhas sem
// expor o tipo concreto de um adapter específico. Vive em port para que
// usecase/secrets e adapter/repository/postgres compartilhem o mesmo tipo sem
// ciclo de import.
type CredentialRow struct {
	TenantID   domain.TenantID
	Channel    domain.Channel
	WrappedDEK []byte
	Encrypted  []byte
	KEKVersion int
}
