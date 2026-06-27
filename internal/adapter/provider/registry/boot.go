// Package registry — boot.go: wire-up do SenderRegistry (Fase 3 #52 + Fase 4 #67).
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
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/whatsmeow"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/secrets"
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

// Build constrói o SenderRegistry e registra factories para os 5 canais
// implementados nas Fases 3+4 (WABA/IG/MSG/TG/WhatsMeow).
func Build(creds *secrets.EnvCredentials, log zerolog.Logger, opts BuildOpts) port.SenderRegistry {
	reg := port.NewMemorySenderRegistry(log, 0)

	reg.Register(domain.ChannelWABA, wabaFactory(creds, log))
	reg.Register(domain.ChannelIG, instagramFactory(creds, log))
	reg.Register(domain.ChannelMSG, messengerFactory(creds, log))
	reg.Register(domain.ChannelTGBot, telegramFactory(creds, log))

	// Fase 4: factory whatsmeow via Manager.
	if opts.Whatsmeow != nil && opts.Whatsmeow.Manager != nil {
		reg.Register(domain.ChannelWAWeb, whatsmeowFactory(opts.Whatsmeow.Manager, log))
	}

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
		client := &stubBotClient{token: c.BotToken}
		return telegram_bot.New(tenantID, client, log), nil
	}
}

// whatsmeowFactory retorna uma factory que delega ao Manager (per-tenant lazy).
func whatsmeowFactory(mgr *whatsmeow.Manager, log zerolog.Logger) port.SenderFactory {
	return func(ctx context.Context, tenantID domain.TenantID) (port.Sender, error) {
		// Fase 4: usamos o stub client por padrão (sem credenciais reais).
		// production: substitui por *whatsmeow.Client com sqlstore pareado.
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
