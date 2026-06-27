// Package registry — testes do wire-up de providers (Fase 7 #52).
//
// Cobre:
//   - Build() registra as 4 factories default (WABA/IG/MSG/TG);
//   - cada factory extrai e parseia seu JSON de credenciais específico;
//   - erro do CredentialsResolver propaga;
//   - JSON inválido é rejeitado com erro;
//   - whatsmeow só é registrado quando BuildOpts.Whatsmeow != nil;
//   - telegramFactory delega ao stub client em teste (sem rede).
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// stubResolver devolve bytes JSON por (channel) ou erro.
type stubResolver struct {
	byChannel map[domain.Channel][]byte
	err       error
}

func (s *stubResolver) ResolveCredentials(_ context.Context, _ domain.TenantID, ch domain.Channel) ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	b, ok := s.byChannel[ch]
	if !ok {
		return nil, port.ErrCredentialsNotFound
	}
	return b, nil
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestBuild_RegistersAllDefaultFactories(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{
			domain.ChannelWABA:  mustJSON(t, wabaCredentials{PhoneNumberID: "p", AccessToken: "a"}),
			domain.ChannelIG:    mustJSON(t, metaPageCredentials{PageID: "ig", AccessToken: "a"}),
			domain.ChannelMSG:   mustJSON(t, metaPageCredentials{PageID: "msg", AccessToken: "a"}),
			domain.ChannelTGBot: mustJSON(t, telegramCredentials{BotToken: "t"}),
		},
	}

	reg := Build(resolver, zerolog.Nop(), BuildOpts{})
	chs := reg.Channels()
	got := make(map[domain.Channel]bool, len(chs))
	for _, c := range chs {
		got[c] = true
	}
	for _, want := range []domain.Channel{
		domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG, domain.ChannelTGBot,
	} {
		if !got[want] {
			t.Errorf("expected channel %q to be registered", want)
		}
	}
	// whatsmeow não foi passado → não deve estar registrado.
	if got[domain.ChannelWAWeb] {
		t.Error("whatsmeow should NOT be registered when BuildOpts.Whatsmeow is nil")
	}
}

func TestBuild_WABAFactory_ParsesCredentials(t *testing.T) {
	t.Parallel()

	want := wabaCredentials{PhoneNumberID: "1234567890", AccessToken: "EAAB..."}
	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{domain.ChannelWABA: mustJSON(t, want)},
	}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	sender, err := reg.Get(context.Background(), "t1", domain.ChannelWABA)
	if err != nil {
		t.Fatalf("Get WABA: %v", err)
	}
	if sender.Channel() != domain.ChannelWABA {
		t.Errorf("sender.Channel() = %q, want waba", sender.Channel())
	}
}

func TestBuild_InstagramFactory_ParsesCredentials(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{
			domain.ChannelIG: mustJSON(t, metaPageCredentials{PageID: "ig-page", AccessToken: "tok"}),
		},
	}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	sender, err := reg.Get(context.Background(), "t1", domain.ChannelIG)
	if err != nil {
		t.Fatalf("Get IG: %v", err)
	}
	if sender.Channel() != domain.ChannelIG {
		t.Errorf("sender.Channel() = %q, want instagram", sender.Channel())
	}
}

func TestBuild_MessengerFactory_ParsesCredentials(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{
			domain.ChannelMSG: mustJSON(t, metaPageCredentials{PageID: "msg-page", AccessToken: "tok"}),
		},
	}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	sender, err := reg.Get(context.Background(), "t1", domain.ChannelMSG)
	if err != nil {
		t.Fatalf("Get MSG: %v", err)
	}
	if sender.Channel() != domain.ChannelMSG {
		t.Errorf("sender.Channel() = %q, want messenger", sender.Channel())
	}
}

func TestBuild_TelegramFactory_ParsesCredentials(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{
			domain.ChannelTGBot: mustJSON(t, telegramCredentials{BotToken: "123:abc"}),
		},
	}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	sender, err := reg.Get(context.Background(), "t1", domain.ChannelTGBot)
	if err != nil {
		t.Fatalf("Get TG: %v", err)
	}
	if sender.Channel() != domain.ChannelTGBot {
		t.Errorf("sender.Channel() = %q, want telegram_bot", sender.Channel())
	}
}

func TestBuild_Factory_ResolverError_Propagates(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{err: errors.New("keyring down")}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	for _, ch := range []domain.Channel{
		domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG, domain.ChannelTGBot,
	} {
		_, err := reg.Get(context.Background(), "t1", ch)
		if err == nil {
			t.Errorf("channel %q: expected error from resolver", ch)
		}
	}
}

func TestBuild_Factory_MissingCredentials_Propagates(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{byChannel: map[domain.Channel][]byte{}}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	_, err := reg.Get(context.Background(), "t1", domain.ChannelWABA)
	if !errors.Is(err, port.ErrCredentialsNotFound) {
		t.Errorf("err = %v, want ErrCredentialsNotFound", err)
	}
}

func TestBuild_Factory_InvalidJSON_PropagatesParseError(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{
		byChannel: map[domain.Channel][]byte{domain.ChannelWABA: []byte("{not-json")},
	}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	_, err := reg.Get(context.Background(), "t1", domain.ChannelWABA)
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	// Erro deve carregar contexto (canal + ação) para diagnóstico.
	msg := err.Error()
	if !contains(msg, "waba") || !contains(msg, "parse") {
		t.Errorf("err message = %q, want contains 'waba' and 'parse'", msg)
	}
}

func TestBuild_Health_AggregatesFactoryResults(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{err: errors.New("all down")}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	health := reg.Health(context.Background(), "t1")
	for _, ch := range []domain.Channel{
		domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG, domain.ChannelTGBot,
	} {
		if got := health[ch]; got == nil {
			t.Errorf("channel %q: expected error in health map", ch)
		}
	}
}

func TestBuild_WithoutWhatsmeow_NoWAWebChannel(t *testing.T) {
	t.Parallel()

	resolver := &stubResolver{byChannel: map[domain.Channel][]byte{}}
	reg := Build(resolver, zerolog.Nop(), BuildOpts{})

	_, err := reg.Get(context.Background(), "t1", domain.ChannelWAWeb)
	if !errors.Is(err, port.ErrSenderNotRegistered) {
		t.Errorf("err = %v, want ErrSenderNotRegistered when whatsmeow not configured", err)
	}
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
