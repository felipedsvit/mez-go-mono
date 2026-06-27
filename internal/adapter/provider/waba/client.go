// Package waba — client HTTP da Graph API (WhatsApp Cloud).
//
// Implementa POST /{phone-number-id}/messages, GET /{media-id} e
// DELETE /{message-id}. É injetável nos testes via BaseURL/HTTPClient.
// Não mantém estado entre chamadas (canal stateless).
package waba

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client é o cliente HTTP da Graph API (WhatsApp Cloud). É injetável nos testes
// via BaseURL/HTTPClient. Não mantém estado entre chamadas.
type Client struct {
	httpClient    *http.Client
	baseURL       string // ex.: https://graph.facebook.com
	version       string // ex.: v21.0
	phoneNumberID string
	token         string
}

// ClientConfig agrupa os parâmetros do cliente.
type ClientConfig struct {
	// BaseURL permite apontar para um stub nos testes. Vazio = Graph oficial.
	BaseURL       string
	Version       string
	PhoneNumberID string
	Token         string
	// HTTPClient permite injetar um cliente nos testes. Vazio = default c/ timeout.
	HTTPClient *http.Client
}

// NewClient cria o cliente da Graph API.
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
		httpClient:    hc,
		baseURL:       base,
		version:       version,
		phoneNumberID: cfg.PhoneNumberID,
		token:         cfg.Token,
	}
}

// sendResponse modela a resposta de POST /messages (whatsapp-cloud.md §4.14).
type sendResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
	Error *graphError `json:"error,omitempty"`
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

// SendMessage faz POST /{phone-number-id}/messages com o body já montado e
// devolve o wamid da mensagem criada.
func (c *Client) SendMessage(ctx context.Context, body map[string]any) (string, error) {
	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.version, c.phoneNumberID)
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
	if len(resp.Messages) == 0 {
		return "", fmt.Errorf("resposta sem message id")
	}
	return resp.Messages[0].ID, nil
}

// MarkRead marca a mensagem alvo como lida (whatsapp-cloud.md §4.2: status=read).
func (c *Client) MarkRead(ctx context.Context, messageID string) error {
	body := map[string]any{
		"messaging_product": "whatsapp",
		"status":            "read",
		"message_id":        messageID,
	}
	url := fmt.Sprintf("%s/%s/%s/messages", c.baseURL, c.version, c.phoneNumberID)
	_, err := c.doJSON(ctx, http.MethodPost, url, body)
	return err
}

// DeleteMessage revoga uma mensagem enviada (whatsapp-cloud.md §4.10):
// DELETE /{message-id}.
func (c *Client) DeleteMessage(ctx context.Context, messageID string) error {
	url := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, messageID)
	_, err := c.doJSON(ctx, http.MethodDelete, url, nil)
	return err
}

// GetMediaURL faz GET /{media-id} e devolve a URL temporária + mimetype
// (whatsapp-cloud.md §6). A URL exige o mesmo Bearer token p/ download.
func (c *Client) GetMediaURL(ctx context.Context, mediaID string) (string, string, error) {
	u := fmt.Sprintf("%s/%s/%s", c.baseURL, c.version, mediaID)
	raw, err := c.doJSON(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", err
	}
	var resp struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", "", fmt.Errorf("decodificar url da mídia: %w", err)
	}
	if resp.URL == "" {
		return "", "", fmt.Errorf("resposta sem url de mídia")
	}
	return resp.URL, resp.MimeType, nil
}

// DownloadMedia baixa os bytes da URL temporária (autenticada com o Bearer).
func (c *Client) DownloadMedia(ctx context.Context, mediaURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("montar requisição de mídia: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("baixar mídia: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("baixar mídia: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// doJSON executa uma requisição com corpo JSON (ou sem, se body == nil),
// autenticada com o Bearer token, e devolve o corpo da resposta.
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
