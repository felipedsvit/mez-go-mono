package main

import (
	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/rs/zerolog"
)

// runSetup is the CLI hint. The actual /setup wizard is exposed by the HTTP
// server (see internal/transport/adminweb/handlers_setup.go). On a fresh
// install, after `serve` starts and migrations run, GET /setup creates the
// first admin global. Subsequent /setup calls return 404.
func runSetup(cfg config.Config, log zerolog.Logger) {
	log.Info().Msg("setup wizard: open GET /setup on the running server (see internal/transport/adminweb)")
}
