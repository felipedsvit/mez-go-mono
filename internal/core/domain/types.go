package domain

// TenantID identifies a tenant in the platform. It is the multi-tenant
// isolation boundary; every domain entity carries a TenantID so that
// repositories can apply row-level isolation policies.
type TenantID string

// Channel is a logical messaging channel such as WhatsApp Business Cloud
// API, Instagram Direct or a Telegram bot.
type Channel string

// Canonical channel names. These are stable, lowercased identifiers that
// are safe to embed in events, logs, and database columns.
const (
	ChannelWABA  Channel = "waba"
	ChannelWAWeb Channel = "whatsmeow"
	ChannelIG    Channel = "instagram"
	ChannelMSG   Channel = "messenger"
	ChannelTGBot Channel = "telegram_bot"
)

// ContactID is a stable identifier for a contact inside a tenant.
type ContactID string

// ConversationID is a stable identifier for a 1:1 or group conversation.
type ConversationID string

// MessageID is a stable identifier for a message.
type MessageID string

// ConversationStatus describes the lifecycle state of a conversation.
type ConversationStatus string

const (
	// ConvStatusOpen means the conversation is active and being routed.
	ConvStatusOpen ConversationStatus = "open"
	// ConvStatusPending means the conversation is queued and not yet
	// being processed by a worker.
	ConvStatusPending ConversationStatus = "pending"
	// ConvStatusResolved means the conversation has been closed.
	ConvStatusResolved ConversationStatus = "resolved"
)

// MessageType is the channel-independent kind of payload carried by a message.
type MessageType string

const (
	MessageTypeText     MessageType = "text"
	MessageTypeImage    MessageType = "image"
	MessageTypeAudio    MessageType = "audio"
	MessageTypeVideo    MessageType = "video"
	MessageTypeDocument MessageType = "document"
	MessageTypeSticker  MessageType = "sticker"
	MessageTypeLocation MessageType = "location"
	MessageTypeButton   MessageType = "button"
	MessageTypeTemplate MessageType = "template"
	MessageTypeReaction MessageType = "reaction"
	MessageTypeSystem   MessageType = "system"
)

// MessageStatus describes the lifecycle of a single message.
type MessageStatus string

const (
	MessageStatusReceived MessageStatus = "received"
	MessageStatusRouted   MessageStatus = "routed"
	MessageStatusNotified MessageStatus = "notified"
)

// Direction indicates whether a message is inbound (from the channel) or
// outbound (to the channel).
type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)
