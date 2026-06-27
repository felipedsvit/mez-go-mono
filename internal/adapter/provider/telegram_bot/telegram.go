// Package telegram_bot é o adapter do canal Telegram Bot API.
//
// Diferente do whatsmeow, o Telegram Bot API é stateless: a "conexão" é o
// long-poll (iniciado em Connect) que recebe updates. Cada update inbound é
// normalizado e publicado no sink (ou, no mez-go-mono, o handler do webhook
// /webhooks/telegram já faz a normalização). Aqui ficam apenas o outbound
// (Send + Actions) cobrindo:
//
//   - Mensagens: text, photo, video, voice, document, sticker, location.
//   - Ações: reaction, edit, revoke, mark_read, typing, presence.
//   - Markup: reply, inline keyboard.
//   - Pagamentos: invoice (gated por feature flag).
//
// Fonte de verdade: docs/canais/telegram.md.
package telegram_bot

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// BotClient é a superfície mínima do cliente Bot API que o adapter usa.
// O stub usado em testes implementa esta interface; em produção,
// satisfazendo com o SDK real (go-telegram/bot).
type BotClient interface {
	// Envio.
	SendMessage(ctx context.Context, chatID int64, text string, replyMarkup ReplyMarkup) (messageID string, err error)
	SendPhoto(ctx context.Context, chatID int64, url, caption string, replyMarkup ReplyMarkup) (string, error)
	SendVideo(ctx context.Context, chatID int64, url, caption string, replyMarkup ReplyMarkup) (string, error)
	SendVoice(ctx context.Context, chatID int64, url, caption string, replyMarkup ReplyMarkup) (string, error)
	SendAudio(ctx context.Context, chatID int64, url, caption string, replyMarkup ReplyMarkup) (string, error)
	SendDocument(ctx context.Context, chatID int64, url, caption, filename string, replyMarkup ReplyMarkup) (string, error)
	SendSticker(ctx context.Context, chatID int64, fileID string) (string, error)
	SendLocation(ctx context.Context, chatID int64, latitude, longitude float64) (string, error)
	SendInvoice(ctx context.Context, chatID int64, payload Invoice) (string, error)

	// Ações de mensagem.
	EditMessageText(ctx context.Context, chatID int64, messageID string, newText string, replyMarkup ReplyMarkup) error
	DeleteMessage(ctx context.Context, chatID int64, messageID string) error
	SetMessageReaction(ctx context.Context, chatID int64, messageID string, emoji string) error

	// Estado de typing.
	SendChatAction(ctx context.Context, chatID int64, action string) error
}

// ReplyMarkup é uma abstração de reply_markup do Bot API. Pode ser InlineKeyboard
// ou ReplyKeyboardMarkup. Mantido pequeno para o adapter; SDKs concretos
// fazem o marshaling.
type ReplyMarkup interface{}

// InlineKeyboard representa um teclado inline (botões sob a mensagem).
type InlineKeyboard struct {
	Buttons [][]InlineButton
}

// InlineButton é um botão de teclado inline.
type InlineButton struct {
	Text         string
	URL          string
	CallbackData string
}

// Invoice é a estrutura simplificada de uma fatura para o Bot API.
type Invoice struct {
	Title       string
	Description string
	Payload     string
	Currency    string
	Amount      int64 // em menor unidade (cents)
}

// Adapter implementa port.Sender para Telegram.
type Adapter struct {
	tenant   domain.TenantID
	bot      BotClient
	log      zerolog.Logger
	payments bool
}

// Option configura o Adapter (feature flags).
type Option func(*Adapter)

// WithPayments habilita invoice (CapPayments). Default false.
func WithPayments(enabled bool) Option {
	return func(a *Adapter) { a.payments = enabled }
}

// New cria o adapter.
func New(tenant domain.TenantID, bot BotClient, log zerolog.Logger, opts ...Option) *Adapter {
	l := log.With().Str("channel", string(domain.ChannelTGBot)).Str("tenant", string(tenant)).Logger()
	a := &Adapter{tenant: tenant, bot: bot, log: l}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Channel retorna o canal.
func (a *Adapter) Channel() domain.Channel { return domain.ChannelTGBot }

// Capabilities retorna o set suportado.
func (a *Adapter) Capabilities() port.CapabilitySet {
	caps := TelegramCapabilities()
	if a.payments {
		caps[port.CapPayments] = true
	}
	return caps
}

// Send entrega mensagem ou executa ação.
func (a *Adapter) Send(ctx context.Context, req port.OutboundRequest) (string, error) {
	if req.Action != "" {
		return a.doAction(ctx, req)
	}

	chatID, err := parseChatID(req.PeerID)
	if err != nil {
		return "", err
	}

	rm := buildReplyMarkup(req.Metadata)

	switch req.Type {
	case domain.MessageTypeText, "":
		msgID, err := a.bot.SendMessage(ctx, chatID, req.Body, rm)
		if err != nil {
			return "", fmt.Errorf("telegram: enviar mensagem: %w", err)
		}
		return msgID, nil

	case domain.MessageTypeImage:
		url, _ := req.Metadata["media_url"].(string)
		return a.bot.SendPhoto(ctx, chatID, url, req.Body, rm)
	case domain.MessageTypeVideo:
		url, _ := req.Metadata["media_url"].(string)
		return a.bot.SendVideo(ctx, chatID, url, req.Body, rm)
	case domain.MessageTypeAudio:
		url, _ := req.Metadata["media_url"].(string)
		// O Bot API tem SendAudio (música) e SendVoice (nota de voz).
		// Aqui tratamos como áudio genérico; pode ser refinado por flag voice.
		return a.bot.SendAudio(ctx, chatID, url, req.Body, rm)
	case domain.MessageTypeDocument:
		url, _ := req.Metadata["media_url"].(string)
		fn, _ := req.Metadata["filename"].(string)
		return a.bot.SendDocument(ctx, chatID, url, req.Body, fn, rm)
	case domain.MessageTypeSticker:
		fileID, _ := req.Metadata["sticker_file_id"].(string)
		return a.bot.SendSticker(ctx, chatID, fileID)
	case domain.MessageTypeLocation:
		lat, _ := req.Metadata["latitude"].(float64)
		lng, _ := req.Metadata["longitude"].(float64)
		return a.bot.SendLocation(ctx, chatID, lat, lng)
	default:
		return "", fmt.Errorf("telegram: type %q não implementado", req.Type)
	}
}

// doAction executa ações de canal.
func (a *Adapter) doAction(ctx context.Context, req port.OutboundRequest) (string, error) {
	chatID, err := parseChatID(req.PeerID)
	if err != nil {
		return "", err
	}
	switch req.Action {
	case port.ActionReaction:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("telegram: reaction sem target_provider_id")
		}
		emoji := req.ReactionEmoji
		if emoji == "" {
			emoji = req.Body
		}
		if err := a.bot.SetMessageReaction(ctx, chatID, req.TargetProviderID, emoji); err != nil {
			return "", fmt.Errorf("telegram: reaction: %w", err)
		}
		return "", nil

	case port.ActionEdit:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("telegram: edit sem target_provider_id")
		}
		newText := req.NewBody
		if newText == "" {
			newText = req.Body
		}
		rm := buildReplyMarkup(req.Metadata)
		if err := a.bot.EditMessageText(ctx, chatID, req.TargetProviderID, newText, rm); err != nil {
			return "", fmt.Errorf("telegram: edit: %w", err)
		}
		return "", nil

	case port.ActionRevoke:
		if req.TargetProviderID == "" {
			return "", fmt.Errorf("telegram: revoke sem target_provider_id")
		}
		if err := a.bot.DeleteMessage(ctx, chatID, req.TargetProviderID); err != nil {
			return "", fmt.Errorf("telegram: revoke: %w", err)
		}
		return "", nil

	case port.ActionMarkRead:
		// Telegram não tem mark_read explícito. Implementação: typing off.
		// Mantido para simetria com WABA/whatsmeow.
		if err := a.bot.SendChatAction(ctx, chatID, ""); err != nil {
			return "", fmt.Errorf("telegram: mark_read: %w", err)
		}
		return "", nil

	case port.ActionTyping:
		state := req.State
		if state == "" {
			state = "typing"
		}
		if err := a.bot.SendChatAction(ctx, chatID, state); err != nil {
			return "", fmt.Errorf("telegram: typing: %w", err)
		}
		return "", nil

	case port.ActionPresence:
		// Telegram não tem presence dedicado; mapeamos para typing.
		state := req.State
		if state == "" {
			state = "typing"
		}
		if err := a.bot.SendChatAction(ctx, chatID, state); err != nil {
			return "", fmt.Errorf("telegram: presence: %w", err)
		}
		return "", nil

	default:
		return "", fmt.Errorf("telegram: ação desconhecida: %q", req.Action)
	}
}

func parseChatID(s string) (int64, error) {
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

// buildReplyMarkup deriva o ReplyMarkup do payload. Suporta o shape
// "inline_keyboard": [[{"text":"OK","callback_data":"ok"}]].
func buildReplyMarkup(metadata map[string]any) ReplyMarkup {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata["inline_keyboard"]
	if !ok {
		return nil
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil
	}
	kb := InlineKeyboard{Buttons: make([][]InlineButton, 0, len(rows))}
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok {
			return nil
		}
		buttons := make([]InlineButton, 0, len(row))
		for _, b := range row {
			m, ok := b.(map[string]any)
			if !ok {
				return nil
			}
			btn := InlineButton{}
			if v, ok := m["text"].(string); ok {
				btn.Text = v
			}
			if v, ok := m["url"].(string); ok {
				btn.URL = v
			}
			if v, ok := m["callback_data"].(string); ok {
				btn.CallbackData = v
			}
			buttons = append(buttons, btn)
		}
		kb.Buttons = append(kb.Buttons, buttons)
	}
	return kb
}

// Compile-time assertion: we satisfy port.Sender.
var _ port.Sender = (*Adapter)(nil)
