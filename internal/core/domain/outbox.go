package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// OutboxMessage é o agregado da fila outbound (issue #126, review DDD-Hex
// §3.8). Outbox e Message são duas tabelas paralelas hoje; o tipo aqui
// referencia a Message por ID, sem duplicar campos.
//
// Decisão consciente: NÃO consolidamos a tabela (volume de retries). A
// consolidação física virá junto com 3.3 (domain events). Por agora,
// introduzimos o tipo de domínio para que usecase.Send e usecase.Relay
// falem em OutboxMessage, não em domain.Message.
type OutboxMessage struct {
	ID             MessageID   `json:"id"`
	MessageID      MessageID   `json:"message_id"`
	TenantID       TenantID    `json:"tenant_id"`
	Channel        Channel     `json:"channel"`
	ConversationID ConversationID `json:"conversation_id"`
	ContactID      ContactID   `json:"contact_id"`
	Status         OutboxStatus  `json:"status"`
	Attempts       int           `json:"attempts"`
	LastError      string        `json:"last_error,omitempty"`
	NextAttemptAt  time.Time     `json:"next_attempt_at"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// OutboxStatus é o ciclo de vida da mensagem no outbox.
type OutboxStatus string

const (
	// OutboxStatusPending: enfileirada, aguardando claim pelo relay.
	OutboxStatusPending OutboxStatus = "pending"
	// OutboxStatusClaimed: claim pelo relay, em envio.
	OutboxStatusClaimed OutboxStatus = "claimed"
	// OutboxStatusSent: entregue (provider confirmou).
	OutboxStatusSent OutboxStatus = "sent"
	// OutboxStatusFailed: falhou, será retentado.
	OutboxStatusFailed OutboxStatus = "failed"
	// OutboxStatusDLQ: excedeu MaxAttempts, movida para DLQ.
	OutboxStatusDLQ OutboxStatus = "dlq"
)

// NewOutboxMessage é a factory. Cria com Status=Pending e NextAttemptAt=now.
func NewOutboxMessage(messageID MessageID, tenantID TenantID, channel Channel, conversationID ConversationID, contactID ContactID) (*OutboxMessage, error) {
	if messageID == "" {
		return nil, ErrInvalidInput
	}
	if tenantID == "" {
		return nil, ErrInvalidInput
	}
	if channel == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	return &OutboxMessage{
		ID:             MessageID(uuid.NewString()),
		MessageID:      messageID,
		TenantID:       tenantID,
		Channel:        channel,
		ConversationID: conversationID,
		ContactID:      contactID,
		Status:         OutboxStatusPending,
		Attempts:       0,
		NextAttemptAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// RecordAttempt é chamado quando o relay tenta entregar. Incrementa
// Attempts e armazena lastErr. Transita para Failed (mantém Pending
// removido pelo Claim).
func (o *OutboxMessage) RecordAttempt(lastErr error) {
	o.Attempts++
	if lastErr != nil {
		o.LastError = lastErr.Error()
	}
	o.UpdatedAt = time.Now().UTC()
}

// MarkClaimed transita Pending|Failed → Claimed. Idempotente: se já
// Claimed, no-op. Outros estados: erro.
func (o *OutboxMessage) MarkClaimed() error {
	if o.Status == OutboxStatusClaimed {
		return nil
	}
	if o.Status != OutboxStatusPending && o.Status != OutboxStatusFailed {
		return ErrInvalidTransition
	}
	o.Status = OutboxStatusClaimed
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkSent transita Claimed → Sent. Idempotente.
func (o *OutboxMessage) MarkSent() error {
	if o.Status == OutboxStatusSent {
		return nil
	}
	if o.Status != OutboxStatusClaimed {
		return ErrInvalidTransition
	}
	o.Status = OutboxStatusSent
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transita Claimed → Failed e agenda próxima tentativa
// (backoff in-process, não persistente — o relay recalcula via Attempts).
func (o *OutboxMessage) MarkFailed(nextAttemptAt time.Time, lastErr error) error {
	if o.Status == OutboxStatusFailed {
		// Re-falha: só atualiza timestamp e erro.
		o.RecordAttempt(lastErr)
		o.NextAttemptAt = nextAttemptAt
		return nil
	}
	if o.Status != OutboxStatusClaimed {
		return ErrInvalidTransition
	}
	o.RecordAttempt(lastErr)
	o.Status = OutboxStatusFailed
	o.NextAttemptAt = nextAttemptAt
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkDLQ move a mensagem para a dead-letter queue. Idempotente.
func (o *OutboxMessage) MarkDLQ(lastErr error) error {
	if o.Status == OutboxStatusDLQ {
		return nil
	}
	if o.Status != OutboxStatusFailed && o.Status != OutboxStatusClaimed {
		return ErrInvalidTransition
	}
	if lastErr != nil {
		o.LastError = lastErr.Error()
	}
	o.Status = OutboxStatusDLQ
	o.UpdatedAt = time.Now().UTC()
	return nil
}

// TrimLastError aplica limite ao last_error para evitar blobs grandes
// no DB. Chamado antes de Upsert.
func (o *OutboxMessage) TrimLastError(maxBytes int) {
	if maxBytes > 0 && len(o.LastError) > maxBytes {
		o.LastError = strings.ToValidUTF8(o.LastError[:maxBytes], "") + "…"
	}
}
