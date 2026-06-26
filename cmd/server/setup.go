package main

import (
	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/rs/zerolog"
)

func runSetup(cfg config.Config, log zerolog.Logger) {
	log.Info().Msg("setup wizard runs via HTTP GET /setup after 'serve' starts")
	log.Info().Msg("use: curl -X POST http://localhost:8080/setup -d 'email=admin@example.com&password=...'")
}
