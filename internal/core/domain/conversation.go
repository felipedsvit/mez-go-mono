package domain

import "time"

// Conversation is an ongoing thread of messages between a contact and
// the platform (often mediated by an agent or a bot). Conversations are
// the unit of routing: a conversation may be open, pending, or resolved.
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
