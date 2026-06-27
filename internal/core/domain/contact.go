package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Contact represents a person (end user) reachable through a channel.
// A contact always belongs to a single tenant.
//
// Reference-by-ID: Contact NÃO é aggregate root. É referenciado por
// Conversation.ContactID e por Message.ContactID. Mutações no Contact
// não disparam domain events (issue #125, review DDD-Hex §3.7 —
// eventual consistency cross-aggregate).
type Contact struct {
	ID         ContactID      `json:"id"`
	TenantID   TenantID       `json:"tenant_id"`
	Channel    Channel        `json:"channel"`
	ProviderID string         `json:"provider_id"`
	Name       string         `json:"name,omitempty"`
	AvatarURL  string         `json:"avatar_url,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// NewContact é a factory com validação coarse-grained. Issue #125.
func NewContact(tenantID TenantID, channel Channel, providerID string) (*Contact, error) {
	if tenantID == "" {
		return nil, ErrInvalidInput
	}
	if channel == "" {
		return nil, ErrInvalidInput
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	return &Contact{
		ID:         ContactID(uuid.NewString()),
		TenantID:   tenantID,
		Channel:    channel,
		ProviderID: providerID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Rename atualiza o nome. Trimming aplicado; nome vazio é ignorado
// silenciosamente (não é erro — o caller pode querer limpar).
func (c *Contact) Rename(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	c.Name = name
	c.UpdatedAt = time.Now().UTC()
}
