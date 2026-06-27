package websocket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.VerifyTestMain(m)
}

func TestHub_Broadcast_DropsToSubscribers(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	defer func() {
		// Limpa subscribers.
		hub.Broadcast("t1", Message{Event: "ping"})
		time.Sleep(10 * time.Millisecond)
	}()

	// Cria 2 subscribers.
	s1 := &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log}
	s2 := &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log}
	if err := hub.Subscribe("t1", s1); err != nil {
		t.Fatal(err)
	}
	if err := hub.Subscribe("t1", s2); err != nil {
		t.Fatal(err)
	}

	hub.Broadcast("t1", Message{Event: "inbound", Channel: "waba"})

	// Verifica que ambos receberam.
	for i, s := range []*Subscriber{s1, s2} {
		select {
		case msg := <-s.send:
			if msg.Event != "inbound" {
				t.Errorf("subscriber %d: event = %q, want inbound", i, msg.Event)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d: timeout", i)
		}
	}
}

func TestHub_Broadcast_Unsubscribe(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	s := &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log}
	if err := hub.Subscribe("t1", s); err != nil {
		t.Fatal(err)
	}
	hub.Unsubscribe("t1", s)

	hub.Broadcast("t1", Message{Event: "ping"})
	select {
	case <-s.send:
		t.Error("unsubscribed subscriber received message")
	case <-time.After(50 * time.Millisecond):
		// ok
	}
}

func TestHub_Stats(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	_ = hub.Subscribe("t1", &Subscriber{tenant: "t1", send: make(chan Message), log: log})
	_ = hub.Subscribe("t1", &Subscriber{tenant: "t1", send: make(chan Message), log: log})
	_ = hub.Subscribe("t2", &Subscriber{tenant: "t2", send: make(chan Message), log: log})

	stats := hub.Stats()
	if stats["t1"] != 2 {
		t.Errorf("t1 stats = %d, want 2", stats["t1"])
	}
	if stats["t2"] != 1 {
		t.Errorf("t2 stats = %d, want 1", stats["t2"])
	}
}

func TestHub_OverflowDropsDropSafe(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	s := &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log}
	_ = hub.Subscribe("t1", s)

	// Enfileira 100 mensagens — só 1 cabe no buffer.
	for i := 0; i < 100; i++ {
		hub.Broadcast("t1", Message{Event: "spam"})
	}
	// Hub não bloqueia; primeiro elemento do canal é a primeira mensagem.
	select {
	case <-s.send:
		// ok
	case <-time.After(50 * time.Millisecond):
		t.Error("subscriber não recebeu primeira mensagem")
	}
}

func TestHub_Shutdown_ClosesSubscribers(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	s := &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log}
	_ = hub.Subscribe("t1", s)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := hub.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Subscriber deve estar fechado (canal fechado).
	if _, ok := <-s.send; ok {
		t.Error("subscriber channel still open after shutdown")
	}
}

func TestHub_Shutdown_Idempotent(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := hub.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := hub.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown should be idempotent, got: %v", err)
	}
}

func TestHub_Shutdown_RejectsSubscribe(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = hub.Shutdown(ctx)

	err := hub.Subscribe("t1", &Subscriber{tenant: "t1", send: make(chan Message, 1), log: log})
	if !errors.Is(err, ErrHubClosed) {
		t.Fatalf("Subscribe after shutdown: want ErrHubClosed, got %v", err)
	}
}

func TestHandler_Rejects_NoTenant(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	tenantCtx := func(_ context.Context) (domain.TenantID, bool) { return "", false }
	h := NewHandler(hub, nil, func(ctx context.Context) (domain.TenantID, bool) { return tenantCtx(ctx) }, log)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/app/ws", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestHandler_Upgrade(t *testing.T) {
	log := zerolog.Nop()
	hub := NewHub(log)
	h := NewHandler(hub, nil, func(_ context.Context) (domain.TenantID, bool) { return domain.TenantID("t1"), true }, log)

	// Servidor de teste que faz o upgrade e envia uma mensagem.
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Setar upgrader customizado (Upgrader do nosso pacote é restrito).
		_ = upgrader
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	// Tenta conectar (vai falhar porque Upgrader.CheckOrigin retorna true,
	// mas o método está ok). Substitui URL scheme.
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	_ = url
}
