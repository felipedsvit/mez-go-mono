package routing

import (
	"context"
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/rs/zerolog"
)

func TestRouter_Assign_ReturnsEmptyForFase2(t *testing.T) {
	r := NewRouter(zerolog.Nop())

	agentID, err := r.Assign(context.Background(), domain.Message{
		ID:       "m1",
		TenantID: "t1",
		Channel:  domain.ChannelWABA,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Fase 2: sempre unassigned.
	if agentID != "" {
		t.Errorf("agent = %q, want empty (fase 2 unassigned)", agentID)
	}
}

func TestRouter_RequiresTenantID(t *testing.T) {
	r := NewRouter(zerolog.Nop())
	_, err := r.Assign(context.Background(), domain.Message{ID: "m1"})
	if err == nil {
		t.Error("expected error for empty tenant_id")
	}
}
