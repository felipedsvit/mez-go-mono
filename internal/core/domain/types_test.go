package domain_test

import (
	"testing"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestChannelConstants(t *testing.T) {
	tests := []struct {
		channel domain.Channel
		want    string
	}{
		{domain.ChannelWABA, "waba"},
		{domain.ChannelWAWeb, "whatsmeow"},
		{domain.ChannelIG, "instagram"},
		{domain.ChannelMSG, "messenger"},
		{domain.ChannelTGBot, "telegram_bot"},
	}
	for _, tt := range tests {
		if string(tt.channel) != tt.want {
			t.Errorf("Channel %s != %s", tt.channel, tt.want)
		}
	}
}

func TestMessageStatusValues(t *testing.T) {
	if domain.MessageStatusReceived != "received" {
		t.Error("MessageStatusReceived should be 'received'")
	}
	if domain.MessageStatusRouted != "routed" {
		t.Error("MessageStatusRouted should be 'routed'")
	}
	if domain.MessageStatusNotified != "notified" {
		t.Error("MessageStatusNotified should be 'notified'")
	}
}
