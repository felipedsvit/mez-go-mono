package domain_test

import (
	"errors"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// TestNewTenant_Valid cobre a factory com validação (issue #125).
func TestNewTenant_Valid(t *testing.T) {
	tn, err := domain.NewTenant("Acme Inc", "acme")
	if err != nil {
		t.Fatalf("NewTenant: %v", err)
	}
	if tn.ID == "" {
		t.Error("ID should be generated")
	}
	if !tn.Active {
		t.Error("Active should be true on creation")
	}
}

// TestNewTenant_Invalid cobre os caminhos de erro da factory.
func TestNewTenant_Invalid(t *testing.T) {
	cases := []struct {
		name, n, slug string
	}{
		{"empty name", "", "acme"},
		{"empty slug", "Acme", ""},
		{"bad slug with space", "Acme", "acme bad"},
		{"slug with underscore", "Acme", "acme_corp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NewTenant(c.n, c.slug)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Errorf("expected ErrInvalidInput, got: %v", err)
			}
		})
	}
}

// TestTenant_Deactivate verifica a transição de estado.
func TestTenant_Deactivate(t *testing.T) {
	tn, _ := domain.NewTenant("Acme", "acme")
	if !tn.IsActive() {
		t.Fatal("new tenant should be active")
	}
	tn.Deactivate()
	if tn.IsActive() {
		t.Error("after Deactivate, tenant should be inactive")
	}
	// Idempotente.
	tn.Deactivate()
	if tn.IsActive() {
		t.Error("second Deactivate should be no-op")
	}
}
