package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

type TenantStatus string

const (
	TenantActive    TenantStatus = "active"
	TenantSuspended TenantStatus = "suspended"
)

type Tenant struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Slug      string       `json:"slug"`
	Status    TenantStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type TenantFilter struct {
	Status *TenantStatus
	Search string
	Limit  int
	Offset int
}

var slugRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]{0,61}[a-z0-9])?$`)

func NewTenant(name, slug string) (*Tenant, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(strings.ToLower(slug))

	if name == "" {
		return nil, ErrInvalidTenantName
	}
	if slug == "" || !slugRegex.MatchString(slug) {
		return nil, ErrInvalidSlug
	}

	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}

	return &Tenant{
		ID:     "tenant_" + hex.EncodeToString(id),
		Name:   name,
		Slug:   slug,
		Status: TenantActive,
	}, nil
}

type TenantRepo interface {
	GetByID(ctx context.Context, id string) (Tenant, error)
	GetBySlug(ctx context.Context, slug string) (Tenant, error)
	List(ctx context.Context, filter TenantFilter) ([]Tenant, error)
	Create(ctx context.Context, t *Tenant) error
	UpdateProfile(ctx context.Context, id, name, slug string) error
	SetStatus(ctx context.Context, id string, status TenantStatus) error
}
