package broker

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

type Bus struct {
	inbound   chan event.InboundEvent
	outbound  chan event.OutboundEvent
	status    chan event.StatusEvent
	dlq       chan event.DLQEvent
	lifecycle chan event.LifecycleEvent

	metrics *metrics.Registry
	log     zerolog.Logger

	inboundHandlers   []func(event.InboundEvent)
	outboundHandlers  []func(event.OutboundEvent)
	statusHandlers    []func(event.StatusEvent)
	dlqHandlers       []func(event.DLQEvent)
	lifecycleHandlers []func(event.LifecycleEvent)

	mu        sync.RWMutex
	wg        sync.WaitGroup
	drainCh   chan struct{}
	drainOnce sync.Once
}

type BusConfig struct {
	InboundBuffer   int
	OutboundBuffer  int
	StatusBuffer    int
	DLQBuffer       int
	LifecycleBuffer int
}

func NewBus(cfg BusConfig, log zerolog.Logger, metrics *metrics.Registry) *Bus {
	if cfg.InboundBuffer <= 0 {
		cfg.InboundBuffer = 1024
	}
	if cfg.OutboundBuffer <= 0 {
		cfg.OutboundBuffer = 1024
	}
	if cfg.StatusBuffer <= 0 {
		cfg.StatusBuffer = 256
	}
	if cfg.DLQBuffer <= 0 {
		cfg.DLQBuffer = 256
	}
	if cfg.LifecycleBuffer <= 0 {
		cfg.LifecycleBuffer = 64
	}

	b := &Bus{
		inbound:   make(chan event.InboundEvent, cfg.InboundBuffer),
		outbound:  make(chan event.OutboundEvent, cfg.OutboundBuffer),
		status:    make(chan event.StatusEvent, cfg.StatusBuffer),
		dlq:       make(chan event.DLQEvent, cfg.DLQBuffer),
		lifecycle: make(chan event.LifecycleEvent, cfg.LifecycleBuffer),
		metrics:   metrics,
		log:       log,
		drainCh:   make(chan struct{}),
	}

	b.startConsumers()
	return b
}

func (b *Bus) startConsumers() {
	b.wg.Add(5)
	go b.consumeInbound()
	go b.consumeOutbound()
	go b.consumeStatus()
	go b.consumeDLQ()
	go b.consumeLifecycle()
}

func (b *Bus) PublishInbound(evt event.InboundEvent) {
	if b.isDrained() {
		return
	}
	select {
	case b.inbound <- evt:
		b.metrics.BusPublishedTotal.WithLabelValues("inbound").Inc()
	default:
		b.metrics.BusDroppedTotal.WithLabelValues("inbound").Inc()
		b.log.Warn().Str("topic", "inbound").Str("msg_id", string(evt.MessageID)).Msg("bus inbound buffer full; drop (reconciler covers)")
	}
}

func (b *Bus) PublishOutbound(evt event.OutboundEvent) {
	if b.isDrained() {
		return
	}
	select {
	case b.outbound <- evt:
		b.metrics.BusPublishedTotal.WithLabelValues("outbound").Inc()
	default:
		b.metrics.BusDroppedTotal.WithLabelValues("outbound").Inc()
		b.log.Warn().Str("topic", "outbound").Msg("bus outbound buffer full; drop")
	}
}

func (b *Bus) PublishStatus(evt event.StatusEvent) {
	if b.isDrained() {
		return
	}
	select {
	case b.status <- evt:
		b.metrics.BusPublishedTotal.WithLabelValues("status").Inc()
	default:
		b.metrics.BusDroppedTotal.WithLabelValues("status").Inc()
		b.log.Warn().Str("topic", "status").Msg("bus status buffer full; drop")
	}
}

// PublishDLQ enqueues a dead-letter event. If the buffer is full the event is
// dropped with an error log — blocking here risks a goroutine hang after Drain.
func (b *Bus) PublishDLQ(evt event.DLQEvent) {
	if b.isDrained() {
		return
	}
	select {
	case b.dlq <- evt:
		b.metrics.BusPublishedTotal.WithLabelValues("dlq").Inc()
	default:
		b.metrics.BusDroppedTotal.WithLabelValues("dlq").Inc()
		b.log.Error().Str("topic", "dlq").Str("msg_id", string(evt.MessageID)).Msg("bus DLQ buffer full; drop")
	}
}

func (b *Bus) PublishLifecycle(evt event.LifecycleEvent) {
	if b.isDrained() {
		return
	}
	select {
	case b.lifecycle <- evt:
		b.metrics.BusPublishedTotal.WithLabelValues("lifecycle").Inc()
	default:
		b.metrics.BusDroppedTotal.WithLabelValues("lifecycle").Inc()
		b.log.Warn().Str("topic", "lifecycle").Str("tenant", string(evt.TenantID)).Msg("bus lifecycle buffer full; drop")
	}
}

func (b *Bus) SubscribeInbound(handler func(event.InboundEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inboundHandlers = append(b.inboundHandlers, handler)
}

func (b *Bus) SubscribeOutbound(handler func(event.OutboundEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.outboundHandlers = append(b.outboundHandlers, handler)
}

func (b *Bus) SubscribeStatus(handler func(event.StatusEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.statusHandlers = append(b.statusHandlers, handler)
}

func (b *Bus) SubscribeDLQ(handler func(event.DLQEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dlqHandlers = append(b.dlqHandlers, handler)
}

func (b *Bus) SubscribeLifecycle(handler func(event.LifecycleEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lifecycleHandlers = append(b.lifecycleHandlers, handler)
}

// Drain signals all consumers to stop and waits for them to finish or ctx to expire.
// Safe to call multiple times — subsequent calls are no-ops.
func (b *Bus) Drain(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	b.drainOnce.Do(func() { close(b.drainCh) })
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *Bus) BufferDepth() map[string]int {
	return map[string]int{
		"inbound":   len(b.inbound),
		"outbound":  len(b.outbound),
		"status":    len(b.status),
		"dlq":       len(b.dlq),
		"lifecycle": len(b.lifecycle),
	}
}

func (b *Bus) isDrained() bool {
	select {
	case <-b.drainCh:
		return true
	default:
		return false
	}
}

// safeCall invokes fn and recovers from any panic, logging it as an error.
func (b *Bus) safeCall(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error().Interface("panic", r).Msg("bus handler panicked; recovered")
		}
	}()
	fn()
}

func (b *Bus) consumeInbound() {
	defer b.wg.Done()
	for {
		select {
		case <-b.drainCh:
			b.drainInbound()
			return
		case evt := <-b.inbound:
			b.mu.RLock()
			handlers := b.inboundHandlers
			b.mu.RUnlock()
			for _, h := range handlers {
				h := h
				b.safeCall(func() { h(evt) })
			}
		}
	}
}

func (b *Bus) consumeOutbound() {
	defer b.wg.Done()
	for {
		select {
		case <-b.drainCh:
			b.drainOutbound()
			return
		case evt := <-b.outbound:
			b.mu.RLock()
			handlers := b.outboundHandlers
			b.mu.RUnlock()
			for _, h := range handlers {
				h := h
				b.safeCall(func() { h(evt) })
			}
		}
	}
}

func (b *Bus) consumeStatus() {
	defer b.wg.Done()
	for {
		select {
		case <-b.drainCh:
			b.drainStatus()
			return
		case evt := <-b.status:
			b.mu.RLock()
			handlers := b.statusHandlers
			b.mu.RUnlock()
			for _, h := range handlers {
				h := h
				b.safeCall(func() { h(evt) })
			}
		}
	}
}

func (b *Bus) consumeDLQ() {
	defer b.wg.Done()
	for {
		select {
		case <-b.drainCh:
			b.drainDLQ()
			return
		case evt := <-b.dlq:
			b.mu.RLock()
			handlers := b.dlqHandlers
			b.mu.RUnlock()
			for _, h := range handlers {
				h := h
				b.safeCall(func() { h(evt) })
			}
		}
	}
}

func (b *Bus) consumeLifecycle() {
	defer b.wg.Done()
	for {
		select {
		case <-b.drainCh:
			b.drainLifecycle()
			return
		case evt := <-b.lifecycle:
			b.mu.RLock()
			handlers := b.lifecycleHandlers
			b.mu.RUnlock()
			for _, h := range handlers {
				h := h
				b.safeCall(func() { h(evt) })
			}
		}
	}
}

func (b *Bus) drainInbound() {
	for {
		select {
		case <-b.inbound:
		default:
			return
		}
	}
}

func (b *Bus) drainOutbound() {
	for {
		select {
		case <-b.outbound:
		default:
			return
		}
	}
}

func (b *Bus) drainStatus() {
	for {
		select {
		case <-b.status:
		default:
			return
		}
	}
}

func (b *Bus) drainDLQ() {
	for {
		select {
		case <-b.dlq:
		default:
			return
		}
	}
}

func (b *Bus) drainLifecycle() {
	for {
		select {
		case <-b.lifecycle:
		default:
			return
		}
	}
}
