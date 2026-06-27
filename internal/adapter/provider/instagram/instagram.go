// Package instagram é o adapter do canal Instagram Direct / Messaging
// (Meta Graph API, oficial e stateless). O inbound vem via /webhooks/meta
// (handler da Fase 2). Esta implementação cobre apenas outbound.
package instagram

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Client é o cliente HTTP da Graph API do Instagram.
type Client struct {
	baseURL string
	version string
	pageID  string
	token   string
}

// NewClient cria o client.
func NewClient(baseURL, version, pageID, token string) *Client {
	if baseURL == "" {
		baseURL = "https://graph.facebook.com"
	}
	if version == "" {
		version = "v21.0"
	}
	return &Client{baseURL: baseURL, version: version, pageID: pageID, token: token}
}

// SendMessage envia mensagem e devolve message_id.
func (c *Client) SendMessage(_ context.Context, _ map[string]any) (string, error) {
	// Stub mínimo — implementação real via http.Client.
	return "ig-mid-stub", nil
}

// Adapter implementa port.Sender para Instagram.
type Adapter struct {
	tenant domain.TenantID
	client *Client
	log    zerolog.Logger
}

// New cria o adapter.
func New(tenant domain.TenantID, client *Client, log zerolog.Logger) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelIG)).Str("tenant", string(tenant)).Logger()
	return &Adapter{tenant: tenant, client: client, log: l}
}

// Channel retorna o canal.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelIG }

// Capabilities retorna o set.
func (a *Adapter) Capabilities() port.CapabilitySet { return InstagramCapabilities() }

// Send entrega mensagem ou ação.
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}
	payload := buildMessagePayload(req)
	mid, err := a.client.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("instagram: %w", err)
	}
	a.log.Info().Str("to", req.PeerID).Str("mid", mid).Msg("instagram: sent")
	return mid, nil
}

func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	switch req.Action {
	case port.ActionReaction:
		payload := buildReactionPayload(req)
		return a.client.SendMessage(ctx, payload)
	case port.ActionMarkRead:
		// IG não tem endpoint de read — no-op silencioso.
		return "", nil
	case port.ActionEdit, port.ActionRevoke:
		return "", fmt.Errorf("instagram: %s não suportado pela API", req.Action)
	case port.ActionTyping, port.ActionPresence:
		return "", fmt.Errorf("instagram: %s não suportado", req.Action)
	default:
		return "", fmt.Errorf("instagram: ação desconhecida: %q", req.Action)
	}
}

func buildMessagePayload(req port.OutboundRequest) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message":   map[string]any{"text": req.Body},
	}
}

func buildReactionPayload(req port.OutboundRequest) map[string]any {
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message": map[string]any{
			"reaction": map[string]any{
				"mid":    req.TargetProviderID,
				"action": "react",
				"emoji":  req.ReactionEmoji,
			},
		},
	}
}
