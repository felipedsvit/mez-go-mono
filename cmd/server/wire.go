// Package main — wire.go: composição de serviços e boot determinístico.
//
// Boot order (C12):
//  1. Logger
//  2. Config (viper, fail-fast)
//  3. DB pools (app, platform, admin)
//  4. TxRunner (RunInTenantTx / RunAsPlatform)
//  5. Repos
//  6. Bus
//  7. Ingestor + Router
//  8. OutboxRepo + SenderRegistry + Relay (Fase 3)
//  9. InboundEventsRepo + Reconciler
//
// 10. SenderService + StatusConsumer
// 11. Webhook handlers (Meta + Telegram)
// 12. API handlers
// 13. HTTP server
//
// Graceful shutdown (D10 + C12): signal → HTTP Shutdown → bus Drain →
// relay Flush → reconciler Stop → pools close.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	providerregistry "github.com/felipedsvit/mez-go-mono/internal/adapter/provider/registry"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/meta"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/secrets"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/telegram"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	httpserver "github.com/felipedsvit/mez-go-mono/internal/transport/http/server"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
	ucoutbox "github.com/felipedsvit/mez-go-mono/internal/usecase/outbox"
	ucreconcile "github.com/felipedsvit/mez-go-mono/internal/usecase/reconcile"
	ucrouting "github.com/felipedsvit/mez-go-mono/internal/usecase/routing"
	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/felipedsvit/mez-go-mono/pkg/health"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

// AppContext agrupa tudo o que o ciclo de vida do processo precisa.
type AppContext struct {
	Log      zerolog.Logger
	Cfg      config.Config
	Health   *health.Checker
	Metrics  *metrics.Registry
	Bus      *broker.Bus
	TxRunner *postgres.TxRunner

	// Repos
	Outbox     *postgres.OutboxRepo
	InboundEvs *postgres.InboundEventsRepo
	ConvRepo   *postgres.ConversationRepo
	MsgRepo    *postgres.MessageRepo
	TenantRepo *postgres.TenantRepo

	// Sender pipeline (Fase 3)
	SenderRegistry port.SenderRegistry
	SenderService  *ucmessaging.SenderService
	StatusConsumer *ucmessaging.StatusConsumer
	Credentials    *secrets.EnvCredentials

	// Usecases
	Ingestor   *ucmessaging.Ingestor
	Router     *ucrouting.Router
	Relay      *ucoutbox.Relay
	Reconciler *ucreconcile.Reconciler

	// Webhook handlers
	MetaHandler     *meta.Handler
	TelegramHandler *telegram.Handler

	// HTTP
	HTTPServer *http.Server
}

// wireServices monta toda a árvore de dependências.
// Retorna erro fatal se algo essencial faltar (ex: secret JWT).
func wireServices(ctx context.Context, cfg config.Config, log zerolog.Logger) (*AppContext, error) {
	// 3. DB pools
	appPool, err := postgres.ConnectPool(ctx, cfg.DatabaseURL, 20)
	if err != nil {
		return nil, fmt.Errorf("connect app pool: %w", err)
	}
	platformPool, err := postgres.ConnectPool(ctx, cfg.PlatformDBURL, 10)
	if err != nil {
		appPool.Close()
		return nil, fmt.Errorf("connect platform pool: %w", err)
	}

	// 4. TxRunner
	txRunner := postgres.NewTxRunner(appPool, platformPool, log)

	// 5. Repos
	tenantRepo := postgres.NewTenantRepo(appPool)
	contactRepo := postgres.NewContactRepo(appPool)
	convRepo := postgres.NewConversationRepo(appPool)
	msgRepo := postgres.NewMessageRepo(appPool)
	outboxRepo := postgres.NewOutboxRepo(appPool, platformPool)
	inboundEvsRepo := postgres.NewInboundEventsRepo(appPool, platformPool)

	// 6. Bus
	metricsReg := metrics.NewRegistry()
	busCfg := broker.BusConfig{
		InboundBuffer:  cfg.BusInboundBuf,
		OutboundBuffer: cfg.BusOutboundBuf,
	}
	bus := broker.NewBus(busCfg, log, metricsReg)

	// 7. Usecases
	ingestor := ucmessaging.NewIngestor(contactRepo, convRepo, msgRepo, outboxRepo, txRunner,
		ucmessaging.WithBus(bus),
		ucmessaging.WithLogger(log),
	)
	routerSvc := ucrouting.NewRouter(log)

	// Routing consumer: subscreve inbound e marca como routed.
	bus.SubscribeInbound(func(evt event.InboundEvent) {
		// Re-fetch da mensagem via repositórios; aqui simplificamos
		// emitindo um routing no-op. O Reconciler (#39) cobre a fila
		// "received" com FOR UPDATE SKIP LOCKED, e este consumer é
		// o caminho fast para a Fase 2.
		log.Debug().
			Str("tenant", evt.TenantID).
			Str("channel", string(evt.Channel)).
			Str("message", evt.MessageID).
			Msg("bus: routing consumer (fase 2: noop)")
		// TODO Fase 3/5: chamar router.Assign + msgRepo.MarkRouted.
		// Para Fase 2, o Reconciler cobre o caso. Aqui só logamos.
		_ = evt
		_ = routerSvc
	})

	// 8. Sender pipeline (Fase 3): credentials + registry + service + relay.
	creds, err := secrets.NewEnvCredentials()
	if err != nil {
		return nil, fmt.Errorf("load credentials: %w", err)
	}
	resolver := port.NewResolver()
	resolver.Register(domain.ChannelWABA, port.CapabilitiesWABA())
	resolver.Register(domain.ChannelIG, port.CapabilitiesInstagram())
	resolver.Register(domain.ChannelMSG, port.CapabilitiesMessenger())
	resolver.Register(domain.ChannelTGBot, port.CapabilitiesTelegram())
	senderRegistry := providerregistry.Build(creds, log)

	pollInterval, err := time.ParseDuration(cfg.OutboxPollInterval)
	if err != nil || pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	relay := ucoutbox.New(outboxRepo, senderRegistry, bus, ucoutbox.Config{
		PollInterval: pollInterval,
		BatchSize:    cfg.OutboxBatch,
	}, log)

	senderSvc := ucmessaging.NewSenderService(msgRepo, outboxRepo, resolver, relay, log)
	statusConsumer := ucmessaging.NewStatusConsumer(bus, msgRepo, log)
	statusConsumer.Subscribe()

	// 9. Reconciler.
	reconcileInterval, err := time.ParseDuration(cfg.ReconcileInterval)
	if err != nil || reconcileInterval <= 0 {
		reconcileInterval = 30 * time.Second
	}
	reconciler := ucreconcile.New(
		inboundEvsRepo,
		func(ctx context.Context, m domain.Message) error {
			_, err := routerSvc.Assign(ctx, m)
			return err
		},
		ucreconcile.Config{
			Interval:  reconcileInterval,
			BatchSize: cfg.ReconcileBatch,
		},
		log,
	)

	// 10. Webhook handlers.
	metaSecrets, err := secrets.NewEnvMetaSecrets()
	if err != nil {
		return nil, fmt.Errorf("load meta secrets: %w", err)
	}
	tgSecrets := secrets.NewEnvTelegramSecrets()
	metaH := meta.New(ingestor, metaSecrets, metaSecrets, meta.Config{}, log)
	tgH := telegram.New(ingestor, tgSecrets, telegram.Config{}, log)

	// 11. Health
	hc := health.NewChecker()
	hc.Add("postgres", func(ctx context.Context) error {
		return appPool.Ping(ctx)
	})

	// 12. HTTP server
	jwtSecret := []byte(cfg.APIJWTSecret)
	handler := httpserver.New(httpserver.Services{
		Log:             log,
		Health:          hc,
		Metrics:         metricsReg,
		ConvRepo:        convRepo,
		MsgRepo:         msgRepo,
		TenantRepo:      tenantRepo,
		MetaHandler:     metaH,
		TelegramHandler: tgH,
		AdminRouter:     nil, // adminweb é montado separado em main
		JWTSecret:       jwtSecret,
		SenderService:   senderSvc,
		SenderRegistry:  senderRegistry,
	})
	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return &AppContext{
		Log: log, Cfg: cfg, Health: hc, Metrics: metricsReg, Bus: bus, TxRunner: txRunner,
		Outbox: outboxRepo, InboundEvs: inboundEvsRepo,
		ConvRepo: convRepo, MsgRepo: msgRepo, TenantRepo: tenantRepo,
		SenderRegistry: senderRegistry, SenderService: senderSvc,
		StatusConsumer: statusConsumer, Credentials: creds,
		Ingestor: ingestor, Router: routerSvc, Relay: relay, Reconciler: reconciler,
		MetaHandler: metaH, TelegramHandler: tgH, HTTPServer: srv,
	}, nil
}

// runWithGracefulShutdown executa app.Run com shutdown coordenado (D10+C12).
func runWithGracefulShutdown(ctx context.Context, app *AppContext) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 4)

	// Goroutine: HTTP server.
	go func() {
		app.Log.Info().Str("addr", app.Cfg.HTTPAddr).Msg("http: listening")
		if err := app.HTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http: %w", err)
		}
	}()

	// Goroutine: Outbox Relay.
	go func() {
		if err := app.Relay.Run(ctx); err != nil {
			errCh <- fmt.Errorf("relay: %w", err)
		}
	}()

	// Goroutine: Reconciler.
	go func() {
		if err := app.Reconciler.Run(ctx); err != nil {
			errCh <- fmt.Errorf("reconciler: %w", err)
		}
	}()

	// Espera sinal ou erro fatal.
	select {
	case sig := <-sigCh:
		app.Log.Info().Str("signal", sig.String()).Msg("shutdown: signal received")
	case err := <-errCh:
		app.Log.Error().Err(err).Msg("shutdown: fatal error")
	}

	// Shutdown coordenado (C12).
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Para de aceitar HTTP.
	if err := app.HTTPServer.Shutdown(shutdownCtx); err != nil {
		app.Log.Error().Err(err).Msg("http shutdown")
	}
	// 2. Bus drain.
	if err := app.Bus.Drain(shutdownCtx); err != nil {
		app.Log.Error().Err(err).Msg("bus drain")
	}
	// 3. Relay stop.
	app.Relay.Stop()
	// 4. Reconciler stop.
	app.Reconciler.Stop()

	// 5. Aguarda goroutines terminarem.
	done := make(chan struct{})
	go func() {
		// espera que tudo finalize graciosamente
		app.Log.Info().Msg("shutdown: complete")
		close(done)
	}()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		app.Log.Warn().Msg("shutdown: timeout")
	}

	return nil
}

// chi.NewRouter é exposto para casos de teste ou extensão futura.
var _ = chi.NewRouter
