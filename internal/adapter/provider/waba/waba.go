// Package waba é o adapter do canal WhatsApp Business Cloud API (oficial e
// stateless). Diferente do whatsmeow/tgbot, a entrada (inbound) NÃO vem do
// adapter: a Meta faz push HTTP para /webhooks/meta, que normaliza e chama o
// Ingestor diretamente. O adapter cuida só da saída, traduzindo o pedido
// canônico (port.OutboundRequest) para a Graph API.
//
// Connect/Disconnect são no-op (não há sessão persistente).
//
// Fonte de verdade das capacidades e payloads: docs/canais/whatsapp-cloud.md.
package waba

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Adapter implementa port.Sender para o WhatsApp Cloud API.
type Adapter struct {
	tenant domain.TenantID
	client *Client
	logger zerolog.Logger
}

// New cria o adapter a partir do cliente da Graph API.
func New(tenant domain.TenantID, client *Client, logger zerolog.Logger) *Adapter {
	l := logger.With().Str("channel", string(domain.ChannelWABA)).Str("tenant", string(tenant)).Logger()
	return &Adapter{tenant: tenant, client: client, logger: l}
}

// Channel retorna o canal lógico.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelWABA }

// Capabilities declara o que o WhatsApp Cloud API suporta.
func (a *Adapter) Capabilities() port.CapabilitySet { return WABACapabilities() }

// Send entrega uma mensagem ou executa uma ação de canal, despachando por
// req.Action / req.Type (espelha os adapters whatsmeow e tgbot).
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
		return "", fmt.Errorf("enviar mensagem waba: %w", err)
	}
	a.logger.Info().
		Str("to", req.PeerID).
		Str("type", string(req.Type)).
		Str("wamid", wamid).
		Msg("mensagem enviada")
	return wamid, nil
}

// doAction executa ações que não são mensagens novas: reação, revogação e
// mark-read. Edit e presença/typing não são suportados pela Cloud API.
func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	switch req.Action {
	case port.ActionReaction:
		payload, err := buildReactionPayload(req)
		if err != nil {
			return "", err
		}
		wamid, err := a.client.SendMessage(ctx, payload)
		if err != nil {
			return "", fmt.Errorf("reação waba: %w", err)
		}
		a.logger.Info().Str("to", req.PeerID).Str("action", string(req.Action)).Msg("ação executada")
		return wamid, nil
	case port.ActionRevoke:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("revoke sem target_provider_id")
		}
		if err := a.client.DeleteMessage(ctx, req.TargetProviderID); err != nil {
			return "", fmt.Errorf("revoke waba: %w", err)
		}
	case port.ActionMarkRead:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("mark_read sem target_provider_id")
		}
		if err := a.client.MarkRead(ctx, req.TargetProviderID); err != nil {
			return "", fmt.Errorf("mark_read waba: %w", err)
		}
	case port.ActionEdit:
		return "", fmt.Errorf("waba: edit não suportado")
	case port.ActionTyping, port.ActionPresence:
		return "", fmt.Errorf("waba: %s não suportado", req.Action)
	default:
		return "", fmt.Errorf("waba: ação desconhecida: %q", req.Action)
	}
	a.logger.Info().Str("to", req.PeerID).Str("action", string(req.Action)).Msg("ação executada")
	return "", nil
}

// --- Saída: pedido canônico → payload da Graph API ---

// buildMessagePayload monta o corpo de POST /messages a partir do pedido
// canônico (whatsapp-cloud.md §4). Mídia é enviada por link (Metadata["media_url"])
// ou por id já carregado (Metadata["media_id"]).
func buildMessagePayload(req port.OutboundRequest) (map[string]any, error) {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                req.PeerID,
	}

	switch req.Type {
	case domain.MessageTypeText, "":
		body["type"] = "text"
		body["text"] = map[string]any{"body": req.Body, "preview_url": true}
	case domain.MessageTypeImage:
		body["type"] = "image"
		body["image"] = mediaObject(req, true)
	case domain.MessageTypeVideo:
		body["type"] = "video"
		body["video"] = mediaObject(req, true)
	case domain.MessageTypeAudio:
		body["type"] = "audio"
		body["audio"] = mediaObject(req, false)
	case domain.MessageTypeDocument:
		body["type"] = "document"
		doc := mediaObject(req, true)
		if fn, ok := req.Metadata["filename"].(string); ok && fn != "" {
			doc["filename"] = fn
		}
		body["document"] = doc
	case domain.MessageTypeSticker:
		body["type"] = "sticker"
		body["sticker"] = mediaObject(req, false)
	case domain.MessageTypeLocation:
		body["type"] = "location"
		body["location"] = locationObject(req.Metadata)
	case domain.MessageType("contact"):
		c, ok := req.Metadata["contacts"]
		if !ok {
			return nil, fmt.Errorf("contact sem metadata[contacts]")
		}
		body["type"] = "contacts"
		body["contacts"] = c
	case domain.MessageTypeTemplate:
		tpl, ok := req.Metadata["template"]
		if !ok {
			return nil, fmt.Errorf("template sem metadata[template]")
		}
		body["type"] = "template"
		body["template"] = tpl
	default:
		return nil, fmt.Errorf("tipo não suportado pelo waba: %q", req.Type)
	}
	return body, nil
}

// mediaObject monta o objeto de mídia (link ou id). Caption só quando withCaption.
func mediaObject(req port.OutboundRequest, withCaption bool) map[string]any {
	obj := map[string]any{}
	if url, ok := req.Metadata["media_url"].(string); ok && url != "" {
		obj["link"] = url
	} else if id, ok := req.Metadata["media_id"].(string); ok && id != "" {
		obj["id"] = id
	}
	if withCaption && req.Body != "" && !strings.HasPrefix(string(req.Type), "sticker") {
		obj["caption"] = req.Body
	}
	return obj
}

func locationObject(metadata map[string]any) map[string]any {
	obj := map[string]any{}
	for _, k := range []string{"latitude", "longitude", "name", "address"} {
		if v, ok := metadata[k]; ok {
			obj[k] = v
		}
	}
	return obj
}

// buildReactionPayload monta o corpo de uma reação (whatsapp-cloud.md §4.7).
func buildReactionPayload(req port.OutboundRequest) (map[string]any, error) {
	if req.TargetProviderID == "" {
		return nil, fmt.Errorf("reação sem target_provider_id")
	}
	emoji := req.ReactionEmoji
	if emoji == "" {
		emoji = req.Body
	}
	if emoji == "" {
		return nil, fmt.Errorf("reação sem emoji")
	}
	return map[string]any{
		"messaging_product": "whatsapp",
		"recipient_type":    "individual",
		"to":                req.PeerID,
		"type":              "reaction",
		"reaction":          map[string]any{"message_id": req.TargetProviderID, "emoji": emoji},
	}, nil
}
