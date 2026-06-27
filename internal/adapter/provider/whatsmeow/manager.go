// Package whatsmeow — manager.go: Manager (1 client/tenant, lazy init, bounded).
//
// Substitui o pool multi-tenant do pai por \`map[tenantID]*clientEntry\`
// + dispatcher per-tenant. Bounded por \`MEZ_MAX_ACTIVE_TENANTS\` (default 100)
// com LRU eviction.
//
// Padrão arquitetural: o Manager **não** implementa \`port.Sender\` diretamente.
// Ele expõe \`Get(ctx, tenantID)\` que retorna um \`*whatsmeow.Adapter\` (que
// implementa \`port.Sender\`). O Manager é o portão de lifecycle; o Adapter
// é a porta outbound. O \`port.SenderRegistry\` (Fase 3) usa o Manager
// internamente para criar o \`*whatsmeow.Adapter\` on-demand.
package whatsmeow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// Config configura o Manager.
type Config struct {
	MaxActiveTenants int           // default 100
	ConnectTimeout   time.Duration // default 30s
	DisconnectGrace  time.Duration // default 10s (D10)
	MaxBackoff       time.Duration // default 30min (E5)
	WarmupEnabled    bool          // default true (E6)
}

// DefaultConfig retorna a config default.
func DefaultConfig() Config {
	return Config{
		MaxActiveTenants: 100,
		ConnectTimeout:   30 * time.Second,
		DisconnectGrace:  10 * time.Second,
		MaxBackoff:       30 * time.Minute,
		WarmupEnabled:    true,
	}
}

// Manager gerencia o ciclo de vida dos clients whatsmeow.
type Manager struct {
	cfg    Config
	log    zerolog.Logger
	stateR *postgres.WhatsAppStateRepo

	mu      sync.Mutex
	clients map[domain.TenantID]*clientEntry
	order   []domain.TenantID // LRU: front=most-recent
}

// clientEntry é o estado interno por tenant.
type clientEntry struct {
	adapter   *Adapter
	dispatch  *Dispatcher
	createdAt time.Time
	lastUsed  time.Time
}

// NewManager cria o Manager.
func NewManager(cfg Config, stateR *postgres.WhatsAppStateRepo, log zerolog.Logger) *Manager {
	if cfg.MaxActiveTenants <= 0 {
		cfg.MaxActiveTenants = 100
	}
	return &Manager{
		cfg:     cfg,
		log:     log.With().Str("component", "whatsmeow.Manager").Logger(),
		stateR:  stateR,
		clients: make(map[domain.TenantID]*clientEntry),
	}
}

// GetOrCreate retorna o *Adapter para um tenant, criando on-demand.
// Aplica LRU eviction se MaxActiveTenants for atingido.
func (m *Manager) GetOrCreate(ctx context.Context, tenantID domain.TenantID, factory ClientFactory) (*Adapter, error) {
	m.mu.Lock()
	if e, ok := m.clients[tenantID]; ok {
		e.lastUsed = time.Now()
		m.touchLRU(tenantID)
		m.mu.Unlock()
		return e.adapter, nil
	}
	m.mu.Unlock()

	// Cria fora do lock para não segurar durante I/O.
	client, err := factory(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("manager: factory: %w", err)
	}
	if client == nil {
		return nil, errors.New("manager: factory returned nil")
	}

	dispatcher := NewDispatcher(m.log)
	adapter := NewAdapter(tenantID, client, dispatcher, m.stateR, m.log)

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-check (race entre dois Get).
	if e, ok := m.clients[tenantID]; ok {
		client.Disconnect()
		e.lastUsed = time.Now()
		m.touchLRU(tenantID)
		return e.adapter, nil
	}
	// LRU eviction.
	if len(m.clients) >= m.cfg.MaxActiveTenants {
		m.evictLRU()
	}
	now := time.Now()
	m.clients[tenantID] = &clientEntry{
		adapter:   adapter,
		dispatch:  dispatcher,
		createdAt: now,
		lastUsed:  now,
	}
	m.order = append(m.order, tenantID)
	return adapter, nil
}

// Get retorna o *Adapter sem criar; retorna false se não existe.
func (m *Manager) Get(tenantID domain.TenantID) (*Adapter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.clients[tenantID]; ok {
		e.lastUsed = time.Now()
		m.touchLRU(tenantID)
		return e.adapter, true
	}
	return nil, false
}

// DisconnectAll encerra todos os clients graciosamente (D10).
// Chamado no shutdown coordenado (signal → Manager.DisconnectAll).
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	entries := make([]*clientEntry, 0, len(m.clients))
	for _, e := range m.clients {
		entries = append(entries, e)
	}
	m.mu.Unlock()

	for _, e := range entries {
		e.dispatch.Stop()
		e.adapter.client.Disconnect()
	}
	m.log.Info().Int("clients", len(entries)).Msg("manager: disconnected all")
}

// Disconnect encerra o client de 1 tenant (usado pelo reset do tenant
// — issue #83). Idempotente: retorna nil se o tenant não tem client ativo.
// Após Disconnect, próximo GetOrCreate recria o client limpo.
func (m *Manager) Disconnect(ctx context.Context, tenantID domain.TenantID) error {
	m.mu.Lock()
	e, ok := m.clients[tenantID]
	if !ok {
		m.mu.Unlock()
		return nil // já não existe
	}
	delete(m.clients, tenantID)
	// Remove também do LRU order.
	for i, t := range m.order {
		if t == tenantID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	e.dispatch.Stop()
	e.adapter.client.Disconnect()
	m.log.Info().Str("tenant", string(tenantID)).Msg("manager: tenant disconnected (reset)")
	return nil
}

// Size retorna o número de clients ativos.
func (m *Manager) Size() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clients)
}

// touchLRU move o tenant para o final (most-recent). Caller deve ter o lock.
func (m *Manager) touchLRU(tenantID domain.TenantID) {
	for i, t := range m.order {
		if t == tenantID {
			m.order = append(m.order[:i], m.order[i+1:]...)
			m.order = append(m.order, tenantID)
			return
		}
	}
}

// evictLRU remove o tenant menos recente. Caller deve ter o lock.
func (m *Manager) evictLRU() {
	if len(m.order) == 0 {
		return
	}
	victim := m.order[0]
	m.order = m.order[1:]
	if e, ok := m.clients[victim]; ok {
		e.dispatch.Stop()
		e.adapter.client.Disconnect()
		delete(m.clients, victim)
		m.log.Warn().Str("victim", string(victim)).Msg("manager: LRU eviction")
	}
}

// ClientFactory cria um *whatsmeow.Client (real ou stub) para um tenant.
type ClientFactory func(ctx context.Context, tenantID domain.TenantID) (Client, error)

// CurrentQR retorna o último QR code gerado para o tenant (Fase 4 #68 API).
// Implementa a interface `api.QRCodeProvider` no server.
//
// Fase 4: stub sempre retorna o QR "stub-qr-code". Production: lê o canal
// `GetQRChannel` do *whatsmeow.Client real e devolve o último Code.
func (m *Manager) CurrentQR(ctx context.Context, tenantID domain.TenantID) (string, error) {
	m.mu.Lock()
	e, ok := m.clients[tenantID]
	m.mu.Unlock()
	if !ok {
		// Cria on-demand via factory default (stub).
		adapter, err := m.GetOrCreate(ctx, tenantID, func(_ context.Context, _ domain.TenantID) (Client, error) {
			return NewStubClient(string(tenantID), m.log), nil
		})
		if err != nil {
			return "", err
		}
		return m.readQR(ctx, adapter)
	}
	return m.readQR(ctx, e.adapter)
}

func (m *Manager) readQR(ctx context.Context, a *Adapter) (string, error) {
	ch, err := a.client.GetQRChannel(ctx)
	if err != nil {
		return "", err
	}
	if ch == nil {
		return "", nil // já conectado
	}
	// Lê o primeiro evento do canal (não-bloqueante).
	select {
	case evt, ok := <-ch:
		if !ok {
			return "", nil
		}
		return evt.Code, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
