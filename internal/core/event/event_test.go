package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/event"
)

func TestEventJSON(t *testing.T) {
	e := event.Event{
		ID:         "evt-1",
		TenantID:   "tenant-1",
		Channel:    event.ChannelWABA,
		EventID:    "evt-1",
		EventType:  "message.received",
		Source:     "provider",
		Timestamp:  time.Now().UTC(),
		ProviderID: "msg-1",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.ID != e.ID {
		t.Errorf("ID mismatch: %s != %s", decoded.ID, e.ID)
	}
	if decoded.Channel != e.Channel {
		t.Errorf("Channel mismatch: %s != %s", decoded.Channel, e.Channel)
	}
}
