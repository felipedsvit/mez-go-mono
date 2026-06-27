// Package telegram_bot é o adapter do canal Telegram Bot API.
// O inbound vem via /webhooks/telegram (handler da Fase 2).
// Esta implementação cobre apenas outbound (Send + actions).
package telegram_bot

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// BotClient é a interface mínima do *tgbot.Bot que o adapter usa.
// Existe para isolar testes do SDK real.
type BotClient interface {
	SendMessage(ctx context.Context, chatID int64, text string) (string, error)
	SendChatAction(ctx context.Context, chatID int64, action string) error
}

// Adapter implementa port.Sender para Telegram.
type Adapter struct {
	tenant domain.TenantID
	bot    BotClient
	log    zerolog.Logger
}

// New cria o adapter.
func New(tenant domain.TenantID, bot BotClient, log zerolog.Logger) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelTGBot)).Str("tenant", string(tenant)).Logger()
	return &Adapter{tenant: tenant, bot: bot, log: l}
}

// Channel retorna o canal.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelTGBot }

// Capabilities retorna o set completo do Bot API.
func (a *Adapter) Capabilities() port.CapabilitySet { return TelegramCapabilities() }

// Send entrega mensagem ou ação.
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}
	if req.Type != domain.MessageTypeText {
		return "", fmt.Errorf("telegram: type %q não implementado (Phase 4)", req.Type)
	}
	chatID, err := parseChatID(req.PeerID)
	if err != nil {
		return "", err
	}
	return a.bot.SendMessage(ctx, chatID, req.Body)
}

func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	chatID, err := parseChatID(req.PeerID)
	if err != nil {
		return "", err
	}
	switch req.Action {
	case port.ActionTyping:
		state := req.State
		if state == "" {
			state = "typing"
		}
		return "", a.bot.SendChatAction(ctx, chatID, state)
	case port.ActionReaction, port.ActionEdit, port.ActionRevoke,
		port.ActionMarkRead, port.ActionPresence:
		return "", fmt.Errorf("telegram: %s stub (Phase 4)", req.Action)
	default:
		return "", fmt.Errorf("telegram: ação desconhecida: %q", req.Action)
	}
}

func parseChatID(s string) (int64, error) {
	// Phase 3: aceita só números. Phase 4 adiciona @username → ID resolve.
	if s == "" {
		return 0, fmt.Errorf("telegram: chat_id vazio")
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("telegram: chat_id inválido: %q", s)
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
