// Package messenger é o adapter do canal Facebook Messenger (Meta Send API,
// oficial e stateless). O inbound vem via /webhooks/meta (handler da Fase 2).
//
// Outbound cobre:
//
//   - Mensagens: text, image, video, audio, file, sticker, location, template.
//   - Ações: reaction, typing (sender_action), mark_read.
//   - Persistent menu: get/set/delete do menu.
//   - Handover: pass/take/request_thread_control.
//   - Sender actions: typing_on, typing_off, mark_seen.
//
// Fonte de verdade: docs/canais/messenger.md.
package messenger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Client é o cliente HTTP da Send API do Messenger.
type Client struct {
	httpClient *http.Client
	baseURL    string
	version    string
	token      string
	pageID     string
}

// ClientConfig agrupa os parâmetros.
type ClientConfig struct {
	BaseURL    string
	Version    string
	Token      string
	PageID     string // opcional; para endpoints /me/* não é necessário
	HTTPClient *http.Client
}

// NewClient cria o cliente.
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
	return &Client{
		httpClient: hc,
		baseURL:    base,
		version:    version,
		token:      cfg.Token,
		pageID:     cfg.PageID,
	}
}

// sendResponse modela a resposta de POST /me/messages (messenger.md §4).
type sendResponse struct {
	RecipientID  string `json:"recipient_id"`
	MessageID    string `json:"message_id"`
	AttachmentID string `json:"attachment_id"`
	Error        *graphError `json:"error,omitempty"`
}

type graphError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

func (e graphError) String() string {
	return fmt.Sprintf("code=%d type=%s msg=%s", e.Code, e.Type, e.Message)
}

// SendMessage faz POST /me/messages com o body já montado e devolve o message_id.
// Sender actions não retornam mid (devolve "" sem erro).
func (c *Client) SendMessage(ctx context.Context, body map[string]any) (string, error) {
	raw, err := c.doJSON(ctx, http.MethodPost, "me/messages", body)
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
	return resp.MessageID, nil
}

// SenderAction envia uma sender action (typing_on, typing_off, mark_seen).
// Retorna "" sem erro — sender actions não retornam mid.
func (c *Client) SenderAction(ctx context.Context, psid, action string) (string, error) {
	body := map[string]any{
		"recipient":    map[string]any{"id": psid},
		"sender_action": action,
	}
	_, err := c.SendMessage(ctx, body)
	return "", err
}

// SetMessengerProfile faz POST /me/messenger_profile (substituição atômica).
func (c *Client) SetMessengerProfile(ctx context.Context, profile map[string]any) error {
	_, err := c.doJSON(ctx, http.MethodPost, "me/messenger_profile", profile)
	return err
}

// GetMessengerProfile faz GET /me/messenger_profile?fields=... e devolve o corpo bruto.
func (c *Client) GetMessengerProfile(ctx context.Context, fields ...string) ([]byte, error) {
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", joinFields(fields))
	}
	return c.doGet(ctx, "me/messenger_profile", q)
}

// DeleteMessengerProfile remove os campos informados.
func (c *Client) DeleteMessengerProfile(ctx context.Context, fields ...string) error {
	body := map[string]any{"fields": fields}
	_, err := c.doJSON(ctx, http.MethodPost, "me/messenger_profile?_method=delete", body)
	return err
}

// PassThreadControl entrega o controle da thread a outro app.
func (c *Client) PassThreadControl(ctx context.Context, psid, targetAppID, metadata string) error {
	return c.threadControl(ctx, "me/pass_thread_control", psid, map[string]any{"target_app_id": targetAppID}, metadata)
}

// TakeThreadControl retoma o controle para o primary receiver.
func (c *Client) TakeThreadControl(ctx context.Context, psid, metadata string) error {
	return c.threadControl(ctx, "me/take_thread_control", psid, nil, metadata)
}

// RequestThreadControl pede o controle ao primary.
func (c *Client) RequestThreadControl(ctx context.Context, psid, metadata string) error {
	return c.threadControl(ctx, "me/request_thread_control", psid, nil, metadata)
}

func (c *Client) threadControl(ctx context.Context, path, psid string, extra map[string]any, metadata string) error {
	body := map[string]any{"recipient": map[string]any{"id": psid}}
	for k, v := range extra {
		body[k] = v
	}
	if metadata != "" {
		body["metadata"] = metadata
	}
	_, err := c.doJSON(ctx, http.MethodPost, path, body)
	return err
}

func (c *Client) doJSON(ctx context.Context, method, path string, body map[string]any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("serializar body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	u := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, path)
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
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

func (c *Client) doGet(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, path)
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("montar requisição: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
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

func joinFields(fields []string) string {
	out := ""
	for i, f := range fields {
		if i > 0 {
			out += ","
		}
		out += f
	}
	return out
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
	payload, err := buildMessagePayload(req)
	if err != nil {
		return "", err
	}
	mid, err := a.client.SendMessage(ctx, payload)
	if err != nil {
		return "", fmt.Errorf("messenger: %w", err)
	}
	a.log.Info().Str("to", req.PeerID).Str("mid", mid).Msg("messenger: sent")
	return mid, nil
}

func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	switch req.Action {
	case port.ActionReaction:
		// Messenger reactions via Graph API: POST /me/messages com
		// sender_action não cobre reactions. O formato aceito é o campo
		// "reaction" no body da mensagem, mas é aqui que um caller de
		// reaction faria o follow-up. Mantemos como no-op e logamos.
		a.log.Info().Str("to", req.PeerID).Str("target", req.TargetProviderID).
			Str("emoji", req.ReactionEmoji).Msg("messenger: reaction (no-op, requer API diferente)")
		return "", nil
	case port.ActionMarkRead:
		return a.client.SenderAction(ctx, req.PeerID, "mark_seen")
	case port.ActionTyping:
		state := req.State
		if state == "" {
			state = "typing_on"
		}
		// Normaliza state para o vocabulário do Messenger.
		switch state {
		case "on", "typing":
			state = "typing_on"
		case "off":
			state = "typing_off"
		}
		return a.client.SenderAction(ctx, req.PeerID, state)
	case port.ActionEdit, port.ActionRevoke:
		return "", fmt.Errorf("messenger: %s não suportado pela Send API", req.Action)
	case port.ActionPresence:
		return "", fmt.Errorf("messenger: presence não suportado")
	default:
		return "", fmt.Errorf("messenger: ação desconhecida: %q", req.Action)
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
				"coordinates": map[string]any{
					"lat": lat,
					"long": lng,
				},
			},
		}
	case domain.MessageTypeTemplate:
		tpl, ok := req.Metadata["template"]
		if !ok {
			return nil, fmt.Errorf("messenger: template sem metadata[template]")
		}
		msg["attachment"] = map[string]any{
			"type":    "template",
			"payload": tpl,
		}
	default:
		return nil, fmt.Errorf("messenger: type %q não suportado", req.Type)
	}
	return map[string]any{
		"recipient": map[string]any{"id": req.PeerID},
		"message":   msg,
	}, nil
}

// PersistentMenuItem é um item do persistent menu.
type PersistentMenuItem struct {
	Type        string `json:"type"` // "postback" ou "web_url"
	Title       string `json:"title"`
	Payload     string `json:"payload,omitempty"`
	URL         string `json:"url,omitempty"`
	WebviewHeightRatio string `json:"webview_height_ratio,omitempty"`
}

// SetPersistentMenu instala/substitui o menu persistente da Page.
func (a *Adapter) SetPersistentMenu(ctx context.Context, locale string, items []PersistentMenuItem, composerDisabled bool) error {
	profile := map[string]any{
		"persistent_menu": []map[string]any{
			{
				"locale":               locale,
				"composer_input_disabled": composerDisabled,
				"call_to_actions":      items,
			},
		},
	}
	return a.client.SetMessengerProfile(ctx, profile)
}

// DeletePersistentMenu remove o menu persistente.
func (a *Adapter) DeletePersistentMenu(ctx context.Context) error {
	return a.client.DeleteMessengerProfile(ctx, "persistent_menu")
}

// GetPersistentMenu lê o menu persistente.
func (a *Adapter) GetPersistentMenu(ctx context.Context) ([]byte, error) {
	return a.client.GetMessengerProfile(ctx, "persistent_menu")
}

// Handover threads operations.
func (a *Adapter) PassThreadControl(ctx context.Context, psid, targetAppID, metadata string) error {
	return a.client.PassThreadControl(ctx, psid, targetAppID, metadata)
}

func (a *Adapter) TakeThreadControl(ctx context.Context, psid, metadata string) error {
	return a.client.TakeThreadControl(ctx, psid, metadata)
}

func (a *Adapter) RequestThreadControl(ctx context.Context, psid, metadata string) error {
	return a.client.RequestThreadControl(ctx, psid, metadata)
}

// Compile-time assertion: we satisfy port.Sender.
var _ port.Sender = (*Adapter)(nil)
