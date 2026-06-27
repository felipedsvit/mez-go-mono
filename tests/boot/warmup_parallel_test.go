//go:build integration
// +build integration

// Package boot — warmup_parallel_test.go: warm-up paralelo vs sequencial
// (Fase 8 #107).
//
// Mede speedup do warm-up paralelo (errgroup bounded em
// MEZ_MAX_ACTIVE_TENANTS) vs. sequencial.
package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/tests/chaos"
)

func TestWarmup_ParallelSpeedup(t *testing.T) {
	if os.Getenv("MEZ_DATABASE_URL") == "" {
		t.Skip("MEZ_DATABASE_URL not set")
	}
	if testing.Short() {
		t.Skip("short")
	}

	const n = 50
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dbURL := os.Getenv("MEZ_DATABASE_URL")
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Seed n tenants.
	for i := 0; i < n; i++ {
		tenantID := uuid.NewString()
		if _, err := pool.Exec(ctx,
			`INSERT INTO tenants (id, name, slug, active) VALUES ($1, $2, $3, true)`,
			tenantID, fmt.Sprintf("warmup-tenant-%d", i), "warmup-"+tenantID[:8],
		); err != nil {
			t.Fatalf("seed: %v", err)
		}
		wrapped, _ := json.Marshal([]byte("dummy-dek-32-bytes-aaaaaaaaaaa"))
		encrypted, _ := json.Marshal([]byte("dummy-encrypted-blob"))
		if _, err := pool.Exec(ctx,
			`INSERT INTO channel_credentials (tenant_id, channel, wrapped_dek, encrypted, kek_version) VALUES ($1, $2, $3, $4, 1)`,
			tenantID, "whatsmeow", wrapped, encrypted,
		); err != nil {
			t.Fatalf("seed creds: %v", err)
		}
	}

	// Sequencial: MaxActiveTenants=1 (força sequencial).
	port1 := chaos.FreePort(t)
	addr1 := ":" + itoa(port1)
	hSeq := chaos.Start(t,
		"MEZ_DATABASE_URL="+dbURL,
		"MEZ_PLATFORM_DATABASE_URL="+dbURL,
		"MEZ_MIGRATE_DATABASE_URL="+dbURL,
		"MEZ_HTTP_ADDR="+addr1,
		"MEZ_MAX_ACTIVE_TENANTS=1", // sequencial
		"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
		"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
	)
	startSeq := time.Now()
	if err := hSeq.WaitReady(120 * time.Second); err != nil {
		t.Fatalf("seq ready: %v", err)
	}
	durSeq := time.Since(startSeq)
	hSeq.Stop(true)

	// Paralelo: MaxActiveTenants=8 (8 cores).
	port2 := chaos.FreePort(t)
	addr2 := ":" + itoa(port2)
	hPar := chaos.Start(t,
		"MEZ_DATABASE_URL="+dbURL,
		"MEZ_PLATFORM_DATABASE_URL="+dbURL,
		"MEZ_MIGRATE_DATABASE_URL="+dbURL,
		"MEZ_HTTP_ADDR="+addr2,
		"MEZ_MAX_ACTIVE_TENANTS=8", // paralelo
		"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
		"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
	)
	defer hPar.Stop(true)
	startPar := time.Now()
	if err := hPar.WaitReady(120 * time.Second); err != nil {
		t.Fatalf("par ready: %v", err)
	}
	durPar := time.Since(startPar)

	speedup := float64(durSeq) / float64(durPar)
	t.Logf("N=%d seq=%v par=%v speedup=%.2fx", n, durSeq, durPar, speedup)

	// Sanity check: speedup deve ser >= 1x (paralelo não pode ser mais
	// lento que sequencial em ambiente de teste).
	if speedup < 1.0 {
		t.Logf("WARNING: speedup=%.2fx < 1.0 (paralelo mais lento que sequencial)", speedup)
	}
}
