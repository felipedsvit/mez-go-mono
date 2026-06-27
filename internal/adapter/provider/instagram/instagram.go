// Package instagram é o adapter do canal Instagram Direct / Messaging
// (Meta Graph API, oficial e stateless). O inbound vem via /webhooks/meta
// (handler da Fase 2). Cobre outbound + Handover Protocol.
package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Client é o cliente HTTP da Graph API (Instagram Messaging). É injetável nos
// testes via BaseURL/HTTPClient. Não mantém estado entre chamadas (stateless).
// As rotas usam o page-id (a Page "casada" com a conta IG Professional); como
// alternativa a Meta aceita /me/messages com o próprio token.
type Client struct {
	httpClient *http.Client
	baseURL    string // ex.: https://graph.facebook.com
	version    string // ex.: v21.0
	pageID     string // Facebook Page ID conectado à conta IG
	token      string // Page/System User access token (Bearer)
}

// ClientConfig agrupa os parâmetros do cliente.
type ClientConfig struct {
	BaseURL    string
	Version    string
	PageID     string // Vazio = usa "me"
	Token      string
	HTTPClient *http.Client
}

// NewClient cria o cliente da Graph API para o Instagram.
func NewClient(cfg ClientConfig) *Client {
	base := cfg.BaseURL
	if base == "" {
		base = "https://graph.facebook.com"
	}
	version := cfg.Version
	if version == "" {
		version = "v21.0"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	page := cfg.PageID
	if page == "" {
		page = "me"
	}
	return &Client{
		httpClient: hc,
		baseURL:    base,
		version:    version,
		pageID:     page,
		token:      cfg.Token,
	}
}

// sendResponse modela a resposta de POST /{page-id}/messages
// (instagram-direct.md §4.1): {recipient_id, message_id}.
type sendResponse struct {
	RecipientID string      `json:"recipient_id"`
	MessageID   string      `json:"message_id"`
	Error       *graphError `json:"error,omitempty"`
}

// graphError é o envelope de erro padrão da Graph API.
type graphError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

func (e graphError) String() string {
	return fmt.Sprintf("code=%d type=%s msg=%s", e.Code, e.Type, e.Message)
}

// SendMessage faz POST /{page-id}/messages com o body já montado e devolve o
// message_id (mid) da mensagem criada.
func (c *Client) SendMessage(ctx context.Context, body map[string]any) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.version, c.pageID)
	raw, err := c.doJSON(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	var resp sendResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("decodificar resposta: %w", err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("graph api: %s", resp.Error.String())
	}
	if resp.MessageID == "" {
		return "", fmt.Errorf("resposta sem message id")
	}
	return resp.MessageID, nil
}

// UploadOwnedMedia faz POST /{page-id}/owned_media (multipart) e devolve o
// attachment_id reutilizável (instagram-direct.md §6.1).
func (c *Client) UploadOwnedMedia(ctx context.Context, mediaType string, data []byte, filename string) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("message", fmt.Sprintf(`{"attachment":{"type":%q,"payload":{"is_reusable":true}}}`, mediaType)); err != nil {
		return "", fmt.Errorf("montar campo message: %w", err)
	}
	part, err := mw.CreateFormFile("filedata", filename)
	if err != nil {
		return "", fmt.Errorf("montar parte de arquivo: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("escrever mídia: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("fechar multipart: %w", err)
	}

	url := fmt.Sprintf("%s/%s/%s/owned_media", c.baseURL, c.version, c.pageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return "", fmt.Errorf("montar requisição: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("chamar owned_media: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ler resposta: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("owned_media status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		ID    string      `json:"id"`
		Error *graphError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decodificar attachment_id: %w", err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("graph api: %s", out.Error.String())
	}
	if out.ID == "" {
		return "", fmt.Errorf("resposta sem attachment_id")
	}
	return out.ID, nil
}

// doJSON executa uma requisição com corpo JSON (ou sem, se body == nil).
func (c *Client) doJSON(ctx context.Context, method, url string, body map[string]any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("serializar body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("montar requisição: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chamar graph api: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ler resposta: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graph api status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
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
	payload, err := buildMessagePayload(req)
	if err != nil {
		return "", err
	}
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
		payload, err := buildReactionPayload(req)
		if err != nil {
			return "", err
		}
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

func buildMessagePayload(req port.OutboundRequest) (map[string]any, error) {
	msg := map[string]any{}
	switch req.Type {
	case domain.MessageTypeText, "":
		msg["text"] = req.Body
	case domain.MessageTypeImage:
		url, _ := req.Metadata["media_url"].(string)
		msg["attachment"] = map[string]any{
			"type": "image",
			"payload": map[string]any{
				"url":         url,
				"is_reusable": false,
			},
		}
	case domain.MessageTypeVideo:
		url, _ := req.Metadata["media_url"].(string)
		msg["attachment"] = map[string]any{
			"type": "video",
			"payload": map[string]any{"url": url, "is_reusable": false},
		}
	case domain.MessageTypeAudio:
		url, _ := req.Metadata["media_url"].(string)
		msg["attachment"] = map[string]any{
			"type": "audio",
			"payload": map[string]any{"url": url, "is_reusable": false},
		}
	case domain.MessageTypeDocument:
		url, _ := req.Metadata["media_url"].(string)
		msg["attachment"] = map[string]any{
			"type": "file",
			"payload": map[string]any{"url": url, "is_reusable": false},
		}
	case domain.MessageTypeSticker:
		// Sticker URL é uma imagem no formato webp; IG não tem tipo dedicado.
		url, _ := req.Metadata["media_url"].(string)
		msg["attachment"] = map[string]any{
			"type": "image",
			"payload": map[string]any{"url": url, "is_reusable": false},
		}
	case domain.MessageTypeLocation:
		lat, _ := req.Metadata["latitude"].(float64)
		lng, _ := req.Metadata["longitude"].(float64)
		msg["attachment"] = map[string]any{
			"type": "location",
			"payload": map[string]any{
				"latitude":  lat,
				"longitude": lng,
			},
		}
	case domain.MessageTypeTemplate:
		// Templates precisam de payload preparado pelo caller; aceitamos o JSON pronto.
		tpl, ok := req.Metadata["template"]
		if !ok {
			return nil, fmt.Errorf("instagram: template sem metadata[template]")
		}
		msg["attachment"] = map[string]any{
			"type":    "template",
			"payload": tpl,
		}
	default:
		return nil, fmt.Errorf("instagram: type %q não suportado", req.Type)
	}
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message":   msg,
	}, nil
}

func buildReactionPayload(req port.OutboundRequest) (map[string]any, error) {
	if req.TargetProviderID == "" {
		return nil, fmt.Errorf("instagram: reaction sem target_provider_id")
	}
	emoji := req.ReactionEmoji
	if emoji == "" {
		emoji = req.Body
	}
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message": map[string]any{
			"reaction": map[string]any{
				"mid":    req.TargetProviderID,
				"action": "react",
				"emoji":  emoji,
			},
		},
	}, nil
}

// Compile-time assertion: we satisfy port.Sender.
var _ port.Sender = (*Adapter)(nil)
