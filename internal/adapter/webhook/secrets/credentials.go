// Package secrets — credentials.go: env-based credentials resolver para
// canais outbound (Fase 3 #50).
//
// Lê credenciais de variáveis de ambiente em formato JSON. Cada canal tem
// sua estrutura. DB-backed com envelope encryption é carryover (Fase 7).
//
// Variáveis suportadas:
//
//	MEZ_WABA_CREDENTIALS     = '[{"tenant_id":"...","phone_number_id":"...","access_token":"..."}]'
//	MEZ_INSTAGRAM_CREDENTIALS = '[{"tenant_id":"...","page_id":"...","access_token":"..."}]'
//	MEZ_MESSENGER_CREDENTIALS = '[{"tenant_id":"...","page_id":"...","access_token":"..."}]'
//	MEZ_TELEGRAM_CREDENTIALS  = '[{"tenant_id":"...","bot_token":"..."}]'
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// ErrCredentialsNotFound indica que o (tenant, channel) não tem credenciais.
var ErrCredentialsNotFound = errors.New("credenciais não configuradas")

// Credentials é a forma unificada de credencial por canal.
type Credentials struct {
	TenantID      domain.TenantID `json:"tenant_id"`
	Channel       domain.Channel  `json:"channel"`
	PhoneNumberID string          `json:"phone_number_id,omitempty"`
	PageID        string          `json:"page_id,omitempty"`
	BotToken      string          `json:"bot_token,omitempty"`
	AccessToken   string          `json:"access_token,omitempty"`
}

// EnvCredentials resolve credenciais a partir de env vars JSON.
//
// Não mantém estado mutável — thread-safe por construção.
type EnvCredentials struct {
	mu sync.RWMutex

	waba      []wabaEntry
	instagram []metaPageEntry
	messenger []metaPageEntry
	telegram  []telegramEntry
}

type wabaEntry struct {
	TenantID      string `json:"tenant_id"`
	PhoneNumberID string `json:"phone_number_id"`
	AccessToken   string `json:"access_token"`
}

type metaPageEntry struct {
	TenantID    string `json:"tenant_id"`
	PageID      string `json:"page_id"`
	AccessToken string `json:"access_token"`
}

type telegramEntry struct {
	TenantID string `json:"tenant_id"`
	BotToken string `json:"bot_token"`
}

// NewEnvCredentials carrega todas as env vars. Erro só se uma env var
// estiver presente mas com JSON inválido (fail-fast).
func NewEnvCredentials() (*EnvCredentials, error) {
	c := &EnvCredentials{}

	if raw := os.Getenv("MEZ_WABA_CREDENTIALS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.waba); err != nil {
			return nil, fmt.Errorf("parse MEZ_WABA_CREDENTIALS: %w", err)
		}
	}
	if raw := os.Getenv("MEZ_INSTAGRAM_CREDENTIALS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.instagram); err != nil {
			return nil, fmt.Errorf("parse MEZ_INSTAGRAM_CREDENTIALS: %w", err)
		}
	}
	if raw := os.Getenv("MEZ_MESSENGER_CREDENTIALS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.messenger); err != nil {
			return nil, fmt.Errorf("parse MEZ_MESSENGER_CREDENTIALS: %w", err)
		}
	}
	if raw := os.Getenv("MEZ_TELEGRAM_CREDENTIALS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.telegram); err != nil {
			return nil, fmt.Errorf("parse MEZ_TELEGRAM_CREDENTIALS: %w", err)
		}
	}
	return c, nil
}

// ResolveWABA retorna phone_number_id + access_token para o tenant.
func (c *EnvCredentials) ResolveWABA(tenantID domain.TenantID) (Credentials, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.waba {
		if e.TenantID == string(tenantID) {
			return Credentials{
				TenantID:      tenantID,
				Channel:       domain.ChannelWABA,
				PhoneNumberID: e.PhoneNumberID,
				AccessToken:   e.AccessToken,
			}, nil
		}
	}
	return Credentials{}, fmt.Errorf("%w: waba tenant=%s", ErrCredentialsNotFound, tenantID)
}

// ResolveInstagram retorna page_id + access_token para o tenant.
func (c *EnvCredentials) ResolveInstagram(tenantID domain.TenantID) (Credentials, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.instagram {
		if e.TenantID == string(tenantID) {
			return Credentials{
				TenantID:    tenantID,
				Channel:     domain.ChannelIG,
				PageID:      e.PageID,
				AccessToken: e.AccessToken,
			}, nil
		}
	}
	return Credentials{}, fmt.Errorf("%w: instagram tenant=%s", ErrCredentialsNotFound, tenantID)
}

// ResolveMessenger retorna page_id + access_token para o tenant.
func (c *EnvCredentials) ResolveMessenger(tenantID domain.TenantID) (Credentials, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.messenger {
		if e.TenantID == string(tenantID) {
			return Credentials{
				TenantID:    tenantID,
				Channel:     domain.ChannelMSG,
				PageID:      e.PageID,
				AccessToken: e.AccessToken,
			}, nil
		}
	}
	return Credentials{}, fmt.Errorf("%w: messenger tenant=%s", ErrCredentialsNotFound, tenantID)
}

// ResolveTelegram retorna bot_token para o tenant.
func (c *EnvCredentials) ResolveTelegram(tenantID domain.TenantID) (Credentials, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, e := range c.telegram {
		if e.TenantID == string(tenantID) {
			return Credentials{
				TenantID: tenantID,
				Channel:  domain.ChannelTGBot,
				BotToken: e.BotToken,
			}, nil
		}
	}
	return Credentials{}, fmt.Errorf("%w: telegram tenant=%s", ErrCredentialsNotFound, tenantID)
}

// ResolveByChannel despacha por canal.
func (c *EnvCredentials) ResolveByChannel(tenantID domain.TenantID, channel domain.Channel) (Credentials, error) {
	switch channel {
	case domain.ChannelWABA:
		return c.ResolveWABA(tenantID)
	case domain.ChannelIG:
		return c.ResolveInstagram(tenantID)
	case domain.ChannelMSG:
		return c.ResolveMessenger(tenantID)
	case domain.ChannelTGBot:
		return c.ResolveTelegram(tenantID)
	default:
		return Credentials{}, fmt.Errorf("canal sem credencial: %s", channel)
	}
}
