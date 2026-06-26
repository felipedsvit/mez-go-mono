package domain_test

import (
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestMessage(t *testing.T) {
	now := time.Now().UTC()
	msg := domain.Message{
		ID:             "msg-1",
		TenantID:       "tenant-1",
		Channel:        domain.ChannelWABA,
		ConversationID: "conv-1",
		ContactID:      "contact-1",
		Direction:      domain.DirectionInbound,
		Type:           domain.MessageTypeText,
		Status:         domain.MessageStatusReceived,
		Body:           "Hello, world!",
		ProviderMsgID:  "wamid.123",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if msg.ID != "msg-1" {
		t.Errorf("ID mismatch: %s", msg.ID)
	}
	if msg.Channel != domain.ChannelWABA {
		t.Errorf("Channel mismatch: %s", msg.Channel)
	}
	if msg.Direction != domain.DirectionInbound {
		t.Errorf("Direction mismatch: %s", msg.Direction)
	}
}
