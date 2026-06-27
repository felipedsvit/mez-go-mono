// Package memory implementa o SenderRegistry in-memory default para o
// mez-go-mono. Issue #121: este é o único local onde o tipo concreto vive
// — o port mantém apenas a interface SenderRegistry.
//
// Thread-safe. Cache com TTL por (tenant, channel). Factories lazy.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// ErrSenderNotRegistered é um alias para port.ErrSenderNotRegistered,
// exposto aqui para que callers que já importam memory não precisem
// importar port. Issue #121.
var ErrSenderNotRegistered = port.ErrSenderNotRegistered

// cacheEntry guarda um Sender + tempo de criação. TTL eviction em Get.
type cacheEntry struct {
	sender     port.Sender
	tenantID   domain.TenantID
	expiration time.Time
}

// cacheKey identifica um Sender cached por (tenant, channel).
type cacheKey struct {
	tenant  domain.TenantID
	channel domain.Channel
}

// Registry é a implementação default. Thread-safe.
//
// O nome do tipo é Registry (não SenderRegistry) para evitar colisão
// com a interface port.SenderRegistry.
//
// Movida de port.MemorySenderRegistry em #121: capability matrix é
// responsabilidade do port, mas a implementação concreta (mutex, cache,
// logger) é do adapter.
type Registry struct {
	mu        sync.RWMutex
	factories map[domain.Channel]port.SenderFactory
	cache     map[cacheKey]*cacheEntry
	ttl       time.Duration
	log       zerolog.Logger
	now       func() time.Time
}

// New cria a registry com TTL (default 5min) e clock injetável (testes
// usam clock fixo).
func New(log zerolog.Logger, ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Registry{
		factories: make(map[domain.Channel]port.SenderFactory),
		cache:     make(map[cacheKey]*cacheEntry),
		ttl:       ttl,
		log:       log,
		now:       time.Now,
	}
}

// Compile-time check: Registry satisfaz port.SenderRegistry.
var _ port.SenderRegistry = (*Registry)(nil)

// Register associa uma factory a um channel.
func (r *Registry) Register(channel domain.Channel, factory port.SenderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[channel] = factory
}

// Get retorna o Sender para (tenant, channel). Cria on-demand na primeira
// chamada; cachea por TTL. Retorna ErrSenderNotRegistered se o channel
// não tem factory.
func (r *Registry) Get(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) (port.Sender, error) {
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
func (r *Registry) Channels() []domain.Channel {
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
func (r *Registry) Health(ctx context.Context, tenantID domain.TenantID) map[domain.Channel]error {
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
