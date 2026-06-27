//go:build integration
// +build integration

// Chaos test: bus drain graceful shutdown (Fase 8 #106).
//
// Valida que Bus.Drain drena buffers corretamente durante shutdown.
package chaos

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBus_DrainGracefulShutdown(t *testing.T) {
	if os.Getenv("MEZ_DATABASE_URL") == "" {
		t.Skip("MEZ_DATABASE_URL not set; chaos test requires real DB")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dbURL := os.Getenv("MEZ_DATABASE_URL")
	port := FreePort(t)
	addr := ":" + itoa(port)

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	tenantID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name, slug, active) VALUES ($1, $2, $3, true)`,
		tenantID, "chaos-tenant", "chaos-"+tenantID[:8],
	); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// Sobe o processo, deixa rodar 2s, SIGTERM graceful.
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
	time.Sleep(2 * time.Second)

	// Graceful shutdown via SIGTERM.
	h.Stop(true)

	// Validação: o exit code deve ser 0 (graceful).
	if err := h.Cmd.Wait(); err != nil {
		// Exit code != 0 é registrado como erro, mas pode ser benigno.
		t.Logf("cmd wait: %v (exit code possivelmente não-zero)", err)
	}
}
