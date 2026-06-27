// Package e2e — testes E2E do pipeline outbound.
//
// Estes testes montam o bus + registry in-memory, registram um recorder
// por canal, e exercitam o caminho PublishOutbound → handler → sender.
package e2e

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

func TestE2E_Outbound_PublishAndDeliver(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	// Recorder para o canal WABA.
	rec := NewSenderRecorder(domain.ChannelWABA, port.CapabilitySet{port.CapText: true, port.CapMedia: true})
	h.RegisterSender(rec)

	// Handler de outbound: pega o evento, busca o sender pelo canal, chama Send.
	h.Bus.SubscribeOutbound(func(evt event.OutboundEvent) {
		tenantID := domain.TenantID(evt.TenantID)
		snd, err := h.Reg.Get(context.Background(), tenantID, domain.Channel(evt.Channel))
		if err != nil {
			t.Errorf("registry.Get: %v", err)
			return
		}
		_, err = snd.Send(context.Background(), port.OutboundRequest{
			TenantID:  tenantID,
			Channel:   domain.Channel(evt.Channel),
			MessageID: domain.MessageID(evt.MessageID),
			PeerID:    "5511999999999",
			Type:      domain.MessageTypeText,
			Body:      "hello",
		})
		if err != nil {
			t.Errorf("Send: %v", err)
		}
	})

	// Publica 3 mensagens outbound.
	for i := 0; i < 3; i++ {
		h.Bus.PublishOutbound(event.OutboundEvent{
			TenantID:  "tenant-1",
			Channel:   event.ChannelWABA,
			MessageID: "msg-" + itoa(i),
		})
	}

	if !WaitForOutboundCalls(t, rec, 3, 2*time.Second) {
		t.Fatalf("expected 3 calls, got %d", len(rec.Calls()))
	}

	calls := rec.Calls()
	for i, call := range calls {
		if call.TenantID != domain.TenantID("tenant-1") {
			t.Errorf("call %d: tenant = %q, want tenant-1", i, call.TenantID)
		}
		if call.Channel != domain.ChannelWABA {
			t.Errorf("call %d: channel = %q, want waba", i, call.Channel)
		}
		if call.Body != "hello" {
			t.Errorf("call %d: body = %q, want hello", i, call.Body)
		}
	}
}

func TestE2E_Outbound_MultipleChannels_Isolated(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	waba := NewSenderRecorder(domain.ChannelWABA, port.CapabilitySet{port.CapText: true})
	tg := NewSenderRecorder(domain.ChannelTGBot, port.CapabilitySet{port.CapText: true})
	h.RegisterSender(waba)
	h.RegisterSender(tg)

	h.Bus.SubscribeOutbound(func(evt event.OutboundEvent) {
		tenantID := domain.TenantID(evt.TenantID)
		snd, err := h.Reg.Get(context.Background(), tenantID, domain.Channel(evt.Channel))
		if err != nil {
			t.Errorf("registry.Get(%s): %v", evt.Channel, err)
			return
		}
		_, _ = snd.Send(context.Background(), port.OutboundRequest{
			TenantID:  tenantID,
			Channel:   domain.Channel(evt.Channel),
			MessageID: domain.MessageID(evt.MessageID),
			PeerID:    "x",
			Type:      domain.MessageTypeText,
			Body:      "x",
		})
	})

	h.Bus.PublishOutbound(event.OutboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "w1"})
	h.Bus.PublishOutbound(event.OutboundEvent{TenantID: "t1", Channel: event.ChannelTGBot, MessageID: "t1"})
	h.Bus.PublishOutbound(event.OutboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "w2"})

	if !WaitForOutboundCalls(t, waba, 2, 2*time.Second) {
		t.Errorf("WABA: expected 2 calls, got %d", len(waba.Calls()))
	}
	if !WaitForOutboundCalls(t, tg, 1, 2*time.Second) {
		t.Errorf("TG: expected 1 call, got %d", len(tg.Calls()))
	}
}

func TestE2E_Outbound_UnknownChannel_HandlerReportsError(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	var errCount atomic.Int32
	var lastErr atomic.Value
	h.Bus.SubscribeOutbound(func(evt event.OutboundEvent) {
		_, err := h.Reg.Get(context.Background(), domain.TenantID(evt.TenantID), domain.Channel(evt.Channel))
		if err != nil {
			errCount.Add(1)
			lastErr.Store(err)
		}
	})

	// Canal não registrado.
	h.Bus.PublishOutbound(event.OutboundEvent{
		TenantID:  "t1",
		Channel:   event.Channel("nonexistent"),
		MessageID: "m1",
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if errCount.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if errCount.Load() != 1 {
		t.Fatalf("expected 1 error from handler, got %d", errCount.Load())
	}
	got := lastErr.Load()
	if !errors.Is(got.(error), port.ErrSenderNotRegistered) {
		t.Errorf("err = %v, want ErrSenderNotRegistered", got)
	}
}

func TestE2E_Outbound_SenderError_PropagatesAsDLQ(t *testing.T) {
	t.Parallel()

	h := NewHarness(t)

	// Sender que sempre falha.
	rec := NewSenderRecorder(domain.ChannelWABA, port.CapabilitySet{port.CapText: true})
	rec.SendErr = errors.New("provider 5xx")
	h.RegisterSender(rec)

	var dlqEvents atomic.Int32
	var lastDLQ atomic.Value
	h.Bus.SubscribeDLQ(func(evt event.DLQEvent) {
		dlqEvents.Add(1)
		lastDLQ.Store(evt)
	})

	h.Bus.SubscribeOutbound(func(evt event.OutboundEvent) {
		snd, _ := h.Reg.Get(context.Background(), domain.TenantID(evt.TenantID), domain.Channel(evt.Channel))
		_, err := snd.Send(context.Background(), port.OutboundRequest{
			TenantID:  domain.TenantID(evt.TenantID),
			Channel:   domain.Channel(evt.Channel),
			MessageID: domain.MessageID(evt.MessageID),
			PeerID:    "x", Type: domain.MessageTypeText, Body: "x",
		})
		if err != nil {
			// Publica no DLQ para inspeção.
			h.Bus.PublishDLQ(event.DLQEvent{
				TenantID:  evt.TenantID,
				Channel:   evt.Channel,
				MessageID: evt.MessageID,
				Error:     err.Error(),
			})
		}
	})

	h.Bus.PublishOutbound(event.OutboundEvent{TenantID: "t1", Channel: event.ChannelWABA, MessageID: "m1"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if dlqEvents.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dlqEvents.Load() != 1 {
		t.Fatalf("expected 1 DLQ event, got %d", dlqEvents.Load())
	}
	dlq := lastDLQ.Load().(event.DLQEvent)
	if dlq.Error != "provider 5xx" {
		t.Errorf("dlq.Error = %q, want 'provider 5xx'", dlq.Error)
	}
	if dlq.MessageID != "m1" {
		t.Errorf("dlq.MessageID = %q, want m1", dlq.MessageID)
	}
}

func TestE2E_Outbound_Drain_StopsProcessing(t *testing.T) {
	t.Parallel()

	// Não usamos NewHarness porque queremos controlar o drain manualmente.
	log := zerolog.Nop()
	met := metrics.NewRegistry()
	bus := broker.NewBus(broker.BusConfig{OutboundBuffer: 8}, log, met)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = bus.Drain(ctx)
	}()

	var processed atomic.Int32
	bus.SubscribeOutbound(func(_ event.OutboundEvent) {
		processed.Add(1)
	})

	// Publica 3, espera processar.
	for i := 0; i < 3; i++ {
		bus.PublishOutbound(event.OutboundEvent{TenantID: "t", Channel: event.ChannelWABA, MessageID: "m"})
	}
	time.Sleep(100 * time.Millisecond)
	if processed.Load() != 3 {
		t.Errorf("processed = %d, want 3 (pre-drain)", processed.Load())
	}

	// Drena.
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	// Publica 2 mais após drain — não devem ser processadas.
	bus.PublishOutbound(event.OutboundEvent{TenantID: "t", Channel: event.ChannelWABA, MessageID: "post-1"})
	bus.PublishOutbound(event.OutboundEvent{TenantID: "t", Channel: event.ChannelWABA, MessageID: "post-2"})
	time.Sleep(50 * time.Millisecond)
	if processed.Load() != 3 {
		t.Errorf("post-drain processed = %d, want 3 (drained)", processed.Load())
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
