// Package registry — boot.go: wire-up do SenderRegistry (Fase 3 #52).
//
// Carrega credenciais via EnvCredentials e cria factories para cada canal
// registrado. As factories são passadas para o port.MemorySenderRegistry
// (lazy init per-tenant).
package registry

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/instagram"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/messenger"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/telegram_bot"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/waba"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/secrets"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// Build constrói o SenderRegistry e registra factories para os 4 canais
// implementados na Fase 3 (WABA/IG/MSG/TG). whatsmeow é deliberadamente
// omitido (Phase 4).
func Build(creds *secrets.EnvCredentials, log zerolog.Logger) port.SenderRegistry {
	reg := port.NewMemorySenderRegistry(log, 0)

	reg.Register(domain.ChannelWABA, wabaFactory(creds, log))
	reg.Register(domain.ChannelIG, instagramFactory(creds, log))
	reg.Register(domain.ChannelMSG, messengerFactory(creds, log))
	reg.Register(domain.ChannelTGBot, telegramFactory(creds, log))

	return reg
}

func wabaFactory(creds *secrets.EnvCredentials, log zerolog.Logger) port.SenderFactory {
	return func(_ context.Context, tenantID domain.TenantID) (port.Sender, error) {
		c, err := creds.ResolveWABA(tenantID)
		if err != nil {
			return nil, fmt.Errorf("waba: %w", err)
		}
		client := waba.NewClient("", "", c.PhoneNumberID, c.AccessToken)
		return waba.New(tenantID, client, log), nil
	}
}

func instagramFactory(creds *secrets.EnvCredentials, log zerolog.Logger) port.SenderFactory {
	return func(_ context.Context, tenantID domain.TenantID) (port.Sender, error) {
		c, err := creds.ResolveInstagram(tenantID)
		if err != nil {
			return nil, fmt.Errorf("instagram: %w", err)
		}
		client := instagram.NewClient("", "", c.PageID, c.AccessToken)
		return instagram.New(tenantID, client, log), nil
	}
}

func messengerFactory(creds *secrets.EnvCredentials, log zerolog.Logger) port.SenderFactory {
	return func(_ context.Context, tenantID domain.TenantID) (port.Sender, error) {
		c, err := creds.ResolveMessenger(tenantID)
		if err != nil {
			return nil, fmt.Errorf("messenger: %w", err)
		}
		client := messenger.NewClient("", "", c.AccessToken)
		return messenger.New(tenantID, client, log), nil
	}
}

func telegramFactory(creds *secrets.EnvCredentials, log zerolog.Logger) port.SenderFactory {
	return func(_ context.Context, tenantID domain.TenantID) (port.Sender, error) {
		c, err := creds.ResolveTelegram(tenantID)
		if err != nil {
			return nil, fmt.Errorf("telegram: %w", err)
		}
		// Phase 3: cliente stub (não chama real Bot API). Phase 4 wire ao SDK.
		client := &stubBotClient{token: c.BotToken}
		return telegram_bot.New(tenantID, client, log), nil
	}
}

// stubBotClient implementa telegram_bot.BotClient sem chamar o Bot API real.
// Usado em Fase 3 para que o pipeline funcione end-to-end sem credenciais
// reais. Phase 4 substitui por *tgbot.Bot.
type stubBotClient struct {
	token string
}

func (s *stubBotClient) SendMessage(_ context.Context, chatID int64, _ string) (string, error) {
	return fmt.Sprintf("tg-stub-%d", chatID), nil
}

func (s *stubBotClient) SendChatAction(_ context.Context, _ int64, _ string) error {
	return nil
}
