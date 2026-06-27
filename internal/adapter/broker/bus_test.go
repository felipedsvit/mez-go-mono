package broker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

func TestPublishInbound_DropSafe(t *testing.T) {
	log := zerolog.Nop()
	metricsReg := metrics.NewRegistry()

	bus := broker.NewBus(broker.BusConfig{
		InboundBuffer:  1,
		OutboundBuffer: 1,
		StatusBuffer:   1,
		DLQBuffer:      1,
	}, log, metricsReg)

	for i := 0; i < 100; i++ {
		bus.PublishInbound(event.InboundEvent{
			TenantID:  "test-tenant",
			Channel:   event.ChannelWABA,
			MessageID: "msg",
		})
	}
}

func TestPublishInbound_HandlerCalled(t *testing.T) {
	log := zerolog.Nop()
	metricsReg := metrics.NewRegistry()
	bus := broker.NewBus(broker.BusConfig{
		InboundBuffer: 10,
	}, log, metricsReg)

	var called atomic.Int32
	bus.SubscribeInbound(func(evt event.InboundEvent) {
		called.Add(1)
	})

	bus.PublishInbound(event.InboundEvent{
		TenantID:  "t1",
		Channel:   event.ChannelWABA,
		MessageID: "m1",
	})

	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(1), called.Load(), "handler should have been called once")
}

func TestBus_Drain(t *testing.T) {
	ctx := context.Background()
	log := zerolog.Nop()
	metricsReg := metrics.NewRegistry()
	bus := broker.NewBus(broker.BusConfig{}, log, metricsReg)

	var called atomic.Int32
	bus.SubscribeInbound(func(evt event.InboundEvent) {
		called.Add(1)
	})

	bus.PublishInbound(event.InboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "m1"})
	bus.PublishInbound(event.InboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "m2"})

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(2), called.Load(), "both events should be processed before drain")

	if err := bus.Drain(ctx); err != nil {
		t.Fatal(err)
	}

	bus.PublishInbound(event.InboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "m3"})
	assert.Equal(t, int32(2), called.Load(), "no new events after drain")
}
