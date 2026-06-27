// Package relay implementa o relay do outbox (#38 + #52) do mez-go-mono.
//
// O relay drena mensagens em status='pending' de outbound_events e chama
// o Sender registrado no SenderRegistry. Sender é uma interface
// desacoplada (port.Sender) — Fase 2 usava NoopSender; Fase 3 pluga os
// adapters reais (WABA/IG/MSG/TG) via registry.
//
// Política de drain (D3):
//   - Sinal in-process via Notify() — rápido, sem latência de poll.
//   - Poll de fallback (5s default) — cobre crash entre enqueue e notify.
//
// Política de retry (D3 reforço + #52):
//   - Cada falha incrementa attempts (via MarkFailed).
//   - Quando attempts >= MaxAttempts, mensagem vai para DLQ (MarkDLQ +
//     bus.PublishDLQ) e não é mais retentada.
package relay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Config configura o relay.
type Config struct {
	PollInterval time.Duration // default 5s
	BatchSize    int           // default 32
	MaxAttempts  int           // default 5; após isso, vai para DLQ
}

// Relay é o loop de drain.
type Relay struct {
	outbox   port.OutboxRelay
	registry port.SenderRegistry
	bus      *broker.Bus
	cfg      Config
	log      zerolog.Logger

	notifyCh chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New cria o relay. registry pode ser nil na Fase 2 (Noop); Fase 3 sempre
// passa registry. bus pode ser nil se o caller não quer DLQ events.
func New(outbox port.OutboxRelay, registry port.SenderRegistry, bus *broker.Bus, cfg Config, log zerolog.Logger) *Relay {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 32
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 5
	}
	return &Relay{
		outbox:   outbox,
		registry: registry,
		bus:      bus,
		cfg:      cfg,
		log:      log,
		notifyCh: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
}

// Notify sinaliza que há novas mensagens prontas para drenar.
// Non-blocking: se já houver um sinal pendente, descarta (o poll cobre).
func (r *Relay) Notify() {
	select {
	case r.notifyCh <- struct{}{}:
	default:
	}
}

// Run inicia o loop. Bloqueia até ctx ser cancelado ou Stop() chamado.
func (r *Relay) Run(ctx context.Context) error {
	r.log.Info().
		Dur("poll_interval", r.cfg.PollInterval).
		Int("batch_size", r.cfg.BatchSize).
		Int("max_attempts", r.cfg.MaxAttempts).
		Msg("outbox relay: starting")

	// Boot: drena tudo (D3 + C1).
	if err := r.drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.log.Error().Err(err).Msg("outbox relay: boot drain failed")
	}

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info().Msg("outbox relay: context cancelled, stopping")
			return nil
		case <-r.stopCh:
			r.log.Info().Msg("outbox relay: stop signal, stopping")
			return nil
		case <-r.notifyCh:
			if err := r.drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.log.Error().Err(err).Msg("outbox relay: drain failed (notify)")
			}
		case <-ticker.C:
			if err := r.drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.log.Error().Err(err).Msg("outbox relay: drain failed (poll)")
			}
		}
	}
}

// Stop sinaliza parada. Idempotente.
func (r *Relay) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}

// drain lê um batch e processa cada mensagem.
func (r *Relay) drain(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		msgs, err := r.outbox.ClaimNext(ctx, r.cfg.BatchSize)
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if len(msgs) == 0 {
			return nil
		}

		for _, m := range msgs {
			r.process(ctx, m)
		}

		// Se Claim devolveu menos que o batch, não há mais nada.
		if len(msgs) < r.cfg.BatchSize {
			return nil
		}
	}
}

// process tenta enviar uma mensagem. Marca sent/failed/dlq conforme resultado.
func (r *Relay) process(ctx context.Context, m domain.Message) {
	// Sem registry: nada a fazer. (Fase 2 behaviour; log e deixa pending.)
	if r.registry == nil {
		r.log.Warn().
			Str("message", string(m.ID)).
			Msg("outbox: registry nil; mensagem permanece pending")
		return
	}

	sender, err := r.registry.Get(ctx, m.TenantID, m.Channel)
	if err != nil {
		// Sender não registrado → registra como failed (vai tentar de novo
		// na próxima vez que o registry for populado). Se o canal realmente
		// não existe, MaxAttempts vai movê-lo para DLQ.
		r.markFailed(ctx, m.ID, fmt.Errorf("registry: %w", err))
		return
	}

	req := port.OutboundRequest{
		TenantID:       m.TenantID,
		Channel:        m.Channel,
		MessageID:      m.ID,
		ConversationID: m.ConversationID,
		ContactID:      m.ContactID,
		PeerID:         string(m.ContactID),
		Type:           m.Type,
		Body:           m.Body,
		Metadata:       m.Metadata,
	}

	providerID, err := sender.Send(ctx, req)
	if err != nil {
		r.handleSendError(ctx, m, err)
		return
	}

	if err := r.outbox.MarkSent(ctx, m.ID); err != nil {
		r.log.Error().Err(err).Str("message", string(m.ID)).Msg("outbox: mark sent")
		return
	}
	r.log.Info().
		Str("message", string(m.ID)).
		Str("channel", string(m.Channel)).
		Str("provider_id", providerID).
		Msg("outbox: sent")
}

// handleSendError trata o erro de Send: incrementa attempts e, se atingiu
// MaxAttempts, move para DLQ.
func (r *Relay) handleSendError(ctx context.Context, m domain.Message, sendErr error) {
	r.markFailed(ctx, m.ID, sendErr)

	attempts, err := r.outbox.GetAttempts(ctx, m.ID)
	if err != nil {
		r.log.Error().Err(err).Str("message", string(m.ID)).Msg("outbox: get attempts")
		return
	}

	if attempts >= r.cfg.MaxAttempts {
		if err := r.outbox.MarkDLQ(ctx, m.ID, sendErr); err != nil {
			r.log.Error().Err(err).Str("message", string(m.ID)).Msg("outbox: mark dlq")
			return
		}
		r.log.Warn().
			Str("message", string(m.ID)).
			Str("channel", string(m.Channel)).
			Int("attempts", attempts).
			Err(sendErr).
			Msg("outbox: dlq after max attempts")
		if r.bus != nil {
			r.bus.PublishDLQ(event.DLQEvent{
				TenantID:  string(m.TenantID),
				Channel:   event.Channel(m.Channel),
				MessageID: string(m.ID),
				Error:     sendErr.Error(),
			})
		}
	}
}

// markFailed é um helper que loga erros de MarkFailed (não os propaga para
// não parar o drain).
func (r *Relay) markFailed(ctx context.Context, id domain.MessageID, sendErr error) {
	if err := r.outbox.MarkFailed(ctx, id, sendErr); err != nil {
		r.log.Error().Err(err).Str("message", string(id)).Msg("outbox: mark failed")
	}
	r.log.Warn().Err(sendErr).Str("message", string(id)).Msg("outbox: send failed")
}
