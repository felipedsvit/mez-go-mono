package domain_test

import (
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestTenant(t *testing.T) {
	now := time.Now().UTC()
	tn := domain.Tenant{
		ID:        "tenant-1",
		Name:      "Acme Inc",
		Slug:      "acme",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if tn.ID != "tenant-1" {
		t.Errorf("ID mismatch: %s", tn.ID)
	}
	if !tn.Active {
		t.Errorf("Active should be true")
	}
	if tn.Slug != "acme" {
		t.Errorf("Slug mismatch: %s", tn.Slug)
	}
}
