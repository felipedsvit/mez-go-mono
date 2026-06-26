package main

import (
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
