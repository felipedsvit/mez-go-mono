package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Message é uma entidade DENTRO do aggregate Conversation (issue #125).
// É criada pelo AR via Conversation.NewInboundMessage; o usecase não
// constrói Message cru diretamente.
//
// FSM de status: Received → Routed → Notified. As transições são
// guardadas pelos métodos; o repo nunca seta Status diretamente.
type Message struct {
	ID             MessageID      `json:"id"`
	TenantID       TenantID       `json:"tenant_id"`
	Channel        Channel        `json:"channel"`
	ConversationID ConversationID `json:"conversation_id"`
	ContactID      ContactID      `json:"contact_id"`
	Direction      Direction      `json:"direction"`
	Type           MessageType    `json:"type"`
	Status         MessageStatus  `json:"status"`
	Body           string         `json:"body"`
	ProviderMsgID  string         `json:"provider_msg_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// NewInboundMessage é o **factory do AR** (issue #125). Chamado por
// Conversation.NewInboundMessage. Recebe os campos normalizados pelo
// adapter; o domain valida FSM e invariantes coarse-grained.
//
// Status inicial: Received. As transições subsequentes (Routed, Notified)
// são feitas pelos métodos MarkRouted/MarkNotified.
func newInboundMessage(
	tenantID TenantID,
	channel Channel,
	conversationID ConversationID,
	contactID ContactID,
	body string,
	providerMsgID string,
) (*Message, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}
	if channel == "" {
		return nil, ErrInvalidInput
	}
	if conversationID == "" {
		return nil, ErrInvalidInput
	}
	if contactID == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	return &Message{
		ID:             MessageID(uuid.NewString()),
		TenantID:       tenantID,
		Channel:        channel,
		ConversationID: conversationID,
		ContactID:      contactID,
		Direction:      DirectionInbound,
		Type:           MessageTypeText,
		Status:         MessageStatusReceived,
		Body:           body,
		ProviderMsgID:  strings.TrimSpace(providerMsgID),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// NewOutboundMessage é a factory para mensagens outbound. Usada pelo
// SenderService (issue #126). Status inicial: Notified (a mensagem
// já foi enfileirada, não precisa de transição Routed).
func NewOutboundMessage(
	tenantID TenantID,
	channel Channel,
	conversationID ConversationID,
	contactID ContactID,
	body string,
	msgType MessageType,
) (*Message, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}
	if channel == "" {
		return nil, ErrInvalidInput
	}
	if conversationID == "" {
		return nil, ErrInvalidInput
	}
	if contactID == "" {
		return nil, ErrInvalidInput
	}
	if msgType == "" {
		msgType = MessageTypeText
	}
	now := time.Now().UTC()
	return &Message{
		ID:             MessageID(uuid.NewString()),
		TenantID:       tenantID,
		Channel:        channel,
		ConversationID: conversationID,
		ContactID:      contactID,
		Direction:      DirectionOutbound,
		Type:           msgType,
		Status:         MessageStatusNotified,
		Body:           body,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// IsInbound é helper de leitura.
func (m *Message) IsInbound() bool { return m.Direction == DirectionInbound }

// IsOutbound é helper de leitura.
func (m *Message) IsOutbound() bool { return m.Direction == DirectionOutbound }

// MarkRouted transita Received → Routed. FSM guard: se já está em Routed
// ou Notified, retorna ErrInvalidTransition. Idempotência fica a cargo
// do caller (reconciler pode chamar repetidamente).
func (m *Message) MarkRouted() error {
	if m.Status == MessageStatusRouted || m.Status == MessageStatusNotified {
		return ErrInvalidTransition
	}
	if m.Status != MessageStatusReceived {
		return ErrInvalidTransition
	}
	m.Status = MessageStatusRouted
	m.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkNotified transita Received|Routed → Notified. FSM guard: já em
// Notified é no-op (idempotente). Chamado pelo reconciler e pelo relay
// após entrega confirmada.
func (m *Message) MarkNotified() error {
	if m.Status == MessageStatusNotified {
		return nil
	}
	if m.Status != MessageStatusReceived && m.Status != MessageStatusRouted {
		return ErrInvalidTransition
	}
	m.Status = MessageStatusNotified
	m.UpdatedAt = time.Now().UTC()
	return nil
}
