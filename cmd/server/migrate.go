package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog"
)

func runMigrate(cfg config.Config, log zerolog.Logger) {
	if len(os.Args) < 3 {
		log.Fatal().Msg("usage: mez-go-mono migrate [up|down|drop|force version]")
	}

	cmd := os.Args[2]

	m, err := migrate.New("file://migrations", cfg.MigrateDBURL)
	if err != nil {
		log.Fatal().Err(err).Msg("create migrator")
	}

	switch cmd {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatal().Err(err).Msg("migrate up")
		}
		log.Info().Msg("migration up complete")
	case "down":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			log.Fatal().Err(err).Msg("migrate down")
		}
		log.Info().Msg("migration down complete")
	case "drop":
		if err := m.Drop(); err != nil {
			log.Fatal().Err(err).Msg("migrate drop")
		}
		log.Info().Msg("migration drop complete")
	case "force":
		if len(os.Args) < 4 {
			log.Fatal().Msg("usage: mez-go-mono migrate force <version>")
		}
		version := os.Args[3]
		var v int
		if _, err := fmt.Sscanf(version, "%d", &v); err != nil {
			log.Fatal().Err(err).Msg("invalid version")
		}
		if err := m.Force(v); err != nil {
			log.Fatal().Err(err).Msg("migrate force")
		}
		log.Info().Int("version", v).Msg("migration force complete")
	default:
		log.Fatal().Str("cmd", cmd).Msg("unknown migrate subcommand")
	}
}

// runMigrateInline executa `migrate up` no boot, antes de subir o
// pipeline (Fase 8 #99 sub-issue). Retorna erro se a migração falhar;
// o caller deve abortar o boot (fail-closed).
//
// Vantagens vs. entrypoint.sh:
//
//   - 1 processo em vez de 2 (`migrate` + `serve`).
//   - Sem race condition entre o migrate terminar e o serve tentar
//     conectar (mesmo binário, mesmo pid).
//   - Em dev/CI, basta `mez-go-mono serve` (sem shell wrapper).
//
// Em produção, recomenda-se manter `migrate_on_boot=false` e rodar
// migrations em job/k8s Job separado (audit trail + atomic rollback).
func runMigrateInline(ctx context.Context, cfg config.Config, log zerolog.Logger) error {
	m, err := migrate.New("file://migrations", cfg.MigrateDBURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	// O golang-migrate v4 não tem contexto; usa lock interno.
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	log.Info().Msg("migrate: up complete (inline)")
	return nil
}
