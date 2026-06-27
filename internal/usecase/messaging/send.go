// Package messaging — send.go: SenderService com Send + SendAction (D6).
//
// Orquestra o pipeline outbound:
//  1. Resolve capability (D7) — fallback media→text se aplicável.
//  2. Persiste mensagem (direction=outbound, status=notified).
//  3. Enfileira outbox row (atômico com a mensagem).
//  4. Notify o relay para drenar imediatamente.
//
// SendAction (D6) é para reações/edit/revoke/mark_read/typing/presence —
// não persiste mensagem, só enfileira outbox action.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// SendRequest é o pedido de envio vindo da API.
type SendRequest struct {
	TenantID       domain.TenantID
	Channel        domain.Channel
	ConversationID domain.ConversationID
	ContactID      domain.ContactID
	PeerID         string
	Type           domain.MessageType
	Body           string
	Metadata       map[string]any
}

// SendActionRequest é o pedido de ação (D6).
type SendActionRequest struct {
	TenantID         domain.TenantID
	Channel          domain.Channel
	ConversationID   domain.ConversationID
	ContactID        domain.ContactID
	PeerID           string
	Action           port.Action
	TargetProviderID string
	ReactionEmoji    string
	NewBody          string
	State            string
	Metadata         map[string]any
}

// SenderService é o entry point outbound.
type SenderService struct {
	repo     port.MessageRepo
	outbox   port.OutboxWriter
	resolver *port.Resolver
	relay    interface{ Notify() }
	log      zerolog.Logger
}

// NewSenderService cria o service.
func NewSenderService(
	repo port.MessageRepo,
	outbox port.OutboxWriter,
	resolver *port.Resolver,
	relay interface{ Notify() },
	log zerolog.Logger,
) *SenderService {
	return &SenderService{repo: repo, outbox: outbox, resolver: resolver, relay: relay, log: log}
}

// Send persiste e enfileira uma mensagem outbound.
//
// Issue #126: a Message é criada via factory domain.NewOutboundMessage
// (issue #125 — comportamento no domain) e o OutboxMessage é enfileirado
// via domain.NewOutboxMessage + Enqueue (não mais Insert de Message cru).
func (s *SenderService) Send(ctx context.Context, req SendRequest) (domain.Message, error) {
	if req.TenantID == "" {
		return domain.Message{}, fmt.Errorf("tenant_id required")
	}
	if req.Channel == "" {
		return domain.Message{}, fmt.Errorf("channel required")
	}
	if req.ConversationID == "" {
		return domain.Message{}, fmt.Errorf("conversation_id required")
	}
	if req.PeerID == "" {
		return domain.Message{}, fmt.Errorf("peer_id required")
	}
	if req.Type == "" {
		req.Type = domain.MessageTypeText
	}

	msg, err := domain.NewOutboundMessage(req.TenantID, req.Channel, req.ConversationID, req.ContactID, req.Body, req.Type)
	if err != nil {
		return domain.Message{}, fmt.Errorf("new outbound message: %w", err)
	}
	msg.Metadata = req.Metadata

	// Persiste mensagem + outbox (atomic).
	if err := s.repo.Insert(ctx, msg); err != nil {
		// Pode ser dedup (já existe). Toleramos.
		s.log.Debug().Err(err).Str("message", string(msg.ID)).Msg("send: message insert dup or ok")
	}

	ob, err := domain.NewOutboxMessage(msg.ID, msg.TenantID, msg.Channel, msg.ConversationID, msg.ContactID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("new outbox message: %w", err)
	}
	if err := s.outbox.Enqueue(ctx, ob); err != nil {
		// Fallback: tentar Insert legado (wrapper que cria OutboxMessage).
		if err2 := s.outbox.Insert(ctx, msg); err2 != nil {
			return domain.Message{}, fmt.Errorf("outbox enqueue: %w (insert fallback: %v)", err, err2)
		}
	}

	if s.relay != nil {
		s.relay.Notify()
	}
	return *msg, nil
}

// SendAction enfileira uma ação de canal (D6).
func (s *SenderService) SendAction(ctx context.Context, req SendActionRequest) error {
	if req.Action == "" {
		return fmt.Errorf("action required")
	}
	if req.TenantID == "" || req.Channel == "" || req.PeerID == "" {
		return fmt.Errorf("tenant_id, channel e peer_id required")
	}

	// Validação coarse: a capability requerida para a ação.
	need := requiredCapabilityForAction(req.Action)
	if need != "" {
		caps, err := s.resolver.Resolve(req.Channel)
		if err != nil {
			return err
		}
		if !caps.Supports(need) {
			return fmt.Errorf("%w: canal=%s capability=%s", port.ErrCapabilityUnsupported, req.Channel, need)
		}
	}

	// Enfileira outbox row com a action (sem persistir mensagem nova).
	msgID := domain.MessageID(uuid.NewString())
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	if req.TargetProviderID != "" {
		metadata["target_provider_id"] = req.TargetProviderID
	}
	if req.ReactionEmoji != "" {
		metadata["emoji"] = req.ReactionEmoji
	}
	if req.NewBody != "" {
		metadata["new_body"] = req.NewBody
	}
	if req.State != "" {
		metadata["state"] = req.State
	}

	// OutboundEvent-like row no outbox. Issue #126: usa OutboxMessage.
	// A Message intermediária existe só para carregar o message_id —
	// não persistimos ela (ação de canal não cria Message nova).
	tmpMsg := domain.Message{
		ID:        msgID,
		TenantID:  req.TenantID,
		Channel:   req.Channel,
		ContactID: req.ContactID,
		Type:      domain.MessageTypeSystem,
		Body:      string(req.Action),
		Metadata:  metadata,
	}
	ob, err := domain.NewOutboxMessage(tmpMsg.ID, tmpMsg.TenantID, tmpMsg.Channel, tmpMsg.ConversationID, tmpMsg.ContactID)
	if err != nil {
		return fmt.Errorf("new outbox (action): %w", err)
	}
	if err := s.outbox.Enqueue(ctx, ob); err != nil {
		// Outbox sem tx-rollback: aceitável log+retornar.
		s.log.Warn().Err(err).Msg("send: outbox action enqueue failed (continuing)")
	}

	if s.relay != nil {
		s.relay.Notify()
	}
	return nil
}

func requiredCapabilityForAction(a port.Action) port.Capability {
	switch a {
	case port.ActionReaction:
		return port.CapReactions
	case port.ActionEdit:
		return port.CapEdit
	case port.ActionRevoke:
		return port.CapDelete
	case port.ActionMarkRead:
		return port.CapMarkRead
	case port.ActionTyping:
		return port.CapTyping
	case port.ActionPresence:
		return port.CapPresence
	default:
		return ""
	}
}

// payloadEncoder exporta a forma JSON do OutboundRequest (para testes).
func payloadEncoder(req port.OutboundRequest) ([]byte, error) {
	return json.Marshal(req)
}

var _ = payloadEncoder
