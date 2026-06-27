// Package whatsmeow — dispatcher.go: bounded buffers + recover (C10).
//
// Regra crítica de produção: o handler de AddEventHandler NUNCA pode
// bloquear — apenas enfileira (descartando se cheio) e retorna.
// HistorySync vai para uma fila própria (OOM guard) para não competir
// com mensagens.
//
// Em Fase 4 (mono / single-binary), o **recover() por goroutine** é
// mandatório: panic num tenant não pode derrubar o processo.
package whatsmeow

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
)

const (
	eventBuffer   = 2048 // mensagens/recibos/lifecycle
	historyBuffer = 8    // HistorySync (poucos, mas grandes)
)

// Dispatcher desacopla a goroutine do socket do whatsmeow do processamento.
type Dispatcher struct {
	logger zerolog.Logger

	queue   chan any
	history chan any
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	once    sync.Once
}

// NewDispatcher cria o dispatcher.
func NewDispatcher(log zerolog.Logger) *Dispatcher {
	return &Dispatcher{
		logger:  log.With().Str("component", "whatsmeow.Dispatcher").Logger(),
		queue:   make(chan any, eventBuffer),
		history: make(chan any, historyBuffer),
	}
}

// Start sobe as goroutines de processamento (eventos e histórico).
// Chamar exatamente uma vez (idempotente via sync.Once).
func (d *Dispatcher) Start(ctx context.Context, onEvent func(context.Context, any), onHistory func(context.Context, any)) {
	d.once.Do(func() {
		ctx, d.cancel = context.WithCancel(ctx)
		d.wg.Add(2)
		go d.loopEvents(ctx, onEvent)
		go d.loopHistory(ctx, onHistory)
	})
}

// Stop encerra o processamento. Idempotente.
func (d *Dispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
}

// HandleRaw é o handler registrado no whatsmeow. Roda na goroutine do
// socket: só enfileira e retorna imediatamente (drop-safe).
func (d *Dispatcher) HandleRaw(evt any) {
	switch evt.(type) {
	case MessageEvent, ReceiptEvent,
		ConnectedEvent, DisconnectedEvent, LoggedOutEvent,
		ConnectFailureEvent, TemporaryBanEvent, StreamErrorEvent,
		CallOfferEvent, CallOfferNoticeEvent, HistorySyncEvent,
		PresenceEvent, ChatPresenceEvent:
		select {
		case d.queue <- evt:
		default:
			d.logger.Warn().Msg("dispatcher: buffer de eventos cheio; drop")
		}
	default:
		// Demais eventos não interessam à ingestão atual.
	}
}

// HandleHistory enfileira um HistorySync.
func (d *Dispatcher) HandleHistory(evt any) {
	select {
	case d.history <- evt:
	default:
		d.logger.Warn().Msg("dispatcher: fila de HistorySync cheia; drop (OOM guard)")
	}
}

func (d *Dispatcher) loopEvents(ctx context.Context, onEvent func(context.Context, any)) {
	defer d.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error().Interface("panic", r).Msg("dispatcher: panic em loopEvents (C10); recuperado")
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-d.queue:
			func() {
				defer func() {
					if r := recover(); r != nil {
						d.logger.Error().Interface("panic", r).Msg("dispatcher: panic em handler de evento (C10); recuperado")
					}
				}()
				onEvent(ctx, evt)
			}()
		}
	}
}

func (d *Dispatcher) loopHistory(ctx context.Context, onHistory func(context.Context, any)) {
	defer d.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			d.logger.Error().Interface("panic", r).Msg("dispatcher: panic em loopHistory (C10); recuperado")
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-d.history:
			func() {
				defer func() {
					if r := recover(); r != nil {
						d.logger.Error().Interface("panic", r).Msg("dispatcher: panic em handler de HistorySync (C10); recuperado")
					}
				}()
				onHistory(ctx, evt)
			}()
		}
	}
}

// Tipos de evento (placeholders que o whatsmeow real popula). Mantidos
// locais para desacoplar testes da lib.
type (
	MessageEvent         struct{ Raw any }
	ReceiptEvent         struct{ Raw any }
	ConnectedEvent       struct{}
	DisconnectedEvent    struct{}
	LoggedOutEvent       struct{ Reason string }
	ConnectFailureEvent  struct{ Reason string }
	TemporaryBanEvent    struct{ Code int }
	StreamErrorEvent     struct{ Code string }
	CallOfferEvent       struct{ From, ID string }
	CallOfferNoticeEvent struct{ From, ID string }
	HistorySyncEvent     struct {
		Data     any
		JID      string
		Messages []HistoryMessage
	}
	PresenceEvent     struct{ From, State string }
	ChatPresenceEvent struct{ From, State string }
)
