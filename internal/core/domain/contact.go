package domain

import "time"

// Contact represents a person (end user) reachable through a channel.
// A contact always belongs to a single tenant.
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
