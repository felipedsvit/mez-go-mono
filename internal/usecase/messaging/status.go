// Package messaging — status.go: StatusConsumer (Fase 3 #54).
//
// Subscreve o bus.Status e atualiza messages.status com base no evento.
//
// provider_msg_id (do webhook) → message_id interno (do messages) via
// MessageRepo.UpdateStatusByProvider.
package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// StatusConsumer subscreve o bus.Status e propaga para o MessageRepo.
type StatusConsumer struct {
	bus  *broker.Bus
	repo port.MessageRepo
	log  zerolog.Logger
}

// NewStatusConsumer cria o consumer.
func NewStatusConsumer(bus *broker.Bus, repo port.MessageRepo, log zerolog.Logger) *StatusConsumer {
	return &StatusConsumer{bus: bus, repo: repo, log: log}
}

// Subscribe registra o handler no bus. Idempotente na prática (cada handler
// é um novo consumer registrado).
func (c *StatusConsumer) Subscribe() {
	if c.bus == nil {
		return
	}
	c.bus.SubscribeStatus(c.handle)
}

// handle processa um StatusEvent.
func (c *StatusConsumer) handle(evt event.StatusEvent) {
	if evt.MessageID == "" {
		return
	}
	if evt.Status == "" {
		return
	}

	ctx := context.Background()
	if err := c.repo.UpdateStatus(ctx, domain.MessageID(evt.MessageID), domain.MessageStatus(evt.Status)); err != nil {
		if !errors.Is(err, port.ErrNotFound) {
			c.log.Error().Err(err).Str("message", evt.MessageID).Msg("status: update")
		}
		return
	}
	c.log.Debug().Str("message", evt.MessageID).Str("status", evt.Status).Msg("status: updated")
}

// Start é um alias para Subscribe para padronizar com outros consumers.
func (c *StatusConsumer) Start() error {
	c.Subscribe()
	return nil
}

// Publish é um helper para adapters publicarem status (delega ao bus).
func PublishStatus(bus *broker.Bus, tenant, message, status string) error {
	if bus == nil {
		return fmt.Errorf("bus nil")
	}
	bus.PublishStatus(event.StatusEvent{
		TenantID:  tenant,
		MessageID: message,
		Status:    status,
	})
	return nil
}
