// Package relay implementa o relay do outbox (#38) do mez-go-mono.
//
// O relay drena mensagens em status='pending' de outbound_events e chama
// o Sender registrado. Sender é uma interface vazia na Fase 2 (NoopSender);
// os adapters reais (WABA, IG, Messenger, TG, WhatsMeow) são plugados na
// Fase 3 sem mudar a infraestrutura.
//
// Política de drain (D3):
//   - Sinal in-process via Notify() — rápido, sem latência de poll.
//   - Poll de fallback (5s default) — cobre crash entre enqueue e notify.
//
// O relay também drena imediatamente no boot, cobrindo o caso de crash
// entre commit e notify.
package relay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Sender é a porta outbound. A Fase 2 usa NoopSender; Fase 3 implementa
// os adapters reais.
type Sender interface {
	Send(ctx context.Context, m domain.Message) (providerMsgID string, err error)
}

// NoopSender é o Sender default da Fase 2: loga warn e retorna
// ErrSenderNotImplemented. O relay trata esse erro especialmente: deixa
// a mensagem em pending e tenta de novo no próximo tick.
type NoopSender struct {
	log zerolog.Logger
}

// NewNoopSender cria o sender noop.
func NewNoopSender(log zerolog.Logger) *NoopSender {
	return &NoopSender{log: log}
}

// ErrSenderNotImplemented é retornado pelo NoopSender.
var ErrSenderNotImplemented = errors.New("sender não registrado (fase 3)")

// Send implementa Sender. Loga warn e retorna o erro sentinel.
func (n *NoopSender) Send(ctx context.Context, m domain.Message) (string, error) {
	n.log.Warn().
		Str("tenant", string(m.TenantID)).
		Str("message", string(m.ID)).
		Str("channel", string(m.Channel)).
		Msg("outbox: noop sender; mensagem permanece pending (fase 3 implementa adapter)")
	return "", ErrSenderNotImplemented
}

// Config configura o relay.
type Config struct {
	PollInterval time.Duration // default 5s
	BatchSize    int           // default 32
	MaxAttempts  int           // default 5; após isso, vai para DLQ
}

// Relay é o loop de drain.
type Relay struct {
	outbox port.OutboxRelay
	sender Sender
	cfg    Config
	log    zerolog.Logger

	notifyCh chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// New cria o relay.
func New(outbox port.OutboxRelay, sender Sender, cfg Config, log zerolog.Logger) *Relay {
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
		sender:   sender,
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
//
// Comportamento:
//   - Drena imediatamente no start (cobre crash entre enqueue e notify).
//   - Depois, alterna entre sinal (notifyCh) e poll (ticker).
func (r *Relay) Run(ctx context.Context) error {
	r.log.Info().
		Dur("poll_interval", r.cfg.PollInterval).
		Int("batch_size", r.cfg.BatchSize).
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
	providerID, err := r.sender.Send(ctx, m)

	// NoopSender: deixa pending e loga warn; tenta de novo no próximo tick.
	if errors.Is(err, ErrSenderNotImplemented) {
		r.log.Warn().
			Str("message", string(m.ID)).
			Str("channel", string(m.Channel)).
			Msg("outbox: sender noop; mensagem permanece pending")
		return
	}

	if err != nil {
		// Marca failed e checa attempts.
		if markErr := r.outbox.MarkFailed(ctx, m.ID, err); markErr != nil {
			r.log.Error().Err(markErr).Str("message", string(m.ID)).Msg("outbox: mark failed")
		}
		r.log.Warn().Err(err).Str("message", string(m.ID)).Msg("outbox: send failed")

		// Verifica se atingiu MaxAttempts para mover para DLQ.
		// (Fase 2: simplificado — não lemos attempts aqui; deixamos para Fase 3.)
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
