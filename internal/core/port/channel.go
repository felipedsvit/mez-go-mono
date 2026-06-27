package port

import (
	"context"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
)

type Channel interface {
	Name() domain.Channel
	Capabilities() CapabilitySet
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Send(ctx context.Context, msg *domain.Message) error
}

// InboundSink is implemented by the broker to receive events from channel adapters.
type InboundSink interface {
	PublishInbound(evt event.InboundEvent)
	PublishStatus(evt event.StatusEvent)
	PublishLifecycle(evt event.LifecycleEvent)
}

// OutboundPublisher is implemented by the broker to fan-out delivery requests.
type OutboundPublisher interface {
	PublishOutbound(evt event.OutboundEvent)
}
