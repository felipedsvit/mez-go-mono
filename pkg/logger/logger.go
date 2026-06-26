package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

func init() {
	zerolog.TimeFieldFormat = time.RFC3339Nano
}

func New(level string, w io.Writer) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	if w == nil {
		w = os.Stdout
	}
	return zerolog.New(w).
		Level(lvl).
		With().
		Timestamp().
		Caller().
		Logger()
}
