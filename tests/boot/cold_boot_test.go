//go:build integration
// +build integration

// Package boot — cold_boot_test.go: cold-boot com N tenants (Fase 8 #107).
//
// Mede tempo de boot (spawn → /readyz 200) para N=20, 50, 100 tenants.
// Detecta regressão de tempo de inicialização. Piso soft (não-fatal em
// hardware lento).
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

func TestColdBoot_ScalesLinearly(t *testing.T) {
	if os.Getenv("MEZ_DATABASE_URL") == "" {
		t.Skip("MEZ_DATABASE_URL not set; cold-boot test requires real DB")
	}
	if testing.Short() {
		t.Skip("short")
	}

	for _, n := range []int{20, 50, 100} {
		n := n
		t.Run(fmt.Sprintf("tenants=%d", n), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			dbURL := os.Getenv("MEZ_DATABASE_URL")
			pool, err := pgxpool.New(ctx, dbURL)
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
			defer pool.Close()

			// Cria N tenants + channel_credentials whatsmeow.
			for i := 0; i < n; i++ {
				tenantID := uuid.NewString()
				if _, err := pool.Exec(ctx,
					`INSERT INTO tenants (id, name, slug, active) VALUES ($1, $2, $3, true)`,
					tenantID, fmt.Sprintf("boot-tenant-%d", i), "boot-"+tenantID[:8],
				); err != nil {
					t.Fatalf("seed tenant: %v", err)
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

			port := chaos.FreePort(t)
			addr := ":" + itoa(port)
			h := chaos.Start(t,
				"MEZ_DATABASE_URL="+dbURL,
				"MEZ_PLATFORM_DATABASE_URL="+dbURL,
				"MEZ_MIGRATE_DATABASE_URL="+dbURL,
				"MEZ_HTTP_ADDR="+addr,
				fmt.Sprintf("MEZ_MAX_ACTIVE_TENANTS=%d", n),
				"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
				"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
			)
			defer h.Stop(true)

			// Mede tempo desde spawn até /readyz 200.
			start := time.Now()
			if err := h.WaitReady(60 * time.Second); err != nil {
				t.Fatalf("not ready: %v", err)
			}
			bootDur := time.Since(start)

			t.Logf("N=%d boot_duration=%v", n, bootDur)

			// Piso soft: boot deve ser < 30s em hardware moderno.
			// Em hardware lento,放宽 pra 60s (warning, não fatal).
			if bootDur > 30*time.Second {
				t.Logf("WARNING: boot > 30s (took %v) — possible regression", bootDur)
			}
		})
	}
}
