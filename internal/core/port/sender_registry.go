// Package port — sender_registry.go: implementação in-memory do
// SenderRegistry (Fase 3 #52). Cache com TTL; lazy init por (tenant, channel).
package port

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// ErrSenderNotRegistered é retornado quando o channel não tem factory.
var ErrSenderNotRegistered = errors.New("sender não registrado para o canal")

// cacheEntry guarda um Sender + tempo de criação. TTL eviction em Get.
type cacheEntry struct {
	sender     Sender
	tenantID   domain.TenantID
	expiration time.Time
}

// MemorySenderRegistry é a implementação default. Thread-safe.
type MemorySenderRegistry struct {
	mu        sync.RWMutex
	factories map[domain.Channel]SenderFactory
	cache     map[cacheKey]*cacheEntry
	ttl       time.Duration
	log       zerolog.Logger
	now       func() time.Time
}

type cacheKey struct {
	tenant  domain.TenantID
	channel domain.Channel
}

// NewMemorySenderRegistry cria a registry com TTL (default 5min) e clock
// injetável (testes usam clock fixo).
func NewMemorySenderRegistry(log zerolog.Logger, ttl time.Duration) *MemorySenderRegistry {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &MemorySenderRegistry{
		factories: make(map[domain.Channel]SenderFactory),
		cache:     make(map[cacheKey]*cacheEntry),
		ttl:       ttl,
		log:       log,
		now:       time.Now,
	}
}

// Register associa uma factory a um channel.
func (r *MemorySenderRegistry) Register(channel domain.Channel, factory SenderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[channel] = factory
}

// Get retorna o Sender para (tenant, channel). Cria on-demand na primeira
// chamada; cachea por TTL. Retorna ErrSenderNotRegistered se o channel
// não tem factory.
func (r *MemorySenderRegistry) Get(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) (Sender, error) {
	key := cacheKey{tenant: tenantID, channel: channel}
	now := r.now()

	r.mu.RLock()
	if e, ok := r.cache[key]; ok && e.expiration.After(now) {
		r.mu.RUnlock()
		return e.sender, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if e, ok := r.cache[key]; ok && e.expiration.After(now) {
		return e.sender, nil
	}

	factory, ok := r.factories[channel]
	if !ok {
		return nil, ErrSenderNotRegistered
	}

	sender, err := factory(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if sender == nil {
		return nil, ErrSenderNotRegistered
	}

	r.cache[key] = &cacheEntry{
		sender:     sender,
		tenantID:   tenantID,
		expiration: now.Add(r.ttl),
	}
	return sender, nil
}

// Channels retorna os channels com factory registrada.
func (r *MemorySenderRegistry) Channels() []domain.Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Channel, 0, len(r.factories))
	for ch := range r.factories {
		out = append(out, ch)
	}
	return out
}

// Health verifica se cada canal registrado pode ser instanciado para o tenant.
// Para canais stateless (WABA/IG/MSG), "instanciar" = "factory não falhou";
// para canais com sessão (Telegram bot), Connect é chamado (best-effort).
//
// Não envia mensagem real — só testa a factory.
func (r *MemorySenderRegistry) Health(ctx context.Context, tenantID domain.TenantID) map[domain.Channel]error {
	r.mu.RLock()
	channels := make([]domain.Channel, 0, len(r.factories))
	for ch := range r.factories {
		channels = append(channels, ch)
	}
	r.mu.RUnlock()

	out := make(map[domain.Channel]error, len(channels))
	for _, ch := range channels {
		_, err := r.Get(ctx, tenantID, ch)
		out[ch] = err
	}
	return out
}
