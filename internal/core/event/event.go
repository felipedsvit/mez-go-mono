// Package event defines the canonical event envelopes that flow through the
// system. Adapters translate provider-specific events to/from these types,
// and the rest of the codebase only ever deals with these envelopes.
package event

import "time"

// Event is a logical thing that happened in the system. It is modeled as a
// domain-layer envelope, allowing adapters and workers to translate
// provider-specific events to and from this canonical shape. The rest of the
// codebase only deals with these envelopes.
type Event struct {
	ID         string            `json:"id"`
	TenantID   string            `json:"tenant_id"`
	Channel    Channel           `json:"channel"`
	EventID    string            `json:"event_id"`
	EventType  string            `json:"event_type"`
	Source     string            `json:"source"`
	ProviderID string            `json:"provider_id,omitempty"`
	Recipient  string            `json:"recipient,omitempty"`
	Timestamp  time.Time         `json:"timestamp"`
	Metadata   map[string]any    `json:"metadata,omitempty"`
	Payload    []byte            `json:"payload,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}

// InboundEvent is a message received from a channel, already normalized
// to the canonical shape.
type InboundEvent struct {
	TenantID  string  `json:"tenant_id"`
	Channel   Channel `json:"channel"`
	MessageID string  `json:"message_id"`
}

// OutboundEvent is a message the application wants the channel adapter
// to deliver.
type OutboundEvent struct {
	TenantID  string  `json:"tenant_id"`
	Channel   Channel `json:"channel"`
	MessageID string  `json:"message_id"`
}

// StatusEvent is published by the worker to indicate progress of a
// message (sent, delivered, read, failed, etc.).
type StatusEvent struct {
	TenantID  string `json:"tenant_id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

// LifecycleEvent signals changes in a worker's or channel's state, e.g.
// when a connection is established or lost.
type LifecycleEvent struct {
	TenantID string         `json:"tenant_id"`
	Channel  Channel        `json:"channel"`
	Event    string         `json:"event"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// DLQEvent is published when a message cannot be delivered after the
// configured retry budget. Operators should inspect the DLQ to recover.
type DLQEvent struct {
	TenantID  string  `json:"tenant_id"`
	Channel   Channel `json:"channel"`
	MessageID string  `json:"message_id"`
	Error     string  `json:"error"`
}

// Channel identifies a logical messaging channel.
type Channel string

// Canonical channel identifiers.
const (
	ChannelWABA  Channel = "waba"
	ChannelWAWeb Channel = "whatsapp_web"
	ChannelIG    Channel = "instagram"
	ChannelMSG   Channel = "messenger"
	ChannelTGBot Channel = "telegram_bot"
)

// Direction indicates whether an event flows inbound or outbound.
type Direction string

const (
	// DirectionInbound means the event is coming from the channel.
	DirectionInbound Direction = "inbound"
	// DirectionOutbound means the event is going to the channel.
	DirectionOutbound Direction = "outbound"
)

// Stream identifies a logical partition of events.
type Stream string

const (
	StreamInbound   Stream = "inbound"
	StreamOutbound  Stream = "outbound"
	StreamStatus    Stream = "status"
	StreamLifecycle Stream = "lifecycle"
	StreamDLQ       Stream = "dlq"
)

// Header keys commonly used on event envelopes.
const (
	HeaderMessageID  = "X-Message-ID"
	HeaderEventID    = "X-Event-ID"
	HeaderRetryCount = "X-Retry-Count"
	HeaderTraceID    = "X-Trace-ID"
)
