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
//  8. OutboxRepo + SenderRegistry + Relay
//  9. InboundEventsRepo + Reconciler
// 10. SenderService + StatusConsumer
// 11. Whatsmeow Manager
// 12. Webhook handlers (Meta + Telegram)
// 13. S3 store (Fase 6)
// 14. Backup Service (Fase 6)
// 15. Admin DB + adminweb.Server (Fase 1) + CSRF middleware (Fase 6)
// 16. API handlers
// 17. HTTP server (com adminweb montado em /admin)
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
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/auth/argon2"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	memcache "github.com/felipedsvit/mez-go-mono/internal/adapter/cache/memory"
	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/instagram"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/messenger"
	providerregistry "github.com/felipedsvit/mez-go-mono/internal/adapter/provider/registry"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/telegram_bot"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/waba"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/provider/whatsmeow"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	adminrepo "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres/admin"
	s3store "github.com/felipedsvit/mez-go-mono/internal/adapter/storage/s3"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/meta"
	webhooksecrets "github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/secrets"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/webhook/telegram"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	adminweb "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb"
	adminmw "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	httpserver "github.com/felipedsvit/mez-go-mono/internal/transport/http/server"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucauth "github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
	ucmessaging "github.com/felipedsvit/mez-go-mono/internal/usecase/messaging"
	ucoutbox "github.com/felipedsvit/mez-go-mono/internal/usecase/outbox"
	ucreconcile "github.com/felipedsvit/mez-go-mono/internal/usecase/reconcile"
	ucrouting "github.com/felipedsvit/mez-go-mono/internal/usecase/routing"
	ucsecrets "github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
	ucsettings "github.com/felipedsvit/mez-go-mono/internal/usecase/settings"
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
	// ListService é o use case de leitura (issue #126).
	ListService    *ucmessaging.ListService
	StatusConsumer *ucmessaging.StatusConsumer

	// Fase 7: envelope encryption
	Keyring  *ucsecrets.Keyring
	CredsRepo *postgres.ChannelCredentialsRepo

	// Usecases
	Ingestor   *ucmessaging.Ingestor
	Router     *ucrouting.Router
	Relay      *ucoutbox.Relay
	Reconciler *ucreconcile.Reconciler

	// Fase 6: backup/restore/reset
	BackupService *ucbackup.Service
	Store         *s3store.Store
	AdminVerifier ucbackup.AdminVerifier

	// Admin (Fase 1) + S3 helpers
	AdminServer *adminweb.Server

	// Webhook handlers
	MetaHandler     *meta.Handler
	TelegramHandler *telegram.Handler

	// HTTP
	HTTPServer *http.Server
}

// wireServices monta toda a árvore de dependências.
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
	outboxRepo := postgres.NewOutboxRepo(appPool, platformPool, postgres.NewTenantEnumerator(platformPool))
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
	routerSvc := ucrouting.NewRouter(txRunner, convRepo, log)

	bus.SubscribeInbound(func(evt event.InboundEvent) {
		log.Debug().
			Str("tenant", evt.TenantID).
			Str("channel", string(evt.Channel)).
			Str("message", evt.MessageID).
			Msg("bus: routing consumer (fase 2: noop)")
		_ = evt
		_ = routerSvc
	})

	// 8. Sealer + ChannelCredentialsRepo + Keyring (Fase 7 #88/#90/#91)
	localSealer, err := adaptercrypto.NewLocalSealer(cfg.MasterKey)
	if err != nil {
		return nil, fmt.Errorf("local sealer: %w", err)
	}
	credsRepo := postgres.NewChannelCredentialsRepo(appPool, platformPool, txRunner)
	keyring := ucsecrets.New(credsRepo, localSealer, log)

	// Capability resolver — cada adapter exporta sua matriz. Issue #120:
	// matriz concreta de cada canal é responsabilidade do adapter, não do port.
	resolver := port.NewResolver()
	resolver.Register(domain.ChannelWABA, waba.WABACapabilities())
	resolver.Register(domain.ChannelIG, instagram.InstagramCapabilities())
	resolver.Register(domain.ChannelMSG, messenger.MessengerCapabilities())
	resolver.Register(domain.ChannelTGBot, telegram_bot.TelegramCapabilities())
	resolver.Register(domain.ChannelWAWeb, whatsmeow.WhatsmeowCapabilities())

	// 11. Whatsmeow Manager (1 client/tenant, lazy init).
	waStateR := postgres.NewWhatsAppStateRepo(appPool, platformPool)
	waManager := whatsmeow.NewManager(whatsmeow.DefaultConfig(), waStateR, log)

	// Fase 10 (#177): app-level config via DB (system_settings) em vez
	// de env vars. Seed dos defaults na primeira inicialização; lê
	// whatsmeow.enabled/device_dsn/identity.kind/identity.os do DB.
	settingsRepo := postgres.NewSystemSettingsRepo(appPool, platformPool)
	settingsSvc := ucsettings.NewService(settingsRepo, ucsettings.NewEnvelopeSealer(localSealer.Envelope()), 1, log)
	if err := settingsSvc.SeedDefaults(ctx, "system@boot"); err != nil {
		log.Warn().Err(err).Msg("settings: seed defaults (continuando)")
	}

	// Watch hot-reload: o Manager re-aplica factory quando
	// whatsmeow.enabled muda.
	settingsCh, settingsCancel := settingsSvc.Watch()
	go func() {
		for ev := range settingsCh {
			if !strings.HasPrefix(ev.Key, "whatsmeow.") {
				continue
			}
			// Para simplificar: o re-apply é um restart do Manager
			// (DisconnectAll + nova factory). Hot-reload real viria
			// com sub-issue de reconnect por-tenant.
			log.Info().Str("key", ev.Key).Str("actor", ev.UpdatedBy).Msg("settings: whatsmeow.* changed — restart required for full effect")
		}
	}()
	defer settingsCancel()

	// Lê whatsmeow.enabled + whatsmeow.device_dsn + identity do DB.
	var whatsmeowEnabled bool
	var whatsmeowDeviceDSN, whatsmeowIdentityKind, whatsmeowIdentityOS string
	if err := settingsSvc.Get(ctx, "whatsmeow.enabled", &whatsmeowEnabled, false); err != nil {
		log.Warn().Err(err).Msg("settings: get whatsmeow.enabled (default false)")
	}
	_ = settingsSvc.Get(ctx, "whatsmeow.device_dsn", &whatsmeowDeviceDSN, "")
	_ = settingsSvc.Get(ctx, "whatsmeow.identity.kind", &whatsmeowIdentityKind, "chrome")
	_ = settingsSvc.Get(ctx, "whatsmeow.identity.os", &whatsmeowIdentityOS, "Mac OS")

	if whatsmeowEnabled && whatsmeowDeviceDSN != "" {
		identity := whatsmeow.IdentityFromConfig(whatsmeowIdentityKind, whatsmeowIdentityOS)
		waManager.SetClientFactory(identity, whatsmeow.NewRealClientFactory(whatsmeow.RealFactoryConfig{
			DeviceDSN:  whatsmeowDeviceDSN,
			Transcoder: nil, // transcoder real é ffmpeg — wire em #158 follow-up
			Log:        log,
		}))
		log.Info().
			Bool("enabled", whatsmeowEnabled).
			Str("identity", whatsmeowIdentityKind).
			Str("device_dsn_host", redactedHost(whatsmeowDeviceDSN)).
			Msg("whatsmeow: real client factory enabled (Phase 9 + 10)")
	} else {
		log.Info().Bool("enabled", whatsmeowEnabled).Msg("whatsmeow: stub (default)")
	}

	senderRegistry := providerregistry.Build(keyring, log, providerregistry.BuildOpts{
		Whatsmeow: &providerregistry.WhatsmeowDeps{Manager: waManager},
	})

	// 13. S3 store (Fase 6)
	store, err := s3store.New(ctx, log, s3store.Config{
		Endpoint:     cfg.S3Endpoint,
		AccessKey:    cfg.S3AccessKey,
		SecretKey:    cfg.S3SecretKey,
		Bucket:       cfg.S3Bucket,
		BackupBucket: cfg.S3BackupBucket,
		UseSSL:       false,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 store: %w", err)
	}

	// 14. Backup service (Fase 6)
	backupJobs := ucbackup.NewJobStore(time.Hour)
	backupJobs.StartJanitor(ctx, 10*time.Minute)

	// Admin pool (Fase 1) + audit repo — conecta aqui para que backup
	// possa registrar audit logs no mesmo pool.
	adminDSN := cfg.AdminDBURL
	if adminDSN == "" {
		adminDSN = cfg.PlatformDBURL
	}
	adminDB, err := adminrepo.NewDB(ctx, adminDSN)
	if err != nil {
		return nil, fmt.Errorf("admin db: %w", err)
	}
	adminRepos := adminrepo.NewRepositories(adminDB)

	backupSvc := ucbackup.New(ucbackup.Options{
		Logger:       log,
		TxRunner:     txRunner,
		Store:        store,
		PGXPool:      appPool,
		PlatformPool: platformPool,
		Jobs:         backupJobs,
		Audit:        adminRepos.Audit,
		Version:      "0.6.0-fase6",
		Disconnector: waManager,
	})

	pollInterval, err := time.ParseDuration(cfg.OutboxPollInterval)
	if err != nil || pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	relay := ucoutbox.New(outboxRepo, senderRegistry, bus, ucoutbox.Config{
		PollInterval: pollInterval,
		BatchSize:    cfg.OutboxBatch,
	}, log)

	senderSvc := ucmessaging.NewSenderService(msgRepo, outboxRepo, resolver, relay, log)
	listSvc := ucmessaging.NewListService(convRepo, msgRepo, log)
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
			// AutoAssign via ACD (se ligado). Sem ACD, no-op silencioso.
			_, err := routerSvc.AutoAssign(ctx, m.TenantID, m.ConversationID)
			return err
		},
		ucreconcile.Config{
			Interval:  reconcileInterval,
			BatchSize: cfg.ReconcileBatch,
		},
		log,
	)

	// 12. Webhook handlers.
	metaSecrets, err := webhooksecrets.NewEnvMetaSecrets()
	if err != nil {
		return nil, fmt.Errorf("load meta secrets: %w", err)
	}
	tgSecrets := webhooksecrets.NewEnvTelegramSecrets()
	metaH := meta.New(ingestor, metaSecrets, metaSecrets, meta.Config{}, log)
	tgH := telegram.New(ingestor, tgSecrets, telegram.Config{}, log)

	// 11. Health
	hc := health.NewChecker()
	hc.Add("postgres", func(ctx context.Context) error {
		return appPool.Ping(ctx)
	})

	// 15. Admin services (Fase 1) + adminweb server (Fase 1+6)
	sessionTTL, _ := time.ParseDuration(cfg.SessionTTL)
	if sessionTTL == 0 {
		sessionTTL = 24 * time.Hour
	}
	hasher := argon2.New(argon2.DefaultParams())
	sessionStore := memcache.NewSessionStore(nil)
	sessionStore.StartReaper(context.Background(), 5*time.Minute)

	loginSvc := ucauth.NewLoginService(
		adminRepos.Users, adminRepos.Roles,
		sessionStore, adminRepos.Audit, hasher,
		nil, sessionStore, nil,
		sessionTTL, false,
	)
	logoutSvc := ucauth.NewLogoutService(sessionStore, adminRepos.Audit)
	sessionSvc := ucauth.NewSessionService(sessionStore, adminRepos.Users, sessionTTL)

	tenantUC := ucadmin.NewTenantService(adminRepos.Tenants, adminRepos.Audit)
	userUC := ucadmin.NewUserService(adminRepos.Users, adminRepos.Roles, adminRepos.Audit, hasher)
	roleUC := ucadmin.NewRoleService(adminRepos.Roles, adminRepos.Audit)
	auditUC := ucadmin.NewAuditQueryService(adminRepos.Audit)

	adminSrv := adminweb.NewServer(
		log, hc, "0.6.0-fase6",
		loginSvc, logoutSvc,
		adminmw.SessionConfig{
			Resolver: sessionSvc,
			Cookie:   "__Host-mez_admin",
			TTL:      sessionTTL,
		},
		sessionStore, nil,
		ratelimit.New(10.0/60.0, 10.0),
		tenantUC, userUC, roleUC, auditUC,
	)
	// Fase 6: injeta backup service e admin verifier (usado para confirmar
	// senha no reset). Se backupSvc for nil, rotas de backup/reset não
	// são registradas.
	adminSrv.SetBackupService(backupSvc, loginSvc)
	// Fase 10 (#177): system settings UI (substitui env vars app-level).
	settingsH := adminweb.NewSettingsHandlers(settingsSvc, log)
	adminSrv.SetSettingsHandlers(settingsH)
	_ = adminmw.CSRF(adminmw.DefaultCSRFConfig())

	// 16. HTTP server
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
		AdminRouter:     adminSrv.Router(), // Fase 6: montado em /admin
		JWTSecret:       jwtSecret,
		SenderService:   senderSvc,
		ListService:     listSvc,
		SenderRegistry:  senderRegistry,
		QRCodeProvider:  waManager,
		BackupService:   backupSvc,
		AdminVerifier:   loginSvc, // Reset usa LoginLocal para confirmar senha
	})
	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
		// Issue #152 (H14 audit, DREAD 6.0): slow-loris defense.
		// ReadHeaderTimeout limita o tempo que o cliente tem para enviar
		// os headers — sem isso, 1 byte a cada 14s mantém a conexão
		// viva indefinidamente (ReadTimeout só conta após o request body).
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		// Limite de header em 1 MiB. Headers legítimos (Authorization,
		// Cookies) cabem em < 4 KiB; 1 MiB protege contra heap blow-up
		// por headers gigantes.
		MaxHeaderBytes: 1 << 20,
	}

	return &AppContext{
		Log: log, Cfg: cfg, Health: hc, Metrics: metricsReg, Bus: bus, TxRunner: txRunner,
		Outbox: outboxRepo, InboundEvs: inboundEvsRepo,
		ConvRepo: convRepo, MsgRepo: msgRepo, TenantRepo: tenantRepo,
		SenderRegistry: senderRegistry, SenderService: senderSvc, ListService: listSvc,
		StatusConsumer: statusConsumer,
		Keyring: keyring, CredsRepo: credsRepo,
		Ingestor: ingestor, Router: routerSvc, Relay: relay, Reconciler: reconciler,
		BackupService: backupSvc, Store: store, AdminVerifier: loginSvc,
		AdminServer: adminSrv,
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

	select {
	case sig := <-sigCh:
		app.Log.Info().Str("signal", sig.String()).Msg("shutdown: signal received")
	case err := <-errCh:
		app.Log.Error().Err(err).Msg("shutdown: fatal error")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.HTTPServer.Shutdown(shutdownCtx); err != nil {
		app.Log.Error().Err(err).Msg("http shutdown")
	}
	if err := app.Bus.Drain(shutdownCtx); err != nil {
		app.Log.Error().Err(err).Msg("bus drain")
	}
	app.Relay.Stop()
	app.Reconciler.Stop()

	done := make(chan struct{})
	go func() {
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

var _ = chi.NewRouter

// redactedHost extrai o host:port de uma DSN pgx para log (sem senha).
// Ex.: "postgres://user:pass@host:5432/db?sslmode=disable" → "host:5432".
func redactedHost(dsn string) string {
	// Procura o último @ antes do ?.
	at := -1
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '@' {
			at = i
		} else if dsn[i] == '?' {
			break
		}
	}
	if at < 0 || at+1 >= len(dsn) {
		return "<invalid dsn>"
	}
	rest := dsn[at+1:]
	// Trim query.
	for i := 0; i < len(rest); i++ {
		if rest[i] == '?' {
			return rest[:i]
		}
	}
	return rest
}
