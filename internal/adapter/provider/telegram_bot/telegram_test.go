// Package telegram_bot — testes do adapter Telegram Bot API.
//
// Cobre:
//   - text message → BotClient.SendMessage com chatID parseado
//   - typing action → BotClient.SendChatAction
//   - reaction/edit/revoke/mark_read/presence → erro (Phase 4 stub)
//   - action desconhecida → erro
//   - chat_id inválido → erro (parseChatID)
//   - type não-text sem action → erro
//   - Capabilities: matriz completa do Bot API.
package telegram_bot

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// recordingBot implementa BotClient capturando chamadas.
type recordingBot struct {
	mu             sync.Mutex
	messages       []recordedMessage
	chatActions    []recordedAction
	sendMessageErr error
	sendActionErr  error
}

type recordedMessage struct {
	ChatID int64
	Text   string
}

type recordedAction struct {
	ChatID int64
	Action string
}

func (r *recordingBot) SendMessage(_ context.Context, chatID int64, text string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendMessageErr != nil {
		return "", r.sendMessageErr
	}
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: text})
	return "tg-msg-id-1", nil
}

func (r *recordingBot) SendChatAction(_ context.Context, chatID int64, action string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendActionErr != nil {
		return r.sendActionErr
	}
	r.chatActions = append(r.chatActions, recordedAction{ChatID: chatID, Action: action})
	return nil
}

func newTestAdapter(bot BotClient) *Adapter {
	return New(domain.TenantID("t1"), bot, zerolog.Nop())
}

func TestParseChatID_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want int64
	}{
		{"simple", "123456789", 123456789},
		{"one digit", "0", 0},
		{"large", "9223372036854775807", 9223372036854775807},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseChatID(tt.in)
			if err != nil {
				t.Fatalf("parseChatID(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseChatID(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseChatID_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"alpha", "abc"},
		{"negative", "-1"},
		{"username", "@bot"},
		{"mixed", "12a34"},
		{"with space", "12 34"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseChatID(tt.in)
			if err == nil {
				t.Errorf("parseChatID(%q) should fail", tt.in)
			}
		})
	}
}

func TestAdapter_ChannelAndCapabilities(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	if got := a.Channel(); got != domain.ChannelTGBot {
		t.Errorf("Channel() = %q, want telegram_bot", got)
	}
	caps := a.Capabilities()
	want := []port.Capability{
		port.CapText, port.CapMedia, port.CapReactions,
		port.CapEdit, port.CapDelete, port.CapTyping, port.CapPresence,
		port.CapGroups, port.CapInlineKeyboard, port.CapForum,
		port.CapPayments, port.CapGifts, port.CapNewsletter,
	}
	for _, c := range want {
		if !caps.Supports(c) {
			t.Errorf("TG should support %q", c)
		}
	}
	notSupported := []port.Capability{
		port.CapTemplates, port.CapHandover, port.CapStoryReply,
	}
	for _, c := range notSupported {
		if caps.Supports(c) {
			t.Errorf("TG should NOT support %q", c)
		}
	}
}

func TestAdapter_Send_Text(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	tgid, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123456789", Type: domain.MessageTypeText, Body: "olá",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if tgid == "" {
		t.Error("expected non-empty tgid")
	}
	if len(bot.messages) != 1 {
		t.Fatalf("expected 1 message recorded, got %d", len(bot.messages))
	}
	got := bot.messages[0]
	if got.ChatID != 123456789 {
		t.Errorf("chatID = %d, want 123456789", got.ChatID)
	}
	if got.Text != "olá" {
		t.Errorf("text = %q, want %q", got.Text, "olá")
	}
}

func TestAdapter_Send_InvalidChatID(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "@username", Type: domain.MessageTypeText, Body: "olá",
	})
	if err == nil {
		t.Fatal("expected error for invalid chat_id")
	}
}

func TestAdapter_Send_MediaNotImplemented(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeImage, Body: "img",
	})
	if err == nil {
		t.Fatal("expected error for non-text type")
	}
}

func TestAdapter_Action_Typing_DefaultState(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionTyping,
	})
	if err != nil {
		t.Fatalf("typing: %v", err)
	}
	if len(bot.chatActions) != 1 {
		t.Fatalf("expected 1 chat action, got %d", len(bot.chatActions))
	}
	if bot.chatActions[0].Action != "typing" {
		t.Errorf("action = %q, want default 'typing'", bot.chatActions[0].Action)
	}
}

func TestAdapter_Action_Typing_CustomState(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionTyping, State: "upload_photo",
	})
	if err != nil {
		t.Fatalf("typing: %v", err)
	}
	if bot.chatActions[0].Action != "upload_photo" {
		t.Errorf("action = %q, want 'upload_photo'", bot.chatActions[0].Action)
	}
}

func TestAdapter_Action_StubErrors(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	for _, action := range []port.Action{
		port.ActionReaction, port.ActionEdit, port.ActionRevoke,
		port.ActionMarkRead, port.ActionPresence,
	} {
		_, err := a.Send(context.Background(), port.OutboundRequest{
			TenantID: "t1", Channel: domain.ChannelTGBot,
			PeerID: "123", Action: action,
		})
		if err == nil {
			t.Errorf("TG should reject %q (Phase 4 stub)", action)
		}
	}
}

func TestAdapter_Action_Unknown_ReturnsError(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.Action("nonsense"),
	})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestAdapter_Action_InvalidChatID(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "@bad", Action: port.ActionTyping,
	})
	if err == nil {
		t.Fatal("expected error: action path também passa por parseChatID")
	}
}

func TestAdapter_BotError_Propagates(t *testing.T) {
	t.Parallel()

	want := errors.New("telegram api down")
	bot := &recordingBot{sendMessageErr: want}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeText, Body: "x",
	})
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want wraps %v", err, want)
	}
}

func TestTelegramCapabilities_MatchesMatrix(t *testing.T) {
	t.Parallel()

	caps := TelegramCapabilities()
	want := map[port.Capability]bool{
		port.CapText:           true,
		port.CapMedia:          true,
		port.CapReactions:      true,
		port.CapEdit:           true,
		port.CapDelete:         true,
		port.CapTyping:         true,
		port.CapPresence:       true,
		port.CapGroups:         true,
		port.CapInlineKeyboard: true,
		port.CapForum:          true,
		port.CapPayments:       true,
		port.CapGifts:          true,
		port.CapNewsletter:     true,
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("TelegramCapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
}
