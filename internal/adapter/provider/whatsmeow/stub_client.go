// Package whatsmeow — stub_client.go: stub do *whatsmeow.Client para o
// build verde (Fase 4). A produção substitui por *whatsmeow.Client real
// (com session pareada).
package whatsmeow

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
)

// stubWhatsmeowClient é a implementação default do Client usada pelo
// Manager no build verde. Não abre socket, não envia nada — apenas
// simula respostas para que o pipeline funcione end-to-end.
type stubWhatsmeowClient struct {
	tenant string
	log    zerolog.Logger
	conn   atomic.Bool
	seq    atomic.Uint64
}

var _ Client = (*stubWhatsmeowClient)(nil)

// NewStubClient cria um stub client para o tenant.
func NewStubClient(tenant string, log zerolog.Logger) *stubWhatsmeowClient {
	return &stubWhatsmeowClient{
		tenant: tenant,
		log:    log.With().Str("component", "whatsmeow.stub").Str("tenant", tenant).Logger(),
	}
}

func (s *stubWhatsmeowClient) Connect(_ context.Context) error {
	s.conn.Store(true)
	return nil
}

func (s *stubWhatsmeowClient) Disconnect() { s.conn.Store(false) }

func (s *stubWhatsmeowClient) IsConnected() bool { return s.conn.Load() }

func (s *stubWhatsmeowClient) nextID() string {
	n := s.seq.Add(1)
	return fmt.Sprintf("stub-%s-%d-%d", s.tenant, time.Now().Unix(), n)
}

func (s *stubWhatsmeowClient) SendMessage(_ context.Context, _ types.JID, _ string) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendImage(_ context.Context, _ types.JID, _ []byte, _, _ string) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendAudio(_ context.Context, _ types.JID, _ []byte, _ string, _ bool) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendDocument(_ context.Context, _ types.JID, _ []byte, _, _, _ string) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendSticker(_ context.Context, _ types.JID, _ []byte) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendVideo(_ context.Context, _ types.JID, _ []byte, _, _ string) (string, error) {
	return s.nextID(), nil
}

func (s *stubWhatsmeowClient) SendReaction(_ context.Context, _ types.JID, _ MessageID, _ string) error {
	return nil
}

func (s *stubWhatsmeowClient) EditMessage(_ context.Context, _ types.JID, _ MessageID, _ string) (bool, error) {
	return true, nil
}

func (s *stubWhatsmeowClient) RevokeMessage(_ context.Context, _ types.JID, _ MessageID) (bool, error) {
	return true, nil
}

func (s *stubWhatsmeowClient) MarkRead(_ context.Context, _ types.JID, _ []MessageID, _ int64) error {
	return nil
}

func (s *stubWhatsmeowClient) SendChatPresence(_ context.Context, _ types.JID, _ types.ChatPresence) error {
	return nil
}

func (s *stubWhatsmeowClient) SendPresence(_ context.Context, _ types.Presence) error {
	return nil
}

func (s *stubWhatsmeowClient) RejectCall(_ context.Context, _ types.JID, _ string) error {
	return nil
}

func (s *stubWhatsmeowClient) GetQRChannel(_ context.Context) (<-chan QRCodeEvent, error) {
	ch := make(chan QRCodeEvent, 1)
	ch <- QRCodeEvent{Code: "stub-qr-code", Event: "code"}
	close(ch)
	return ch, nil
}

func (s *stubWhatsmeowClient) AddEventHandler(_ EventHandler) uint32 { return 0 }

func (s *stubWhatsmeowClient) Logout(_ context.Context) error {
	s.conn.Store(false)
	return nil
}
