// Package websocket — handler.go: HTTP handler de upgrade /app/ws.
//
// Wire: \`/app/ws\` → upgrader.Upgrade → NewSubscriber → hub.Subscribe.
// ReadPump + WritePump rodando em goroutines separadas.
package websocket

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// Handler é o handler HTTP para o upgrade WS.
type Handler struct {
	hub           *Hub
	tenantFromCtx func(context.Context) (domain.TenantID, bool)
	log           zerolog.Logger
}

// NewHandler cria o handler.
func NewHandler(hub *Hub, tenantFromCtx func(context.Context) (domain.TenantID, bool), log zerolog.Logger) *Handler {
	return &Handler{
		hub:           hub,
		tenantFromCtx: tenantFromCtx,
		log:           log.With().Str("component", "ws.Handler").Logger(),
	}
}

// ServeHTTP faz o upgrade. tenantID vem do context (cookie session middleware).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenantFromCtx(r.Context())
	if !ok {
		http.Error(w, "tenant required", http.StatusUnauthorized)
		return
	}
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Debug().Err(err).Msg("ws: upgrade failed")
		return
	}

	sub := NewSubscriber(string(tenantID), conn, h.log)
	h.hub.Subscribe(string(tenantID), sub)
	defer h.hub.Unsubscribe(string(tenantID), sub)

	// writePump recupera de panic (C10).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				h.log.Error().Interface("panic", r).Msg("ws: panic in writePump (C10); recovered")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		sub.WritePump(ctx)
	}()
	// readPump também recuperado.
	func() {
		defer func() {
			if r := recover(); r != nil {
				h.log.Error().Interface("panic", r).Msg("ws: panic in readPump (C10); recovered")
			}
		}()
		sub.ReadPump()
	}()
}
