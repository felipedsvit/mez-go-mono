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
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

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
}

// NewHub cria o hub.
func NewHub(log zerolog.Logger) *Hub {
	return &Hub{
		log:         log.With().Str("component", "ws.Hub").Logger(),
		subscribers: make(map[string]map[*Subscriber]struct{}),
	}
}

// Subscribe adiciona um subscriber. Caller deve chamar Unsubscribe ao
// desconectar.
func (h *Hub) Subscribe(tenantID string, s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subscribers[tenantID]; !ok {
		h.subscribers[tenantID] = make(map[*Subscriber]struct{})
	}
	h.subscribers[tenantID][s] = struct{}{}
	h.log.Debug().Str("tenant", tenantID).Int("total", len(h.subscribers[tenantID])).Msg("ws: subscribed")
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

// Close fecha o subscriber. Idempotente.
func (s *Subscriber) Close() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.send)
	_ = s.conn.Close()
}

// Upgrader padrão (verifica origin e upgrade HTTP → WS).
var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Fase 5: aceita qualquer origin (dev). Production: validar.
		return true
	},
}
