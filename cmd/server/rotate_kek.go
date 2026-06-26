package main

import (
	"fmt"
	"os"

	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/rs/zerolog"
)

func runRotateKEK(cfg config.Config, log zerolog.Logger) {
	log.Warn().Msg("rotate-kek not yet implemented (planned for Phase 7)")
	fmt.Fprintln(os.Stderr, "rotate-kek: not implemented in Phase 0")
	os.Exit(1)
}
