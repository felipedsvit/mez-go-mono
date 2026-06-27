// Package secrets implementa resolvers simples de credenciais para
// webhooks da Fase 2. A solução é env-based (não-DB) porque:
//   - Fase 2 é fundação; o painel admin para configurar credenciais chega
//     na Fase 5.
//   - Em dev, MEZ_META_APP_SECRETS e MEZ_TELEGRAM_SECRETS permitem testar
//     sem provisionar nada no DB.
//   - Em produção, valores podem vir de Vault/SSM via sidecar.
//
// O resolver real (DB-backed com envelope encryption) é wired na Fase 5.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// ErrNotConfigured é retornado quando o secret não está configurado.
var ErrNotConfigured = errors.New("secret not configured")

// EnvMetaSecrets resolve app secrets da Meta a partir da env var
// MEZ_META_APP_SECRETS. Formato JSON:
//
//	[{"app_id":"123","tenant_id":"<uuid>","channel":"waba","secret":"..."}]
type EnvMetaSecrets struct {
	mu      sync.RWMutex
	loaded  bool
	entries []metaEntry
}

type metaEntry struct {
	AppID    string `json:"app_id"`
	TenantID string `json:"tenant_id"`
	Channel  string `json:"channel"`
	Secret   string `json:"secret"`
}

// NewEnvMetaSecrets carrega os secrets da env.
func NewEnvMetaSecrets() (*EnvMetaSecrets, error) {
	raw := os.Getenv("MEZ_META_APP_SECRETS")
	if raw == "" {
		return &EnvMetaSecrets{}, nil
	}
	var entries []metaEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse MEZ_META_APP_SECRETS: %w", err)
	}
	return &EnvMetaSecrets{entries: entries, loaded: true}, nil
}

// ResolveMetaSecret retorna o secret para (tenant, channel, app_id).
func (e *EnvMetaSecrets) ResolveMetaSecret(_ context.Context, tenantID domain.TenantID, channel domain.Channel, appID string) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, m := range e.entries {
		if m.AppID == appID && m.TenantID == string(tenantID) && m.Channel == string(channel) {
			return []byte(m.Secret), nil
		}
	}
	return nil, fmt.Errorf("%w: app_id=%s tenant=%s channel=%s", ErrNotConfigured, appID, tenantID, channel)
}

// ResolveChannel infere o canal a partir do app_id. Retorna o primeiro
// match para o app_id (se houver mais de um tenant para o mesmo app_id,
// retorna o primeiro).
func (e *EnvMetaSecrets) ResolveChannel(appID string) (domain.Channel, domain.TenantID, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, m := range e.entries {
		if m.AppID == appID {
			return domain.Channel(m.Channel), domain.TenantID(m.TenantID), nil
		}
	}
	return "", "", fmt.Errorf("%w: app_id=%s", ErrNotConfigured, appID)
}

// EnvTelegramSecrets resolve secrets do Telegram a partir da env var
// MEZ_TELEGRAM_SECRETS. Formato:
//
//	tenant_id_1=secret_1
//	tenant_id_2=secret_2
type EnvTelegramSecrets struct {
	mu      sync.RWMutex
	entries map[string]string
}

// NewEnvTelegramSecrets carrega os secrets da env.
func NewEnvTelegramSecrets() *EnvTelegramSecrets {
	raw := os.Getenv("MEZ_TELEGRAM_SECRETS")
	entries := make(map[string]string)
	if raw != "" {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			entries[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return &EnvTelegramSecrets{entries: entries}
}

// ResolveTelegramSecret retorna o secret para o tenant.
func (e *EnvTelegramSecrets) ResolveTelegramSecret(_ context.Context, tenantID domain.TenantID) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.entries[string(tenantID)]
	if !ok {
		return "", fmt.Errorf("%w: tenant=%s", ErrNotConfigured, tenantID)
	}
	return s, nil
}
