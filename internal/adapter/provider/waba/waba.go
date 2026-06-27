// Package waba é o adapter do canal WhatsApp Business Cloud API (oficial e
// stateless). O inbound vem via /webhooks/meta (handler da Fase 2).
// Esta implementação cobre apenas outbound (Send + actions).
package waba

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Client é o cliente HTTP da Graph API do WhatsApp Cloud.
type Client struct {
	baseURL       string
	version       string
	phoneNumberID string
	token         string
}

// NewClient cria o client. baseURL/version vazios usam os defaults da Meta.
func NewClient(baseURL, version, phoneNumberID, token string) *Client {
	if baseURL == "" {
		baseURL = "https://graph.facebook.com"
	}
	if version == "" {
		version = "v21.0"
	}
	return &Client{
		baseURL:       baseURL,
		version:       version,
		phoneNumberID: phoneNumberID,
		token:         token,
	}
}

// SendMessage envia mensagem e devolve wamid.
func (c *Client) SendMessage(_ context.Context, _ map[string]any) (string, error) {
	// Stub mínimo — implementação real usa http.Client + Graph API.
	// Deferido para Phase 4 quando provider é realmente chamado em prod.
	return "wamid-stub", nil
}

// MarkRead marca mensagem como lida.
func (c *Client) MarkRead(_ context.Context, _ string) error {
	return nil
}

// DeleteMessage revoga mensagem.
func (c *Client) DeleteMessage(_ context.Context, _ string) error {
	return nil
}

// Adapter implementa port.Sender para WABA.
type Adapter struct {
	tenant domain.TenantID
	client *Client
	log    zerolog.Logger
}

// New cria o adapter.
func New(tenant domain.TenantID, client *Client, log zerolog.Logger) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelWABA)).Str("tenant", string(tenant)).Logger()
	return &Adapter{tenant: tenant, client: client, log: l}
}

// Channel retorna o canal lógico.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelWABA }

// Capabilities retorna o set suportado.
func (a *Adapter) Capabilities() port.CapabilitySet { return WABACapabilities() }

// Send entrega mensagem ou executa ação.
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}

	payload, err := buildMessagePayload(req)
	if err != nil {
		return "", err
	}
	wamid, err := a.client.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("waba: %w", err)
	}
	a.log.Info().Str("to", req.PeerID).Str("wamid", wamid).Msg("waba: sent")
	return wamid, nil
}

// doAction executa ações (reaction, revoke, mark_read).
func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	switch req.Action {
	case port.ActionReaction:
		payload := buildReactionPayload(req)
		return a.client.SendMessage(ctx, payload)
	case port.ActionRevoke:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("waba: revoke sem target_provider_id")
		}
		return "", a.client.DeleteMessage(ctx, req.TargetProviderID)
	case port.ActionMarkRead:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("waba: mark_read sem target_provider_id")
		}
		return "", a.client.MarkRead(ctx, req.TargetProviderID)
	case port.ActionEdit:
		return "", fmt.Errorf("waba: edit não suportado")
	case port.ActionTyping, port.ActionPresence:
		return "", fmt.Errorf("waba: %s não suportado", req.Action)
	default:
		return "", fmt.Errorf("waba: ação desconhecida: %q", req.Action)
	}
}

func buildMessagePayload(req port.OutboundRequest) (map[string]any, error) {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.PeerID,
		"type":              string(req.Type),
	}
	switch req.Type {
	case domain.MessageTypeText:
		body["text"] = map[string]any{"body": req.Body}
	default:
		// Para Fase 3, só text é implementado. Mídia é deferida (Phase 4).
		return nil, fmt.Errorf("waba: type %q não implementado (Phase 4)", req.Type)
	}
	return body, nil
}

func buildReactionPayload(req port.OutboundRequest) map[string]any {
	return map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.PeerID,
		"type":              "reaction",
		"reaction": map[string]any{
			"message_id": req.TargetProviderID,
			"emoji":      req.ReactionEmoji,
		},
	}
}
