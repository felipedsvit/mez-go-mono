package instagram

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// pageInboxAppID é o app id fixo da Page Inbox da Meta (o "humano" na UI). Quando
// o controle passa para ele, a thread está sob atendimento humano na inbox.
const pageInboxAppID = "263902037430900"

// ThreadOwner identifica quem detém o controle de uma thread no Handover Protocol
// (instagram-direct.md §5.4).
type ThreadOwner string

const (
	// OwnerApp: este app (Primary Receiver) controla a thread e pode responder.
	OwnerApp ThreadOwner = "app"
	// OwnerPageInbox: a Page Inbox (humano) controla a thread.
	OwnerPageInbox ThreadOwner = "page_inbox"
	// OwnerSecondary: outro app secondary receiver controla/solicita a thread.
	OwnerSecondary ThreadOwner = "secondary"
)

// HandoverEvent é um evento do Handover Protocol normalizado a partir do webhook
// (pass/take/request_thread_control). PeerID (IGSID) identifica a conversa 1:1.
type HandoverEvent struct {
	TenantID domain.TenantID
	PeerID   string
	Owner    ThreadOwner
	AppID    string
	Metadata string
}

// TenantThreadConfig registra quem é o owner atual de uma thread, por tenant.
type TenantThreadConfig struct {
	TenantID   domain.TenantID
	PeerID     string
	Owner      ThreadOwner
	OwnerAppID string
	UpdatedAt  time.Time
}

// ThreadStore mantém o owner de cada thread em memória (MVP). Reconstruível por
// re-sync dos webhooks de handover, então durabilidade não é crítica na V1;
// persistência em tabela fica para V1.1.
type ThreadStore struct {
	mu sync.RWMutex
	m  map[string]TenantThreadConfig
}

// NewThreadStore cria um store vazio.
func NewThreadStore() *ThreadStore {
	return &ThreadStore{m: make(map[string]TenantThreadConfig)}
}

func threadKey(tenant domain.TenantID, peer string) string {
	return string(tenant) + "|" + peer
}

// Apply atualiza o owner de uma thread a partir de um HandoverEvent.
func (s *ThreadStore) Apply(ev HandoverEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[threadKey(ev.TenantID, ev.PeerID)] = TenantThreadConfig{
		TenantID:   ev.TenantID,
		PeerID:     ev.PeerID,
		Owner:      ev.Owner,
		OwnerAppID: ev.AppID,
		UpdatedAt:  time.Now(),
	}
}

// Set registra explicitamente o owner (usado após uma ação de controle bem
// sucedida disparada pelo próprio Mez).
func (s *ThreadStore) Set(tenant domain.TenantID, peer string, owner ThreadOwner, appID string) {
	s.Apply(HandoverEvent{TenantID: tenant, PeerID: peer, Owner: owner, AppID: appID})
}

// Get devolve a config de owner de uma thread, se conhecida.
func (s *ThreadStore) Get(tenant domain.TenantID, peer string) (TenantThreadConfig, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.m[threadKey(tenant, peer)]
	return c, ok
}

// ownerFromAppID deriva o ThreadOwner a partir do app id que recebeu o controle.
func ownerFromAppID(appID string) ThreadOwner {
	if appID == pageInboxAppID {
		return OwnerPageInbox
	}
	return OwnerSecondary
}

// --- Operações de controle (cliente HTTP) ---

// PassThreadControl entrega o controle da thread a outro app (tipicamente a Page
// Inbox para atendimento humano). instagram-direct.md §5.4.
func (c *Client) PassThreadControl(ctx context.Context, recipientID, targetAppID, metadata string) error {
	body := map[string]any{
		"recipient":     map[string]any{"id": recipientID},
		"target_app_id": targetAppID,
	}
	if metadata != "" {
		body["metadata"] = metadata
	}
	url := fmt.Sprintf("%s/%s/me/pass_thread_control", c.baseURL, c.version)
	_, err := c.doJSON(ctx, http.MethodPost, url, body)
	return err
}

// TakeThreadControl retoma o controle da thread para este app (Primary Receiver).
func (c *Client) TakeThreadControl(ctx context.Context, recipientID, metadata string) error {
	body := map[string]any{"recipient": map[string]any{"id": recipientID}}
	if metadata != "" {
		body["metadata"] = metadata
	}
	url := fmt.Sprintf("%s/%s/me/take_thread_control", c.baseURL, c.version)
	_, err := c.doJSON(ctx, http.MethodPost, url, body)
	return err
}

// RequestThreadControl pede o controle da thread ao Primary Receiver (usado por
// um secondary receiver).
func (c *Client) RequestThreadControl(ctx context.Context, recipientID, metadata string) error {
	body := map[string]any{"recipient": map[string]any{"id": recipientID}}
	if metadata != "" {
		body["metadata"] = metadata
	}
	url := fmt.Sprintf("%s/%s/me/request_thread_control", c.baseURL, c.version)
	_, err := c.doJSON(ctx, http.MethodPost, url, body)
	return err
}
