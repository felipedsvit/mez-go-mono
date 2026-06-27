package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb"
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
		cancel()
		log.Fatal().Err(err).Msg("connect app pool")
	}
	defer appPool.Close()

	platformPool, err := postgres.ConnectPool(ctx, cfg.PlatformDBURL, 10)
	if err != nil {
		cancel()
		log.Fatal().Err(err).Msg("connect platform pool")
	}
	defer platformPool.Close()

	_ = postgres.NewTxRunner(appPool, platformPool, log)

	healthChecker.Add("postgres", func(ctx context.Context) error {
		return appPool.Ping(ctx)
	})

	busCfg := broker.BusConfig{
		InboundBuffer:  cfg.BusInboundBuf,
		OutboundBuffer: cfg.BusOutboundBuf,
	}
	bus := broker.NewBus(busCfg, log, metricsReg)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/health", health.LiveHandler())
	r.Get("/readyz", health.ReadyHandler(healthChecker))
	r.Get("/metrics", metricsReg.Handler().ServeHTTP)

	// Static assets (htmx placeholder, css).
	staticSub, _ := fs.Sub(adminweb.Assets(), "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// /setup wizard (issue #16). 404 once an admin exists.
	setupH := adminweb.NewSetupHandler(appPool, log)
	r.Get("/setup", setupH.Get)
	r.Post("/setup", setupH.Post)

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
