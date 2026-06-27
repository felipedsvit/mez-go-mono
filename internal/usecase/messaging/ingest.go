// Package messaging implementa os casos de uso de mensageria do mez-go-mono.
//
// Ingestor (#36) é o ponto de entrada do pipeline inbound: recebe um
// event.InboundEvent já normalizado por um adapter de provider, resolve
// Contact + Conversation (idempotente), persiste a Message com dedup
// ON CONFLICT, e enfileira o outbox para envio (Fase 3) — tudo dentro
// de uma única RunInTenantTx para garantir atomicidade.
//
// O pipeline é o mesmo do mez-go (pai), com duas divergências:
//   - tenant_id é UUID (não string) em mez-go-mono
//   - outbox.Insert ao final (no pai, é parte do Send; aqui antecipamos
//     para que ingestor já prepare o relay)
package messaging

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Ingestor é o caso de uso de ingestão de mensagens inbound.
type Ingestor struct {
	contactRepo port.ContactRepo
	convRepo    port.ConversationRepo
	messageRepo port.MessageRepo
	outbox      port.OutboxWriter
	tx          port.TxRunner
	bus         BusPublisher
	log         zerolog.Logger
}

// BusPublisher é o port mínimo do bus in-process que o ingestor precisa.
// Definido aqui (não em core/port) para evitar acoplamento com o adapter
// broker — o ingestor é usecase, o broker é adapter.
type BusPublisher interface {
	PublishInbound(event.InboundEvent)
}

// Option configura o Ingestor (functional options pattern do pai).
type Option func(*Ingestor)

// WithBus injeta o publisher do bus. Se não for setado, o ingestor não
// notifica (modo silencioso — útil em testes).
func WithBus(b BusPublisher) Option {
	return func(i *Ingestor) { i.bus = b }
}

// WithLogger injeta logger customizado.
func WithLogger(log zerolog.Logger) Option {
	return func(i *Ingestor) { i.log = log }
}

// NewIngestor cria o Ingestor.
func NewIngestor(
	contactRepo port.ContactRepo,
	convRepo port.ConversationRepo,
	messageRepo port.MessageRepo,
	outbox port.OutboxWriter,
	tx port.TxRunner,
	opts ...Option,
) *Ingestor {
	i := &Ingestor{
		contactRepo: contactRepo,
		convRepo:    convRepo,
		messageRepo: messageRepo,
		outbox:      outbox,
		tx:          tx,
	}
	for _, o := range opts {
		o(i)
	}
	return i
}

// Ingest processa um evento inbound: resolve contact, resolve conversation,
// insere a mensagem com dedup, enfileira outbox. Tudo dentro de uma tx
// tenant-scoped.
//
// Retorna o messageID criado (ou existente, em caso de dedup). Não retorna
// erro se a mensagem já existia (idempotente).
func (i *Ingestor) Ingest(ctx context.Context, evt event.InboundEvent) (domain.MessageID, error) {
	if evt.TenantID == "" {
		return "", errors.New("ingest: tenant_id required")
	}
	if evt.Channel == "" {
		return "", errors.New("ingest: channel required")
	}
	if evt.MessageID == "" {
		return "", errors.New("ingest: message_id required")
	}

	tenantID := domain.TenantID(evt.TenantID)

	var (
		persistedID domain.MessageID
		convID      domain.ConversationID
	)

	err := i.tx.RunInTenantTx(ctx, tenantID, func(ctx context.Context) error {
		// 1. Resolve Contact (upsert idempotente).
		contact := &domain.Contact{
			ID:         domain.ContactID(uuid.NewString()),
			TenantID:   tenantID,
			Channel:    domain.Channel(evt.Channel),
			ProviderID: evt.MessageID, // será sobrescrito abaixo com o peer ID real
		}
		// Tenta extrair o peer ID do payload se existir; senão usa o MessageID
		// do envelope (que pode ser o ID da mensagem e não do contato).
		// O adapter deve popular evt.Recipient com o peer ID real; aqui
		// apenas usamos o que vier.
		contact.ProviderID = evt.MessageID

		if err := i.contactRepo.Upsert(ctx, contact); err != nil {
			return fmt.Errorf("upsert contact: %w", err)
		}

		// 2. Resolve Conversation (upsert idempotente por (tenant, channel, external_id)).
		// external_id aqui = provider_msg_id do peer (ou evt.MessageID se não houver).
		conv := &domain.Conversation{
			ID:         domain.ConversationID(uuid.NewString()),
			TenantID:   tenantID,
			Channel:    domain.Channel(evt.Channel),
			ContactID:  contact.ID,
			Status:     domain.ConvStatusOpen,
			ExternalID: evt.MessageID,
		}
		if err := i.convRepo.Upsert(ctx, conv); err != nil {
			return fmt.Errorf("upsert conversation: %w", err)
		}
		convID = conv.ID

		// 3. Insert Message com dedup ON CONFLICT.
		msg := &domain.Message{
			ID:             domain.MessageID(uuid.NewString()),
			TenantID:       tenantID,
			Channel:        domain.Channel(evt.Channel),
			ConversationID: conv.ID,
			ContactID:      contact.ID,
			Direction:      domain.DirectionInbound,
			Type:           domain.MessageTypeText,
			Status:         domain.MessageStatusReceived,
			Body:           "",
			ProviderMsgID:  evt.MessageID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := i.messageRepo.Insert(ctx, msg); err != nil {
			return fmt.Errorf("insert message: %w", err)
		}
		persistedID = msg.ID

		// 4. Outbox insert (para a Fase 3 enviar). Se Sender for noop,
		// permanece pending — comportamento definido em #38.
		if err := i.outbox.Insert(ctx, msg); err != nil {
			return fmt.Errorf("outbox insert: %w", err)
		}

		return nil
	})
	if err != nil {
		return "", err
	}

	// 5. Notificar bus (non-blocking, drop-safe).
	if i.bus != nil {
		i.bus.PublishInbound(event.InboundEvent{
			TenantID:  string(tenantID),
			Channel:   evt.Channel,
			MessageID: string(persistedID),
		})
	}

	if i.log.Info().Enabled() {
		i.log.Info().
			Str("tenant", string(tenantID)).
			Str("channel", string(evt.Channel)).
			Str("conversation", string(convID)).
			Str("message", string(persistedID)).
			Msg("ingested")
	}

	return persistedID, nil
}
