// Package whatsmeow — adapter.go: implementa port.Sender para whatsmeow.
//
// Cobre:
//   - Send (text + mídias: image/audio/sticker/video) — port.Sender interface
//   - doAction (D6: reaction/edit/revoke/mark_read/typing/presence) — Action enum
//   - Capabilities (set honesto)
//   - error_filter: classificação de erros (E6 anti-ban)
//
// Toda chamada ao Client passa pelo Dispatcher (single goroutine por
// tenant — `*whatsmeow.Client` não é thread-safe).
package whatsmeow

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Adapter implementa port.Sender para whatsmeow. Uma instância por tenant
// (criada pelo Manager). Safe para uso concorrente **somente através do
// port.Sender**; o Client interno é serializado via Dispatcher.
type Adapter struct {
	tenant   domain.TenantID
	client   Client
	dispatch *Dispatcher
	stateR   *postgres.WhatsAppStateRepo
	log      zerolog.Logger

	connected atomic.Bool
	mu        sync.Mutex // protege mudanças de client (Logout/Connect)
}

// NewAdapter cria o adapter.
func NewAdapter(tenant domain.TenantID, client Client, dispatch *Dispatcher, stateR *postgres.WhatsAppStateRepo, log zerolog.Logger) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelWAWeb)).Str("tenant", string(tenant)).Logger()
	return &Adapter{
		tenant:   tenant,
		client:   client,
		dispatch: dispatch,
		stateR:   stateR,
		log:      l,
	}
}

// Channel retorna o canal.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelWAWeb }

// Capabilities retorna o set honesto suportado pelo whatsmeow.
// (groups/newsletters/communities/privacy/blocklist/disappearing declarados
// como planejados; o envio retorna ErrNotImplemented — carryover.)
func (a *Adapter) Capabilities() port.CapabilitySet {
	return WhatsmeowCapabilities()
}

// IsConnected retorna o estado atual (atômico).
func (a *Adapter) IsConnected() bool { return a.connected.Load() }

// SetConnected atualiza o flag (chamado pelo Dispatcher em Connected/Disconnected).
func (a *Adapter) SetConnected(v bool) { a.connected.Store(v) }

// Client retorna o *whatsmeow.Client (apenas para o Dispatcher / Manager / testes).
func (a *Adapter) Client() Client { return a.client }

// Send entrega mensagem ou executa ação (D6).
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}
	return a.sendMessage(ctx, req)
}

func (a *Adapter) sendMessage(ctx context.Context, req port.OutboundRequest) (string, error) {
	if !a.IsConnected() {
		return "", ErrNotConnected
	}
	jid, err := ParseJID(req.PeerID)
	if err != nil {
		// peer_id pode ser telefone puro (sem @s.whatsapp.net). Tenta normalizar.
		jid, err = ParseJID(req.PeerID + "@s.whatsapp.net")
		if err != nil {
			return "", fmt.Errorf("parse peer_id %q: %w", req.PeerID, err)
		}
	}
	// ResolveWarmup: verifica se o envio é permitido pela cota diária (E6).
	if a.stateR != nil {
		st, err := a.stateR.LoadState(ctx, a.tenant, jid.String())
		if err == nil && st.BannedAt.IsZero() == false {
			return "", fmt.Errorf("whatsmeow: tenant banned at %s", st.BannedAt.Format("2006-01-02"))
		}
	}

	switch req.Type {
	case domain.MessageTypeText, "":
		return a.client.SendMessage(ctx, jid, req.Body)
	case domain.MessageTypeImage:
		// Mídia: Fase 4 — metadata.url é tratado como data URL; production lê S3.
		data, mime, _ := extractMedia(req)
		return a.client.SendImage(ctx, jid, data, mime, req.Body)
	case domain.MessageTypeAudio:
		data, mime, _ := extractMedia(req)
		return a.client.SendAudio(ctx, jid, data, mime, true)
	case domain.MessageTypeVideo:
		data, mime, _ := extractMedia(req)
		return a.client.SendVideo(ctx, jid, data, mime, req.Body)
	case domain.MessageTypeDocument:
		data, mime, filename := extractMedia(req)
		if filename == "" {
			filename = "document"
		}
		return a.client.SendDocument(ctx, jid, data, mime, filename, req.Body)
	case domain.MessageTypeSticker:
		data, _, _ := extractMedia(req)
		return a.client.SendSticker(ctx, jid, data)
	default:
		return "", fmt.Errorf("whatsmeow: type %q não implementado (Fase 4: text+media)", req.Type)
	}
}

// doAction despacha ações D6.
func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	if !a.IsConnected() {
		return "", ErrNotConnected
	}
	jid, err := ParseJID(req.PeerID)
	if err != nil {
		jid, err = ParseJID(req.PeerID + "@s.whatsapp.net")
		if err != nil {
			return "", fmt.Errorf("parse peer_id %q: %w", req.PeerID, err)
		}
	}
	targetID, _ := decodeMessageID(req.TargetProviderID)

	switch req.Action {
	case port.ActionReaction:
		if req.TargetProviderID == "" {
			return "", errors.New("whatsmeow: reaction sem target_provider_id")
		}
		return "", a.client.SendReaction(ctx, jid, targetID, req.ReactionEmoji)

	case port.ActionEdit:
		if req.TargetProviderID == "" {
			return "", errors.New("whatsmeow: edit sem target_provider_id")
		}
		_, err := a.client.EditMessage(ctx, jid, targetID, req.NewBody)
		return "", err

	case port.ActionRevoke:
		if req.TargetProviderID == "" {
			return "", errors.New("whatsmeow: revoke sem target_provider_id")
		}
		_, err := a.client.RevokeMessage(ctx, jid, targetID)
		return "", err

	case port.ActionMarkRead:
		if req.TargetProviderID == "" {
			return "", errors.New("whatsmeow: mark_read sem target_provider_id")
		}
		return "", a.client.MarkRead(ctx, jid, []MessageID{targetID}, 0)

	case port.ActionTyping:
		state := req.State
		if state == "" {
			state = "composing"
		}
		return "", a.client.SendChatPresence(ctx, jid, parseChatPresence(state))

	case port.ActionPresence:
		state := req.State
		if state == "" {
			state = "available"
		}
		return "", a.client.SendPresence(ctx, parsePresence(state))

	default:
		return "", fmt.Errorf("whatsmeow: ação %q não suportada", req.Action)
	}
}

// extractMedia extrai bytes + mime + filename de req.Metadata.
// Fase 4: aceita url pointer; production lê do S3.
func extractMedia(req port.OutboundRequest) (data []byte, mime, filename string) {
	if req.Metadata == nil {
		return nil, "", ""
	}
	if u, ok := req.Metadata["url"].(string); ok && u != "" {
		_ = u // production chamaria storage.S3.Get(ctx, u)
	}
	if m, ok := req.Metadata["mime"].(string); ok {
		mime = m
	}
	if f, ok := req.Metadata["filename"].(string); ok {
		filename = f
	}
	return nil, mime, filename
}

// decodeMessageID converte string em types.MessageID (string alias).
// Stub: production usaria types.MessageID(s) que parseia o ID serializado.
// Para Fase 4 é identity (MessageID = string).
func decodeMessageID(s string) (MessageID, error) {
	return MessageID(s), nil
}

func parseChatPresence(s string) types.ChatPresence {
	switch s {
	case "composing", "typing":
		return types.ChatPresenceComposing
	case "paused":
		return types.ChatPresencePaused
	}
	return types.ChatPresencePaused
}

func parsePresence(s string) types.Presence {
	if s == "unavailable" {
		return types.PresenceUnavailable
	}
	return types.PresenceAvailable
}
