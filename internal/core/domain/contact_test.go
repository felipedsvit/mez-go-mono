package domain_test

import (
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

func TestContact(t *testing.T) {
	now := time.Now().UTC()
	c := domain.Contact{
		ID:         "contact-1",
		TenantID:   "tenant-1",
		Channel:    domain.ChannelWABA,
		ProviderID: "+5511900000000",
		Name:       "John Doe",
		AvatarURL:  "https://example.com/avatar.png",
		Metadata:   map[string]any{"source": "import"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if c.ID != "contact-1" {
		t.Errorf("ID mismatch: %s", c.ID)
	}
	if c.TenantID != "tenant-1" {
		t.Errorf("TenantID mismatch: %s", c.TenantID)
	}
	if c.Channel != domain.ChannelWABA {
		t.Errorf("Channel mismatch: %s", c.Channel)
	}
}
