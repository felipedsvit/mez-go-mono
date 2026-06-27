// Package telegram_bot — testes do adapter Telegram Bot API.
//
// Cobre:
//   - text message → BotClient.SendMessage com chatID parseado
//   - typing action → BotClient.SendChatAction
//   - reaction/edit/revoke/mark_read/presence → executam ações no client
//   - action desconhecida → erro
//   - chat_id inválido → erro (parseChatID)
//   - type image/video/audio/document/sticker/location → BotClient correspondente
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
	mu sync.Mutex

	messages       []recordedMessage
	photos         []recordedPhoto
	stickers       []recordedSticker
	locations      []recordedLocation
	edits          []recordedEdit
	deletes        []recordedDelete
	reactions      []recordedReaction
	chatActions    []recordedAction

	sendMessageErr error
	sendPhotoErr   error
	sendStickerErr error
	sendLocationErr error
	editErr        error
	deleteErr      error
	reactionErr    error
	sendActionErr  error
}

type recordedMessage struct {
	ChatID   int64
	Text     string
	Markup   ReplyMarkup
}
type recordedPhoto struct {
	ChatID  int64
	URL     string
	Caption string
	Markup  ReplyMarkup
}
type recordedSticker struct {
	ChatID int64
	FileID string
}
type recordedLocation struct {
	ChatID    int64
	Latitude  float64
	Longitude float64
}
type recordedEdit struct {
	ChatID    int64
	MessageID string
	NewText   string
	Markup    ReplyMarkup
}
type recordedDelete struct {
	ChatID    int64
	MessageID string
}
type recordedReaction struct {
	ChatID    int64
	MessageID string
	Emoji     string
}
type recordedAction struct {
	ChatID int64
	Action string
}

func (r *recordingBot) SendMessage(_ context.Context, chatID int64, text string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendMessageErr != nil {
		return "", r.sendMessageErr
	}
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: text, Markup: rm})
	return "tg-msg-id-1", nil
}
func (r *recordingBot) SendPhoto(_ context.Context, chatID int64, url, caption string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendPhotoErr != nil {
		return "", r.sendPhotoErr
	}
	r.photos = append(r.photos, recordedPhoto{ChatID: chatID, URL: url, Caption: caption, Markup: rm})
	return "tg-photo-1", nil
}
func (r *recordingBot) SendVideo(_ context.Context, chatID int64, url, caption string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: "[video] " + url + " " + caption})
	return "tg-vid-1", nil
}
func (r *recordingBot) SendVoice(_ context.Context, chatID int64, url, caption string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: "[voice] " + url + " " + caption})
	return "tg-voice-1", nil
}
func (r *recordingBot) SendAudio(_ context.Context, chatID int64, url, caption string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: "[audio] " + url + " " + caption})
	return "tg-audio-1", nil
}
func (r *recordingBot) SendDocument(_ context.Context, chatID int64, url, caption, filename string, rm ReplyMarkup) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: "[doc] " + url + " " + caption + " " + filename})
	return "tg-doc-1", nil
}
func (r *recordingBot) SendSticker(_ context.Context, chatID int64, fileID string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendStickerErr != nil {
		return "", r.sendStickerErr
	}
	r.stickers = append(r.stickers, recordedSticker{ChatID: chatID, FileID: fileID})
	return "tg-sticker-1", nil
}
func (r *recordingBot) SendLocation(_ context.Context, chatID int64, lat, lng float64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sendLocationErr != nil {
		return "", r.sendLocationErr
	}
	r.locations = append(r.locations, recordedLocation{ChatID: chatID, Latitude: lat, Longitude: lng})
	return "tg-loc-1", nil
}
func (r *recordingBot) SendInvoice(_ context.Context, chatID int64, payload Invoice) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, recordedMessage{ChatID: chatID, Text: "[invoice] " + payload.Title})
	return "tg-invoice-1", nil
}
func (r *recordingBot) EditMessageText(_ context.Context, chatID int64, msgID, text string, rm ReplyMarkup) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.editErr != nil {
		return r.editErr
	}
	r.edits = append(r.edits, recordedEdit{ChatID: chatID, MessageID: msgID, NewText: text, Markup: rm})
	return nil
}
func (r *recordingBot) DeleteMessage(_ context.Context, chatID int64, msgID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deletes = append(r.deletes, recordedDelete{ChatID: chatID, MessageID: msgID})
	return nil
}
func (r *recordingBot) SetMessageReaction(_ context.Context, chatID int64, msgID, emoji string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reactionErr != nil {
		return r.reactionErr
	}
	r.reactions = append(r.reactions, recordedReaction{ChatID: chatID, MessageID: msgID, Emoji: emoji})
	return nil
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
		port.CapGifts, port.CapNewsletter,
	}
	for _, c := range want {
		if !caps.Supports(c) {
			t.Errorf("TG should support %q", c)
		}
	}
	notSupported := []port.Capability{
		port.CapTemplates, port.CapHandover, port.CapStoryReply, port.CapPayments,
	}
	for _, c := range notSupported {
		if caps.Supports(c) {
			t.Errorf("TG should NOT support %q", c)
		}
	}
}

func TestAdapter_Capabilities_PaymentsToggle(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	if a.Capabilities().Supports(port.CapPayments) {
		t.Fatal("expected payments to be off by default")
	}
	b := New(domain.TenantID("t1"), &recordingBot{}, zerolog.Nop(), WithPayments(true))
	if !b.Capabilities().Supports(port.CapPayments) {
		t.Fatal("expected payments to be on when WithPayments(true)")
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

func TestAdapter_Send_Text_WithInlineKeyboard(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeText, Body: "escolha:",
		Metadata: map[string]any{
			"inline_keyboard": []any{
				[]any{
					map[string]any{"text": "Sim", "callback_data": "yes"},
					map[string]any{"text": "Não", "callback_data": "no"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	kb, ok := bot.messages[0].Markup.(InlineKeyboard)
	if !ok {
		t.Fatalf("expected InlineKeyboard, got %T", bot.messages[0].Markup)
	}
	if len(kb.Buttons) != 1 || len(kb.Buttons[0]) != 2 {
		t.Errorf("inline keyboard shape unexpected: %+v", kb)
	}
}

func TestAdapter_Send_Image(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeImage, Body: "olha isso",
		Metadata: map[string]any{"media_url": "https://example.com/x.png"},
	})
	if err != nil {
		t.Fatalf("Send image: %v", err)
	}
	if len(bot.photos) != 1 {
		t.Fatalf("expected 1 photo, got %d", len(bot.photos))
	}
	if bot.photos[0].URL != "https://example.com/x.png" {
		t.Errorf("url = %q", bot.photos[0].URL)
	}
}

func TestAdapter_Send_Sticker(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeSticker,
		Metadata: map[string]any{"sticker_file_id": "CAACAgI..."},
	})
	if err != nil {
		t.Fatalf("Send sticker: %v", err)
	}
	if len(bot.stickers) != 1 || bot.stickers[0].FileID != "CAACAgI..." {
		t.Errorf("sticker not recorded: %+v", bot.stickers)
	}
}

func TestAdapter_Send_Location(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Type: domain.MessageTypeLocation,
		Metadata: map[string]any{"latitude": -23.55, "longitude": -46.63},
	})
	if err != nil {
		t.Fatalf("Send location: %v", err)
	}
	if len(bot.locations) != 1 {
		t.Fatalf("expected 1 location, got %d", len(bot.locations))
	}
	if bot.locations[0].Latitude != -23.55 || bot.locations[0].Longitude != -46.63 {
		t.Errorf("coords = %+v", bot.locations[0])
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

func TestAdapter_Action_Reaction(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionReaction,
		TargetProviderID: "tg-msg-1", ReactionEmoji: "👍",
	})
	if err != nil {
		t.Fatalf("reaction: %v", err)
	}
	if len(bot.reactions) != 1 || bot.reactions[0].Emoji != "👍" {
		t.Errorf("reaction not recorded: %+v", bot.reactions)
	}
}

func TestAdapter_Action_Edit(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionEdit,
		TargetProviderID: "tg-msg-1", NewBody: "editado",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if len(bot.edits) != 1 || bot.edits[0].NewText != "editado" {
		t.Errorf("edit not recorded: %+v", bot.edits)
	}
}

func TestAdapter_Action_Revoke(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionRevoke,
		TargetProviderID: "tg-msg-1",
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(bot.deletes) != 1 || bot.deletes[0].MessageID != "tg-msg-1" {
		t.Errorf("delete not recorded: %+v", bot.deletes)
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

func TestAdapter_Action_Presence(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionPresence, State: "typing",
	})
	if err != nil {
		t.Fatalf("presence: %v", err)
	}
	if len(bot.chatActions) != 1 {
		t.Errorf("expected 1 chat action, got %d", len(bot.chatActions))
	}
}

func TestAdapter_Action_MarkRead(t *testing.T) {
	t.Parallel()

	bot := &recordingBot{}
	a := newTestAdapter(bot)
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionMarkRead,
	})
	if err != nil {
		t.Fatalf("mark_read: %v", err)
	}
}

func TestAdapter_Action_Reaction_RequiresTarget(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionReaction, ReactionEmoji: "👍",
	})
	if err == nil {
		t.Fatal("expected error when target_provider_id missing")
	}
}

func TestAdapter_Action_Edit_RequiresTarget(t *testing.T) {
	t.Parallel()

	a := newTestAdapter(&recordingBot{})
	_, err := a.Send(context.Background(), port.OutboundRequest{
		TenantID: "t1", Channel: domain.ChannelTGBot,
		PeerID: "123", Action: port.ActionEdit, NewBody: "x",
	})
	if err == nil {
		t.Fatal("expected error when target_provider_id missing")
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
		port.CapGifts:          true,
		port.CapNewsletter:     true,
		// payments é gated — não está na matriz default.
	}
	for c, expected := range want {
		if caps.Supports(c) != expected {
			t.Errorf("TelegramCapabilities: %q = %v, want %v", c, caps.Supports(c), expected)
		}
	}
	if caps.Supports(port.CapPayments) {
		t.Error("TelegramCapabilities should NOT have payments by default")
	}
}

func TestBuildReplyMarkup(t *testing.T) {
	t.Parallel()

	t.Run("nil", func(t *testing.T) {
		t.Parallel()
		if rm := buildReplyMarkup(nil); rm != nil {
			t.Errorf("expected nil, got %v", rm)
		}
	})
	t.Run("no inline_keyboard", func(t *testing.T) {
		t.Parallel()
		if rm := buildReplyMarkup(map[string]any{"foo": "bar"}); rm != nil {
			t.Errorf("expected nil, got %v", rm)
		}
	})
	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		rm := buildReplyMarkup(map[string]any{
			"inline_keyboard": []any{
				[]any{
					map[string]any{"text": "A", "callback_data": "a"},
					map[string]any{"text": "B", "url": "https://b.example"},
				},
			},
		})
		kb, ok := rm.(InlineKeyboard)
		if !ok {
			t.Fatalf("expected InlineKeyboard, got %T", rm)
		}
		if len(kb.Buttons) != 1 || len(kb.Buttons[0]) != 2 {
			t.Errorf("buttons: %+v", kb.Buttons)
		}
		if kb.Buttons[0][0].CallbackData != "a" || kb.Buttons[0][1].URL != "https://b.example" {
			t.Errorf("button attrs: %+v", kb.Buttons[0])
		}
	})
	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		rm := buildReplyMarkup(map[string]any{
			"inline_keyboard": "not an array",
		})
		if rm != nil {
			t.Errorf("expected nil for malformed, got %v", rm)
		}
	})
}
