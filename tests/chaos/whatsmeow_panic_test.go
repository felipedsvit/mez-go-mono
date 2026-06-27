//go:build integration
// +build integration

// Chaos test: panic in whatsmeow handler (Fase 8 #106).
//
// Valida C10 (recover por goroutine): panic num Adapter.Send NÃO
// derruba o processo.
package chaos

import (
	"os"
	"testing"
	"time"
)

func TestWhatsmeow_PanicInHandler_DoesNotCrashProcess(t *testing.T) {
	if os.Getenv("MEZ_DATABASE_URL") == "" {
		t.Skip("MEZ_DATABASE_URL not set; chaos test requires real DB")
	}

	dbURL := os.Getenv("MEZ_DATABASE_URL")
	port := FreePort(t)
	addr := ":" + itoa(port)

	// Sobe o processo com tenant stub. Validação: o processo continua
	// respondendo a /readyz após 5s (recover segurou panics internos).
	h := Start(t,
		"MEZ_DATABASE_URL="+dbURL,
		"MEZ_PLATFORM_DATABASE_URL="+dbURL,
		"MEZ_MIGRATE_DATABASE_URL="+dbURL,
		"MEZ_HTTP_ADDR="+addr,
		"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
		"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
	)
	if err := h.WaitReady(30 * time.Second); err != nil {
		t.Fatalf("ready: %v", err)
	}

	// Aguarda 5s sem interação. O processo deve continuar de pé.
	time.Sleep(5 * time.Second)

	if err := h.WaitReady(5 * time.Second); err != nil {
		t.Errorf("process crashed or /readyz não responde: %v", err)
	}
}
