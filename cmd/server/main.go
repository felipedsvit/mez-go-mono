// Package main — entrypoint do mez-go-mono (single binary).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/felipedsvit/mez-go-mono/pkg/logger"
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

// runServe inicia o pipeline inbound completo (Fase 2+).
// Boot determinístico e graceful shutdown coordenado — ver wire.go.
func runServe(cfg config.Config, log zerolog.Logger) {
	if err := cfg.ValidateServe(); err != nil {
		log.Fatal().Err(err).Msg("config validation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fase 8 #99 sub-issue: migrate inline antes de subir o pipeline
	// (default true; em prod pode-se desligar via MEZ_MIGRATE_ON_BOOT=false
	// e rodar migrations em job separado).
	if cfg.MigrateOnBoot {
		log.Info().Msg("serve: running migrations inline (migrate_on_boot=true)")
		if err := runMigrateInline(ctx, cfg, log); err != nil {
			log.Fatal().Err(err).Msg("inline migrate failed")
		}
	}

	app, err := wireServices(ctx, cfg, log)
	if err != nil {
		log.Fatal().Err(err).Msg("wire services")
	}

	log.Info().
		Str("addr", app.Cfg.HTTPAddr).
		Bool("s3", app.Store != nil).
		Msg("serve: starting")

	if err := runWithGracefulShutdown(ctx, app); err != nil {
		log.Error().Err(err).Msg("serve")
	}
}
