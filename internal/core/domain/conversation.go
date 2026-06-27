package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Conversation é o **aggregate root** do subdomínio thread
// (issue #125, review DDD-Hex §3.7). Tudo que muda o estado da thread
// (adicionar mensagem, mudar status, atribuir agente) é método deste
// tipo. O usecase (messaging.Ingestor) chama Conversation.NewInboundMessage
// em vez de construir Message cru.
//
// Mutações só passam pelos métodos do domain. Não há setters públicos
// para os campos além de NewConversation (que cria o agregado) e dos
// métodos abaixo (que aplicam FSM guards).
type Conversation struct {
	ID            ConversationID     `json:"id"`
	TenantID      TenantID           `json:"tenant_id"`
	Channel       Channel            `json:"channel"`
	ContactID     ContactID          `json:"contact_id"`
	Status        ConversationStatus `json:"status"`
	ExternalID    string             `json:"external_id,omitempty"`
	AssignedAgent string             `json:"assigned_agent,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// NewConversation é a factory do AR. Cria a conversa no estado Open.
// ExternalID é o identificador do peer (wa_id, chat_id, psid) usado para
// idempotência de upsert.
func NewConversation(tenantID TenantID, channel Channel, contactID ContactID, externalID string) (*Conversation, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}
	if channel == "" {
		return nil, ErrInvalidInput
	}
	if contactID == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	return &Conversation{
		ID:         ConversationID(uuid.NewString()),
		TenantID:   tenantID,
		Channel:    channel,
		ContactID:  contactID,
		Status:     ConvStatusOpen,
		ExternalID: strings.TrimSpace(externalID),
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// IsOpen é helper de leitura.
func (c *Conversation) IsOpen() bool { return c.Status == ConvStatusOpen }

// IsResolved é helper de leitura.
func (c *Conversation) IsResolved() bool { return c.Status == ConvStatusResolved }

// Assign atribui a conversa a um agentID. agentID vazio significa
// "desmarcar" (unassign). Idempotente: chamar com o mesmo agentID é
// no-op. Resolve é terminal — Assign após Resolve é FSM error.
func (c *Conversation) Assign(agentID string) error {
	if c.Status == ConvStatusResolved {
		return ErrInvalidTransition
	}
	agentID = strings.TrimSpace(agentID)
	if c.AssignedAgent == agentID {
		return nil
	}
	c.AssignedAgent = agentID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Resolve marca a conversa como resolvida. Idempotente.
func (c *Conversation) Resolve() error {
	if c.Status == ConvStatusResolved {
		return nil
	}
	if c.Status != ConvStatusOpen && c.Status != ConvStatusPending {
		return ErrInvalidTransition
	}
	c.Status = ConvStatusResolved
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Touch atualiza UpdatedAt. Usado pelo AR ao adicionar mensagem.
func (c *Conversation) Touch() {
	c.UpdatedAt = time.Now().UTC()
}

// NewInboundMessage é o **método do AR** que cria uma Message e atualiza
// a conversa. Issue #125: é a única forma de criar uma Message inbound a
// partir do usecase (Ingestor). Garante que:
//
//  1. A Message sempre referencia este Conversation (consistência do AR).
//  2. A FSM da Message é inicializada (Received).
//  3. A Conversation tem UpdatedAt tocado (audit-friendly).
//  4. Se a Conversation está Resolved, abrir uma nova mensagem não é
//     permitido (FSM guard — issue #125).
//
// Retorna ErrInvalidTransition se a conversa já está resolvida. O
// caller (Ingestor) decide se cria uma nova conversa ou reativa esta.
func (c *Conversation) NewInboundMessage(
	body string,
	providerMsgID string,
) (*Message, error) {
	if c.Status == ConvStatusResolved {
		return nil, ErrInvalidTransition
	}
	msg, err := newInboundMessage(
		c.TenantID,
		c.Channel,
		c.ID,
		c.ContactID,
		body,
		providerMsgID,
	)
	if err != nil {
		return nil, err
	}
	c.Touch()
	return msg, nil
}
