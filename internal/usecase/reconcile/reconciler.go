// Package reconcile implementa o Reconciler inbound (#39) do mez-go-mono.
//
// O Reconciler é a peça que fecha o ciclo C1: cobre mensagens em status
// 'received' que foram persistidas mas não roteadas (e.g. crash entre
// commit do insert e o consumo pelo bus consumer). Roda:
//   - uma vez no boot (cobre kill -9 antes do Run completo)
//   - periodicamente (default 30s) para cobrir drift
//
// Iteração cross-tenant via mez_platform (RunAsPlatform); uso de
// FOR UPDATE SKIP LOCKED na query para não colidir com o routing
// consumer que também processa essas mensagens.
package reconcile

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// InboundEventsRepo é o port mínimo que o reconciler precisa. Mantido
// local (não em core/port) porque é a única peça que precisa enxergar
// cross-tenant. A interface postgres.InboundEventsRepo implementa isto.
type InboundEventsRepo interface {
	SelectUnroutedMessages(ctx context.Context, batchSize int) ([]domain.Message, error)
	MarkRouted(ctx context.Context, id domain.MessageID) error
	CountUnrouted(ctx context.Context) (int, error)
}

// AssignFn é a função que o reconciler chama para atribuir uma mensagem.
// Mantida como função (não interface) para que a Fase 5 plugue ACD sem
// mudar este código.
type AssignFn func(ctx context.Context, m domain.Message) error

// Reconciler fecha o ciclo de mensagens órfãs.
type Reconciler struct {
	repo      InboundEventsRepo
	assign    AssignFn
	log       zerolog.Logger
	interval  time.Duration
	batchSize int

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// Config configura o reconciler.
type Config struct {
	Interval  time.Duration // default 30s
	BatchSize int           // default 100
}

// New cria o reconciler.
func New(repo InboundEventsRepo, assign AssignFn, cfg Config, log zerolog.Logger) *Reconciler {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return &Reconciler{
		repo:      repo,
		assign:    assign,
		log:       log,
		interval:  cfg.Interval,
		batchSize: cfg.BatchSize,
		stopCh:    make(chan struct{}),
	}
}

// ReconcileAll varre todos os tenants e processa mensagens em 'received'.
// Usado no boot e em testes.
func (r *Reconciler) ReconcileAll(ctx context.Context) (int, error) {
	processed := 0
	for {
		if err := ctx.Err(); err != nil {
			return processed, err
		}
		msgs, err := r.repo.SelectUnroutedMessages(ctx, r.batchSize)
		if err != nil {
			return processed, err
		}
		if len(msgs) == 0 {
			return processed, nil
		}
		for _, m := range msgs {
			if err := r.assign(ctx, m); err != nil {
				r.log.Error().Err(err).
					Str("message", string(m.ID)).
					Str("tenant", string(m.TenantID)).
					Msg("reconciler: assign failed")
				continue
			}
			if err := r.repo.MarkRouted(ctx, m.ID); err != nil {
				r.log.Error().Err(err).
					Str("message", string(m.ID)).
					Msg("reconciler: mark routed failed")
				continue
			}
			processed++
		}
		if len(msgs) < r.batchSize {
			return processed, nil
		}
	}
}

// Run inicia o loop: drena no boot, depois tick periódico.
func (r *Reconciler) Run(ctx context.Context) error {
	r.log.Info().
		Dur("interval", r.interval).
		Int("batch_size", r.batchSize).
		Msg("reconciler: starting")

	// Boot: cobre kill -9 antes do Run.
	if n, err := r.ReconcileAll(ctx); err != nil {
		r.log.Error().Err(err).Msg("reconciler: boot sweep failed")
	} else {
		r.log.Info().Int("processed", n).Msg("reconciler: boot sweep complete")
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info().Msg("reconciler: context cancelled, stopping")
			return nil
		case <-r.stopCh:
			r.log.Info().Msg("reconciler: stop signal, stopping")
			return nil
		case <-ticker.C:
			if n, err := r.ReconcileAll(ctx); err != nil {
				r.log.Error().Err(err).Msg("reconciler: tick failed")
			} else if n > 0 {
				r.log.Info().Int("processed", n).Msg("reconciler: tick complete")
			}
		}
	}
}

// Stop sinaliza parada. Idempotente.
func (r *Reconciler) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })
}
