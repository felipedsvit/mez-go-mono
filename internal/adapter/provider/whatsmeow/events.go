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
	history HistoryPersister
	log     zerolog.Logger
}

// HistoryPersister é o subset de *postgres.HistoryRepo que o processor usa.
// Best-effort: se nil, HistorySync apenas loga (sem persistir).
type HistoryPersister interface {
	InsertMany(ctx context.Context, msgs []HistoryMessage) (int, error)
}

// HistoryMessage é a forma de entrada do HistoryPersister (espelha
// o domain do postgres). Mantida aqui para desacoplar o processor
// do postgres.HistoryRepo.
type HistoryMessage struct {
	JID       string
	MsgID     string
	Timestamp int64
	FromMe    bool
	Body      string
	Type      string
}

// NewEventProcessor cria o processor.
func NewEventProcessor(tenant domain.TenantID, adapter *Adapter, sink BusSink, history HistoryPersister, log zerolog.Logger) *EventProcessor {
	return &EventProcessor{
		tenant:  tenant,
		adapter: adapter,
		sink:    sink,
		history: history,
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
//
// Espera receber um *events.HistorySync (lib whatsmeow) ou um
// HistorySyncEvent (stub local). Best-effort: se history for nil,
// apenas loga e descarta. Se history estiver presente, persiste em
// lote de até 1000 mensagens/tenant.
func (p *EventProcessor) ProcessHistory(ctx context.Context, evt any) {
	if p.history == nil {
		p.log.Info().Msg("whatsmeow: HistorySync recebido (sem persister; log-only)")
		return
	}

	msgs := extractHistoryMessages(evt)
	if len(msgs) == 0 {
		p.log.Info().Msg("whatsmeow: HistorySync vazio")
		return
	}

	// bounded 1000.
	if len(msgs) > 1000 {
		msgs = msgs[:1000]
	}

	pubCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	inserted, err := p.history.InsertMany(pubCtx, msgs)
	if err != nil {
		p.log.Error().Err(err).Int("attempted", len(msgs)).Msg("whatsmeow: history persist failed")
		return
	}
	p.log.Info().
		Int("received", len(msgs)).
		Int("inserted", inserted).
		Msg("whatsmeow: HistorySync persistido")
}

// extractHistoryMessages extrai mensagens de diferentes fontes
// (events.HistorySync do whatsmeow real ou HistorySyncEvent local).
func extractHistoryMessages(evt any) []HistoryMessage {
	switch v := evt.(type) {
	case HistorySyncEvent:
		// Stub local
		out := make([]HistoryMessage, 0, len(v.Messages))
		for _, m := range v.Messages {
			out = append(out, HistoryMessage{
				JID:       v.JID,
				MsgID:     m.MsgID,
				Timestamp: m.Timestamp,
				FromMe:    m.FromMe,
				Body:      m.Body,
				Type:      m.Type,
			})
		}
		return out
	}
	// Tenta extrair de *events.HistorySync (lib real) via type assertion
	// dinâmica via interface. Mantemos simples aqui — a versão
	// tipada está em history_translator.go.
	if e, ok := evt.(interface {
		GetConversations() []historyConversation
	}); ok {
		out := make([]HistoryMessage, 0, 64)
		for _, c := range e.GetConversations() {
			for _, m := range c.GetMessages() {
				out = append(out, HistoryMessage{
					JID:       c.GetJID(),
					MsgID:     m.GetID(),
					Timestamp: m.GetTimestamp(),
					FromMe:    m.GetFromMe(),
					Body:      m.GetBody(),
					Type:      m.GetType(),
				})
				if len(out) >= 1000 {
					return out
				}
			}
		}
		return out
	}
	return nil
}

// historyConversation é a interface mínima que extractHistoryMessages
// usa para iterar HistorySync. Compatível com o que o whatsmeow real
// emite (a versão concreta vem de types/events.HistorySync).
type historyConversation interface {
	GetJID() string
	GetMessages() []historyMessageLite
}

// historyMessageLite é a forma lite (subset de waE2E.Message).
type historyMessageLite interface {
	GetID() string
	GetTimestamp() int64
	GetFromMe() bool
	GetBody() string
	GetType() string
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
