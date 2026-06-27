// Package registry — boot.go: wire-up do SenderRegistry (Fase 3 #52 + Fase 4 #67 + Fase 7).
//
// Fase 7: credenciais são resolvidas via port.CredentialsResolver (Keyring em
// produção). Cada factory chama ResolveCredentials e faz Unmarshal do JSON de
// retorno na struct canal-específica. O formato dos bytes é contrato entre
// usecase/secrets.Keyring (SetCredentials) e as factories aqui.
package registry

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"

	sendermem "github.com/felipedsvit/mez-go-mono/internal/adapter/sender/memory"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/instagram"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/messenger"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/telegram_bot"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/waba"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/whatsmeow"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// WhatsmeowDeps agrupa as dependências do whatsmeow para o Manager (Fase 4).
type WhatsmeowDeps struct {
	Manager *whatsmeow.Manager
}

// BuildOpts configura o Build.
type BuildOpts struct {
	Whatsmeow *WhatsmeowDeps
}

// Build constrói o SenderRegistry e registra factories para os 5 canais.
// resolver é o port.CredentialsResolver — em produção é o Keyring (Fase 7);
// em dev/test pode ser qualquer implementação.
//
// Issue #121: usa internal/adapter/sender/memory ao invés de
// port.MemorySenderRegistry (que foi removido do port).
func Build(resolver port.CredentialsResolver, log zerolog.Logger, opts BuildOpts) port.SenderRegistry {
	reg := sendermem.New(log, 0)

	reg.Register(domain.ChannelWABA, wabaFactory(resolver, log))
	reg.Register(domain.ChannelIG, instagramFactory(resolver, log))
	reg.Register(domain.ChannelMSG, messengerFactory(resolver, log))
	reg.Register(domain.ChannelTGBot, telegramFactory(resolver, log))

	if opts.Whatsmeow != nil && opts.Whatsmeow.Manager != nil {
		reg.Register(domain.ChannelWAWeb, whatsmeowFactory(opts.Whatsmeow.Manager, log))
	}

	return reg
}

// wabaCredentials é o formato JSON armazenado no Keyring para WABA.
type wabaCredentials struct {
	PhoneNumberID string `json:"phone_number_id"`
	AccessToken   string `json:"access_token"`
}

func wabaFactory(resolver port.CredentialsResolver, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		raw, err := resolver.ResolveCredentials(ctx, tenantID, domain.ChannelWABA)
		if err != nil {
			return nil, fmt.Errorf("waba credentials: %w", err)
		}
		var c wabaCredentials
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("waba credentials parse: %w", err)
		}
		client := waba.NewClient("", "", c.PhoneNumberID, c.AccessToken)
		return waba.New(tenantID, client, log), nil
	}
}

// metaPageCredentials é o formato JSON armazenado no Keyring para IG e MSG.
type metaPageCredentials struct {
	PageID      string `json:"page_id"`
	AccessToken string `json:"access_token"`
}

func instagramFactory(resolver port.CredentialsResolver, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		raw, err := resolver.ResolveCredentials(ctx, tenantID, domain.ChannelIG)
		if err != nil {
			return nil, fmt.Errorf("instagram credentials: %w", err)
		}
		var c metaPageCredentials
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("instagram credentials parse: %w", err)
		}
		client := instagram.NewClient("", "", c.PageID, c.AccessToken)
		return instagram.New(tenantID, client, log), nil
	}
}

func messengerFactory(resolver port.CredentialsResolver, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		raw, err := resolver.ResolveCredentials(ctx, tenantID, domain.ChannelMSG)
		if err != nil {
			return nil, fmt.Errorf("messenger credentials: %w", err)
		}
		var c metaPageCredentials
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("messenger credentials parse: %w", err)
		}
		client := messenger.NewClient("", "", c.AccessToken)
		return messenger.New(tenantID, client, log), nil
	}
}

// telegramCredentials é o formato JSON armazenado no Keyring para TG Bot.
type telegramCredentials struct {
	BotToken string `json:"bot_token"`
}

func telegramFactory(resolver port.CredentialsResolver, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		raw, err := resolver.ResolveCredentials(ctx, tenantID, domain.ChannelTGBot)
		if err != nil {
			return nil, fmt.Errorf("telegram credentials: %w", err)
		}
		var c telegramCredentials
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("telegram credentials parse: %w", err)
		}
		client := &stubBotClient{token: c.BotToken}
		return telegram_bot.New(tenantID, client, log), nil
	}
}

// whatsmeowFactory delega ao Manager (per-tenant lazy).
func whatsmeowFactory(mgr *whatsmeow.Manager, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		adapter, err := mgr.GetOrCreate(ctx, tenantID, func(_ context.Context, _ domain.TenantID) (whatsmeow.Client, error) {
			return whatsmeow.NewStubClient(string(tenantID), log), nil
		})
		if err != nil {
			return nil, err
		}
		return adapter, nil
	}
}

// stubBotClient implementa telegram_bot.BotClient sem chamar o Bot API real.
type stubBotClient struct {
	token string
}

func (s *stubBotClient) SendMessage(_ context.Context, chatID int64, _ string) (string, error) {
	return fmt.Sprintf("tg-stub-%d", chatID), nil
}

func (s *stubBotClient) SendChatAction(_ context.Context, _ int64, _ string) error {
	return nil
}
