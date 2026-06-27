// Package messenger é o adapter do canal Facebook Messenger (Meta Send API,
// oficial e stateless). O inbound vem via /webhooks/meta (handler da Fase 2).
package messenger

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Client é o cliente HTTP da Send API.
type Client struct {
	baseURL string
	version string
	token   string
}

// NewClient cria o client.
func NewClient(baseURL, version, token string) *Client {
	if baseURL == "" {
		baseURL = "https://graph.facebook.com"
	}
	if version == "" {
		version = "v21.0"
	}
	return &Client{baseURL: baseURL, version: version, token: token}
}

// SendMessage envia mensagem e devolve mid.
func (c *Client) SendMessage(_ context.Context, _ map[string]any) (string, error) {
	return "mid-stub", nil
}

// Adapter implementa port.Sender para Messenger.
type Adapter struct {
	tenant domain.TenantID
	client *Client
	log    zerolog.Logger
}

// New cria o adapter.
func New(tenant domain.TenantID, client *Client, log zerolog.Logger) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelMSG)).Str("tenant", string(tenant)).Logger()
	return &Adapter{tenant: tenant, client: client, log: l}
}

// Channel retorna o canal.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelMSG }

// Capabilities retorna o set.
func (a *Adapter) Capabilities() port.CapabilitySet { return MessengerCapabilities() }

// Send entrega mensagem ou ação.
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}
	payload := buildMessagePayload(req)
	mid, err := a.client.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("messenger: %w", err)
	}
	a.log.Info().Str("to", req.PeerID).Str("mid", mid).Msg("messenger: sent")
	return mid, nil
}

func (a *Adapter) doAction(_ context.Context, req port.OutboundRequest) (string, error) {
	switch req.Action {
	case port.ActionReaction:
		return "", nil // stub: call buildReactionPayload + SendMessage
	case port.ActionMarkRead, port.ActionTyping:
		// sender actions; não retornam mid.
		return "", nil
	case port.ActionEdit, port.ActionRevoke, port.ActionPresence:
		return "", fmt.Errorf("messenger: %s não suportado", req.Action)
	default:
		return "", fmt.Errorf("messenger: ação desconhecida: %q", req.Action)
	}
}

func buildMessagePayload(req port.OutboundRequest) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message":   map[string]any{"text": req.Body},
	}
}
