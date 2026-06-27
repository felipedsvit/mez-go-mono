// Package websocket — hub.go: WS hub per-tenant com fan-out (Fase 5 #71).
//
// Substitui o hub do pai (`mez-go/internal/transport/websocket/`).
// Decisão arquitetural crítica: Hub vive no mesmo binário que o adminweb
// (single-binary mono). Mutex global protege estado; cada subscriber tem
// canal bounded (drop-safe) e recover() por goroutine de handler (C10).
//
// Protocolo (minimal):
//   - Client conecta em `/app/ws?tenant=<id>` (cookie session válida).
//   - Server subscribe o cliente ao fan-out do tenant.
//   - Server faz broadcast de mensagens inbound via bus.SubscribeInbound.
//   - Heartbeat: ping a cada 30s; cliente responde pong; timeout = disconnect.
package websocket

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

var ErrHubClosed = errors.New("ws: hub is closed")

const (
	// Buffer por subscriber (drop-safe: se o cliente demorar, mensagens
	// são dropadas e o reconciler/StatusConsumer cobre o gap).
	subscriberBuffer = 64
	// writeWait é o tempo máximo para escrever uma mensagem.
	writeWait = 10 * time.Second
	// pongWait é o tempo máximo entre pongs.
	pongWait = 60 * time.Second
	// pingPeriod é menor que pongWait (RFC 6455).
	pingPeriod = 30 * time.Second
)

// Message é o envelope WS (genérico, marshalled como JSON).
type Message struct {
	Event   string         `json:"event"` // "inbound" | "status" | "lifecycle" | "qr" | "ping"
	Channel string         `json:"channel,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Hub é o fan-out per-tenant.
type Hub struct {
	log zerolog.Logger

	mu          sync.RWMutex
	subscribers map[string]map[*Subscriber]struct{} // tenantID -> set
	closed      atomic.Bool
}

// NewHub cria o hub.
func NewHub(log zerolog.Logger) *Hub {
	return &Hub{
		log:         log.With().Str("component", "ws.Hub").Logger(),
		subscribers: make(map[string]map[*Subscriber]struct{}),
	}
}

// Subscribe adiciona um subscriber. Caller deve chamar Unsubscribe ao
// desconectar. Retorna ErrHubClosed se o hub foi fechado.
func (h *Hub) Subscribe(tenantID string, s *Subscriber) error {
	if h.closed.Load() {
		return ErrHubClosed
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed.Load() {
		return ErrHubClosed
	}
	if _, ok := h.subscribers[tenantID]; !ok {
		h.subscribers[tenantID] = make(map[*Subscriber]struct{})
	}
	h.subscribers[tenantID][s] = struct{}{}
	h.log.Debug().Str("tenant", tenantID).Int("total", len(h.subscribers[tenantID])).Msg("ws: subscribed")
	return nil
}

// Unsubscribe remove um subscriber.
func (h *Hub) Unsubscribe(tenantID string, s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if subs, ok := h.subscribers[tenantID]; ok {
		delete(subs, s)
		if len(subs) == 0 {
			delete(h.subscribers, tenantID)
		}
	}
	h.log.Debug().Str("tenant", tenantID).Msg("ws: unsubscribed")
}

// Broadcast envia msg para todos os subscribers do tenant (non-blocking).
// Drop-safe: se o buffer do subscriber estiver cheio, descarta.
func (h *Hub) Broadcast(tenantID string, msg Message) {
	h.mu.RLock()
	subs := make([]*Subscriber, 0, len(h.subscribers[tenantID]))
	for s := range h.subscribers[tenantID] {
		subs = append(subs, s)
	}
	h.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.send <- msg:
		default:
			h.log.Warn().Str("tenant", tenantID).Msg("ws: subscriber buffer cheio; drop")
		}
	}
}

// Stats retorna contadores para métricas.
func (h *Hub) Stats() map[string]int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[string]int, len(h.subscribers))
	for k, v := range h.subscribers {
		out[k] = len(v)
	}
	return out
}

// Shutdown fecha todos os subscribers e impede novos. Idempotente.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	if !h.closed.CompareAndSwap(false, true) {
		h.mu.Unlock()
		return nil // já fechado
	}

	// Coleta todos os subscribers.
	all := make([]*Subscriber, 0)
	for _, subs := range h.subscribers {
		for s := range subs {
			all = append(all, s)
		}
	}
	// Limpa o mapa para não ter mais referências.
	h.subscribers = make(map[string]map[*Subscriber]struct{})
	h.mu.Unlock()

	h.log.Info().Int("subscribers", len(all)).Msg("ws: shutting down hub")

	// Fecha todos os subscribers concorrentemente (cada Close é idempotente).
	done := make(chan struct{}, len(all))
	for _, s := range all {
		s := s
		go func() {
			s.Close()
			done <- struct{}{}
		}()
	}

	// Aguarda todos ou timeout.
	received := 0
	for range all {
		select {
		case <-done:
			received++
		case <-ctx.Done():
			h.log.Warn().Int("remaining", len(all)-received).Msg("ws: shutdown timeout")
			return ctx.Err()
		}
	}
	h.log.Info().Msg("ws: hub shutdown complete")
	return nil
}

// Subscriber é uma conexão WS individual.
type Subscriber struct {
	conn    *websocket.Conn
	send    chan Message
	tenant  string
	log     zerolog.Logger
	closeMu sync.Mutex
	closed  bool
}

// NewSubscriber cria o subscriber.
func NewSubscriber(tenant string, conn *websocket.Conn, log zerolog.Logger) *Subscriber {
	return &Subscriber{
		conn:   conn,
		send:   make(chan Message, subscriberBuffer),
		tenant: tenant,
		log:    log.With().Str("component", "ws.Subscriber").Str("tenant", tenant).Logger(),
	}
}

// ReadPump lê mensagens do cliente (no-op para o nosso caso: cliente só
// recebe). Detecta fechamento via SetReadDeadline + pong handler.
func (s *Subscriber) ReadPump() {
	defer s.Close()
	s.conn.SetReadLimit(4096)
	_ = s.conn.SetReadDeadline(time.Now().Add(pongWait))
	s.conn.SetPongHandler(func(string) error {
		_ = s.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		// Não esperamos mensagens do cliente; só mantemos a conexão viva.
		if _, _, err := s.conn.NextReader(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.log.Debug().Err(err).Msg("ws: read error")
			}
			return
		}
	}
}

// WritePump escreve mensagens do canal send para a conexão. Heartbeat
// ping a cada 30s.
func (s *Subscriber) WritePump(ctx context.Context) {
	defer s.Close()
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-s.send:
			_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = s.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := s.conn.WriteJSON(msg); err != nil {
				s.log.Debug().Err(err).Msg("ws: write error")
				return
			}
		case <-pingTicker.C:
			_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close fecha o subscriber. Idempotente. Nil-safe para testes.
func (s *Subscriber) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.send)
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// DefaultUpgrader devolve um Upgrader permissivo para dev/test apenas.
// **Não usar em produção.** Production deve injetar via NewHandler
// um Upgrader criado por NewUpgrader(UpgraderConfig{...}) com allowlist.
//
// Mantido temporariamente para retro-compat com testes que ainda
// referenciam o símbolo. Será removido após migration completa (issue #129).
func DefaultUpgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(*http.Request) bool { return true },
	}
}
