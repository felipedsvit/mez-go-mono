package domain

import "time"

// Message is a single inbound or outbound payload, normalized across all
// channels. The application code only ever deals with this type; per-channel
// adapters translate to/from their native formats.
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
