// Package logger — testes do construtor New.
//
// Cobre:
//   - New() parseia level string e cria logger estruturado
//   - writer nil → default stdout (não panicar)
//   - level inválido → fallback para info
//   - todos os níveis válidos produzem logger funcional
package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNew_AllValidLevels(t *testing.T) {
	t.Parallel()

	levels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	for _, lvl := range levels {
		lvl := lvl
		t.Run(lvl, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			l := New(lvl, &buf)
			// Emite no nível configurado. fatal/panic são skip (encerram o processo).
			switch lvl {
			case "trace":
				l.Trace().Msg("m")
			case "debug":
				l.Debug().Msg("m")
			case "info":
				l.Info().Msg("m")
			case "warn":
				l.Warn().Msg("m")
			case "error":
				l.Error().Msg("m")
			case "fatal":
				// Não chamamos — fatal chama os.Exit. Verificamos apenas que
				// o logger é construído sem panic.
				if l.GetLevel().String() != "fatal" {
					t.Errorf("level = %s, want fatal", l.GetLevel())
				}
				return
			case "panic":
				// Não chamamos — panic levanta runtime.Goexit. Verificamos apenas
				// o nível configurado.
				if l.GetLevel().String() != "panic" {
					t.Errorf("level = %s, want panic", l.GetLevel())
				}
				return
			}
			if buf.Len() == 0 {
				t.Errorf("level=%s produced no output", lvl)
			}
		})
	}
}

func TestNew_InvalidLevel_FallsBackToInfo(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := New("not-a-level", &buf)
	l.Info().Msg("visible")
	if buf.Len() == 0 {
		t.Error("invalid level should fall back to info; info message should appear")
	}

	var buf2 bytes.Buffer
	l2 := New("not-a-level", &buf2)
	l2.Debug().Msg("hidden")
	if buf2.Len() != 0 {
		t.Error("debug message should NOT appear when level falls back to info")
	}
}

func TestNew_NilWriter_DefaultsToStdout(t *testing.T) {
	// Não usamos t.Parallel porque o default writer é global.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("New(_, nil) panicked: %v", r)
		}
	}()
	l := New("info", nil)
	if l.GetLevel() == 0 {
		t.Error("logger should have a default level set")
	}
}

func TestNew_OutputIsJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := New("info", &buf)
	l.Info().Str("k", "v").Msg("hi")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not JSON: %v\nraw: %s", err, buf.String())
	}
	if parsed["message"] != "hi" {
		t.Errorf("message field = %v, want 'hi'", parsed["message"])
	}
	if parsed["k"] != "v" {
		t.Errorf("custom field k = %v, want 'v'", parsed["k"])
	}
	if _, ok := parsed["time"]; !ok {
		t.Error("time field should be present")
	}
	if _, ok := parsed["caller"]; !ok {
		t.Error("caller field should be present (zerolog Caller enabled)")
	}
}

func TestNew_RFC3339NanoTimeFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	l := New("info", &buf)
	l.Info().Msg("ts")

	var parsed map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	ts, ok := parsed["time"].(string)
	if !ok {
		t.Fatal("time field missing or wrong type")
	}
	if !strings.Contains(ts, "T") {
		t.Errorf("time format should be RFC3339Nano (contains 'T'), got %q", ts)
	}
	// RFC3339Nano tem fração de segundos com vírgula ou ponto dependendo do encoding.
	if len(ts) < len("2025-01-01T00:00:00Z") {
		t.Errorf("time string too short: %q", ts)
	}
}
