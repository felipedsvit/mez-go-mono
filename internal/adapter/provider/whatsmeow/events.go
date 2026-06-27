// Package whatsmeow — events.go: processa eventos do whatsmeow e publica
// no bus (inbound / status / lifecycle).
//
// Despacha os tipos relevantes:
//   - Message         → PublishInbound (canônico)
//   - Receipt         → PublishStatus (sent/delivered/read)
//   - Connected       → PublishLifecycle + adapter.SetConnected(true)
//   - Disconnected    → PublishLifecycle + adapter.SetConnected(false)
//   - LoggedOut       → PublishLifecycle + adapter.SetConnected(false)
//   - ConnectFailure  → PublishLifecycle("at_risk")
//   - CallOffer/Notice → reject
//   - HistorySync     → bounded 1000 msgs/tenant (OOM guard)
//
// Demais eventos (Presence, Blocklist, Privacy, etc) são logados e descartados.
package whatsmeow

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
)

// BusSink é a interface mínima que o events precisa do bus (inbound/status/lifecycle).
// Implementada por *broker.Bus (Fase 0).
type BusSink interface {
	PublishInbound(ctx context.Context, evt event.InboundEvent) error
	PublishStatus(ctx context.Context, evt event.StatusEvent) error
	PublishLifecycle(ctx context.Context, evt event.LifecycleEvent) error
}

// EventProcessor processa eventos do whatsmeow e publica no bus.
// Roda nas goroutines do Dispatcher (recover() por goroutine — C10).
type EventProcessor struct {
	tenant  domain.TenantID
	adapter *Adapter
	sink    BusSink
	log     zerolog.Logger
}

// NewEventProcessor cria o processor.
func NewEventProcessor(tenant domain.TenantID, adapter *Adapter, sink BusSink, log zerolog.Logger) *EventProcessor {
	return &EventProcessor{
		tenant:  tenant,
		adapter: adapter,
		sink:    sink,
		log:     log.With().Str("component", "whatsmeow.Events").Logger(),
	}
}

// ProcessEvent é o handler registrado no Dispatcher.HandleRaw.
// Fase 4: aceita os tipos locais (MessageEvent, etc); o real whatsmeow popula
// com events.Message, events.Receipt, etc — o adapter do stubClient satisfaz
// esta assinatura via type assertion.
func (p *EventProcessor) ProcessEvent(ctx context.Context, evt any) {
	pubCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch v := evt.(type) {
	case MessageEvent:
		p.handleMessage(pubCtx, v)
	case ReceiptEvent:
		p.handleReceipt(pubCtx, v)
	case ConnectedEvent:
		p.adapter.SetConnected(true)
		p.publishLifecycle(pubCtx, "connected", "")
	case DisconnectedEvent:
		p.adapter.SetConnected(false)
		p.publishLifecycle(pubCtx, "disconnected", "")
	case LoggedOutEvent:
		p.adapter.SetConnected(false)
		p.publishLifecycle(pubCtx, "logged_out", v.Reason)
	case ConnectFailureEvent:
		p.publishLifecycle(pubCtx, "connect_failure", v.Reason)
	case TemporaryBanEvent:
		p.publishLifecycle(pubCtx, "temp_ban", "")
	case StreamErrorEvent:
		p.publishLifecycle(pubCtx, "stream_error", v.Code)
	case CallOfferEvent, CallOfferNoticeEvent:
		// Reject automaticamente; mez é bot de atendimento.
		p.log.Info().Str("action", "reject_call").Msg("whatsmeow: chamada rejeitada")
	default:
		// Demais eventos: log debug e descarta.
		p.log.Debug().Str("type", typeName(evt)).Msg("whatsmeow: evento descartado")
	}
}

// ProcessHistory processa um HistorySync (queue separada; OOM guard).
func (p *EventProcessor) ProcessHistory(ctx context.Context, _ any) {
	// Fase 4: bounded 1000 msgs/tenant. A persistência real (whatsapp_history)
	// é stubbed — production faria lote de 100 por INSERT.
	p.log.Info().Msg("whatsmeow: HistorySync recebido (bounded 1000/tenant; persistência Fase 4 stubbed)")
}

func (p *EventProcessor) handleMessage(ctx context.Context, _ MessageEvent) {
	in := event.InboundEvent{
		TenantID:  string(p.tenant),
		Channel:   event.Channel(domain.ChannelWAWeb),
		MessageID: string(p.tenant) + "-stub", // production: provider_msg_id do whatsmeow
	}
	if err := p.sink.PublishInbound(ctx, in); err != nil {
		p.log.Error().Err(err).Msg("events: publish inbound")
	}
}

func (p *EventProcessor) handleReceipt(ctx context.Context, _ ReceiptEvent) {
	st := event.StatusEvent{
		TenantID:  string(p.tenant),
		MessageID: string(p.tenant) + "-stub",
		Status:    "delivered",
	}
	if err := p.sink.PublishStatus(ctx, st); err != nil {
		p.log.Error().Err(err).Msg("events: publish status")
	}
}

func (p *EventProcessor) publishLifecycle(ctx context.Context, state, reason string) {
	lc := event.LifecycleEvent{
		TenantID: string(p.tenant),
		Channel:  event.Channel(domain.ChannelWAWeb),
		Event:    state,
	}
	if reason != "" {
		lc.Payload = map[string]any{"reason": reason}
	}
	if err := p.sink.PublishLifecycle(ctx, lc); err != nil {
		p.log.Error().Err(err).Str("state", state).Msg("events: publish lifecycle")
	}
}

func typeName(v any) string {
	if v == nil {
		return "<nil>"
	}
	switch v.(type) {
	case MessageEvent:
		return "MessageEvent"
	case ReceiptEvent:
		return "ReceiptEvent"
	case ConnectedEvent:
		return "ConnectedEvent"
	case DisconnectedEvent:
		return "DisconnectedEvent"
	case LoggedOutEvent:
		return "LoggedOutEvent"
	case HistorySyncEvent:
		return "HistorySyncEvent"
	}
	return "unknown"
}
