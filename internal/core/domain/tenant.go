package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Tenant is a logical customer of the platform. It is the root of the
// multi-tenant hierarchy: every other domain entity ultimately belongs to
// a single Tenant.
//
// Issue #125: factory NewTenant com validação de slug (espelha
// core/admin/tenant.go.NewTenant). O domain.Tenant é o agregado "raw"
// exposto para messaging/reconcile; o core/admin.Tenant é o modelo
// rico com status TenantStatus — bounded context diferente.
type Tenant struct {
	ID        TenantID  `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewTenant é a factory com validação. Retorna Tenant pronto para
// persistir (Active=true, CreatedAt/UpdatedAt=now).
func NewTenant(name, slug string) (*Tenant, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(strings.ToLower(slug))

	if name == "" {
		return nil, ErrInvalidInput
	}
	if slug == "" || !slugRegex.MatchString(slug) {
		return nil, ErrInvalidInput
	}

	now := time.Now().UTC()
	return &Tenant{
		ID:        TenantID(uuid.NewString()),
		Name:      name,
		Slug:      slug,
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// IsActive retorna o estado (helper de leitura).
func (t Tenant) IsActive() bool { return t.Active }

// Deactivate é uma transição de estado. Idempotente (no-op se já inativo).
// Útil para o reconcile ao detectar tenant suspenso no admin context.
func (t *Tenant) Deactivate() {
	if t.Active {
		t.Active = false
		t.UpdatedAt = time.Now().UTC()
	}
}
