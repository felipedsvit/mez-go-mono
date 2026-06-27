package domain_test

import (
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestConversation(t *testing.T) {
	now := time.Now().UTC()
	conv := domain.Conversation{
		ID:            "conv-1",
		TenantID:      "tenant-1",
		Channel:       domain.ChannelWABA,
		ContactID:     "contact-1",
		Status:        domain.ConvStatusOpen,
		ExternalID:    "ext-1",
		AssignedAgent: "agent-1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if conv.ID != "conv-1" {
		t.Errorf("ID mismatch: %s", conv.ID)
	}
	if conv.Status != domain.ConvStatusOpen {
		t.Errorf("Status mismatch: %s", conv.Status)
	}
	if conv.AssignedAgent != "agent-1" {
		t.Errorf("AssignedAgent mismatch: %s", conv.AssignedAgent)
	}
}
