// Package breaker — circuit breaker per (tenant, channel) in-memory.
//
// Issue #160 (Sprint 1 A2 audit): circuit breaker wrap do Sender.Send
// para isolar falhas de um canal de um tenant sem derrubar outros.
// Estado in-memory (sem Redis/NATS, alinhado com AGENTS.md §1.1).
//
// Trade-off aceito (ADR-0022): estado não é compartilhado entre
// instâncias. Em scale-out (Fase 10+), breaker seria per-pod e
// failures afetariam o pod todo. Aceitável porque falhas downstream
// (WABA rate limit, IG outage) já impactam todos os pods via
// upstream rate limit do provider.
package breaker

import (
	"fmt"
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
)

// Registry mantém circuit breakers por (tenant, channel). Thread-safe.
type Registry struct {
	mu       sync.RWMutex
	breakers map[cacheKey]*gobreaker.CircuitBreaker[any]
	config   Config
}

// cacheKey identifica unicamente um breaker.
type cacheKey struct {
	tenantID string
	channel  string
}

// Config configura breakers no Registry.
type Config struct {
	// MaxRequests no half-open: requests permitidos antes de fechar.
	// Default 3.
	MaxRequests uint32
	// Interval: janela rolling de contagem de falhas. Default 30s.
	Interval time.Duration
	// Timeout: tempo aberto antes de tentar half-open. Default 60s.
	Timeout time.Duration
	// FailThreshold: falhas consecutivas para abrir. Default 5.
	FailThreshold uint32
}

// DefaultConfig retorna config sã: breaker abre após 5 falhas em 30s,
// tenta 3 requests em half-open após 60s.
func DefaultConfig() Config {
	return Config{
		MaxRequests:   3,
		Interval:      30 * time.Second,
		Timeout:       60 * time.Second,
		FailThreshold: 5,
	}
}

// NewRegistry cria registry vazio.
func NewRegistry(cfg Config) *Registry {
	if cfg.MaxRequests == 0 {
		cfg.MaxRequests = 3
	}
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.FailThreshold == 0 {
		cfg.FailThreshold = 5
	}
	return &Registry{
		breakers: make(map[cacheKey]*gobreaker.CircuitBreaker[any]),
		config:   cfg,
	}
}

// GetOrCreate retorna o breaker para (tenantID, channel), criando
// sob demanda. Thread-safe.
func (r *Registry) GetOrCreate(tenantID, channel string) *gobreaker.CircuitBreaker[any] {
	key := cacheKey{tenantID: tenantID, channel: channel}

	r.mu.RLock()
	cb, ok := r.breakers[key]
	r.mu.RUnlock()
	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check após upgrade do lock
	if cb, ok = r.breakers[key]; ok {
		return cb
	}

	settings := gobreaker.Settings{
		Name:        fmt.Sprintf("mez/%s/%s", tenantID, channel),
		MaxRequests: r.config.MaxRequests,
		Interval:    r.config.Interval,
		Timeout:     r.config.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= r.config.FailThreshold
		},
	}
	cb = gobreaker.NewCircuitBreaker[any](settings)
	r.breakers[key] = cb
	return cb
}

// Execute wraps fn num breaker. Se breaker aberto, retorna
// gobreaker.ErrOpenState ou ErrTooManyRequests sem chamar fn.
// Caso contrário, fn é executado; panic/error conta como falha.
func (r *Registry) Execute(tenantID, channel string, fn func() error) error {
	cb := r.GetOrCreate(tenantID, channel)
	_, err := cb.Execute(func() (any, error) {
		return nil, fn()
	})
	return err
}

// Snapshot retorna estado atual de todos os breakers (para telemetria).
// Map: key → state name (closed/half-open/open).
func (r *Registry) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.breakers))
	for k, cb := range r.breakers {
		state := cb.State()
		out[fmt.Sprintf("%s/%s", k.tenantID, k.channel)] = state.String()
	}
	return out
}
