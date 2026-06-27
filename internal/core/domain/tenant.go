package domain

import "time"

// Tenant is a logical customer of the platform. It is the root of the
// multi-tenant hierarchy: every other domain entity ultimately belongs to
// a single Tenant.
type Tenant struct {
	ID        TenantID  `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
