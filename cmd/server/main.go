package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/auth/argon2"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	memcache "github.com/felipedsvit/mez-go-mono/internal/adapter/cache/memory"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	adminrepo "github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres/admin"
	adminweb "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb"
	adminmw "github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/middleware/ratelimit"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/felipedsvit/mez-go-mono/pkg/health"
	"github.com/felipedsvit/mez-go-mono/pkg/logger"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.LogLevel, os.Stdout)

	if len(os.Args) < 2 {
		log.Fatal().Msg("expected subcommand: serve | migrate | setup | rotate-kek")
	}

	switch os.Args[1] {
	case "serve":
		runServe(cfg, log)
	case "migrate":
		runMigrate(cfg, log)
	case "setup":
		runSetup(cfg, log)
	case "rotate-kek":
		runRotateKEK(cfg, log)
	default:
		log.Fatal().Str("cmd", os.Args[1]).Msg("unknown subcommand")
	}
}

func runServe(cfg config.Config, log zerolog.Logger) {
	if err := cfg.ValidateServe(); err != nil {
		log.Fatal().Err(err).Msg("config validation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	metricsReg := metrics.NewRegistry()
	healthChecker := health.NewChecker()

	appPool, err := postgres.ConnectPool(ctx, cfg.DatabaseURL, 20)
	if err != nil {
		log.Fatal().Err(err).Msg("connect app pool")
	}
	defer appPool.Close()

	platformPool, err := postgres.ConnectPool(ctx, cfg.PlatformDBURL, 10)
	if err != nil {
		log.Fatal().Err(err).Msg("connect platform pool")
	}
	defer platformPool.Close()

	postgres.NewTxRunner(appPool, platformPool, log)

	healthChecker.Add("postgres", func(ctx context.Context) error {
		return appPool.Ping(ctx)
	})

	busCfg := broker.BusConfig{
		InboundBuffer:  cfg.BusInboundBuf,
		OutboundBuffer: cfg.BusOutboundBuf,
	}
	bus := broker.NewBus(busCfg, log, metricsReg)

	sessionTTL, err := time.ParseDuration(cfg.SessionTTL)
	if err != nil {
		sessionTTL = 24 * time.Hour
	}

	hasher := argon2.New(argon2.DefaultParams())

	sessionStore := memcache.NewSessionStore(nil)
	sessionStore.StartReaper(context.Background(), 5*time.Minute)

	adminDSN := cfg.AdminDBURL
	if adminDSN == "" {
		adminDSN = cfg.PlatformDBURL
	}

	adminDB, err := adminrepo.NewDB(ctx, adminDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("connect admin db")
	}
	defer adminDB.Close()

	adminRepos := adminrepo.NewRepositories(adminDB)

	loginSvc := auth.NewLoginService(
		adminRepos.Users,
		adminRepos.Roles,
		sessionStore,
		adminRepos.Audit,
		hasher,
		nil,
		sessionStore,
		nil, // lockout: default (5 fails / 15min)
		sessionTTL,
		false,
	)

	logoutSvc := auth.NewLogoutService(sessionStore, adminRepos.Audit)

	sessionSvc := auth.NewSessionService(sessionStore, adminRepos.Users, sessionTTL)

	tenantUC := ucadmin.NewTenantService(adminRepos.Tenants, adminRepos.Audit)
	userUC := ucadmin.NewUserService(adminRepos.Users, adminRepos.Roles, adminRepos.Audit, hasher)
	roleUC := ucadmin.NewRoleService(adminRepos.Roles, adminRepos.Audit)
	auditUC := ucadmin.NewAuditQueryService(adminRepos.Audit)

	adminSrv := adminweb.NewServer(
		log, healthChecker, "0.1.0",
		loginSvc, logoutSvc,
		adminmw.SessionConfig{
			Resolver: sessionSvc,
			Cookie:   "__Host-mez_admin",
			TTL:      sessionTTL,
		},
		sessionStore,
		nil,                            // OIDC IdP — disabled in Phase 1 by default
		ratelimit.New(10.0/60.0, 10.0), // 10 requests/minute, burst 10
		tenantUC, userUC, roleUC, auditUC,
	)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

	r.Get("/health", health.LiveHandler())
	r.Get("/readyz", health.ReadyHandler(healthChecker))
	r.Get("/metrics", metricsReg.Handler().ServeHTTP)
	r.Get("/setup", setupHandler(log))

	r.Mount("/admin", adminSrv.Router())

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info().Str("addr", cfg.HTTPAddr).Msg("starting server")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	<-sigCh
	log.Info().Msg("shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := bus.Drain(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("bus drain error")
	}

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server shutdown error")
	}

	log.Info().Msg("server stopped")
}

func setupHandler(log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!DOCTYPE html>
<html><body>
<h1>Mez Go Mono Setup</h1>
<p>Setup wizard for Phase 1.</p>
<form method="POST" action="/setup">
<label>Email: <input type="email" name="email" required></label><br>
<label>Password: <input type="password" name="password" required minlength="8"></label><br>
<button type="submit">Create Admin</button>
</form>
</body></html>`))
	}
}
