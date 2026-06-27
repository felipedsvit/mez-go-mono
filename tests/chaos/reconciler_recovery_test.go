//go:build integration
// +build integration

// Chaos test: reconciler recover from SIGKILL (Fase 8 #106).
//
// Valida C1 (reconciler recovery): após kill -9, a próxima instância
// drena via boot sweep em < 35s.
package chaos

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReconciler_RecoversFromSIGKILL(t *testing.T) {
	if os.Getenv("MEZ_DATABASE_URL") == "" {
		t.Skip("MEZ_DATABASE_URL not set; chaos test requires real DB")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

	// Insere 50 mensagens em status='received'.
	for i := 0; i < 50; i++ {
		convID := uuid.NewString()
		contactID := uuid.NewString()
		msgID := uuid.NewString()
		if _, err := pool.Exec(ctx,
			`INSERT INTO contacts (id, tenant_id, channel, provider_id) VALUES ($1, $2, $3, $4)`,
			contactID, tenantID, "waba", "peer-1",
		); err != nil {
			t.Fatalf("seed contact: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO conversations (id, tenant_id, channel, contact_id, status, external_id) VALUES ($1, $2, $3, $4, $5, $6)`,
			convID, tenantID, "waba", contactID, "open", "ext-1",
		); err != nil {
			t.Fatalf("seed conv: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO messages (id, tenant_id, channel, conversation_id, contact_id, direction, type, status, body) VALUES ($1, $2, $3, $4, $5, $6, $7, 'received', $8)`,
			msgID, tenantID, "waba", convID, contactID, "inbound", "text", "orphan",
		); err != nil {
			t.Fatalf("seed msg: %v", err)
		}
	}

	// Primeira instância: sobe, espera reconciler processar ~10, mata.
	h1 := Start(t,
		"MEZ_DATABASE_URL="+dbURL,
		"MEZ_PLATFORM_DATABASE_URL="+dbURL,
		"MEZ_MIGRATE_DATABASE_URL="+dbURL,
		"MEZ_HTTP_ADDR="+addr,
		"MEZ_RECONCILE_INTERVAL=2s",
		"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
		"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
		"MEZ_S3_ENDPOINT=http://localhost:0",
		"MEZ_S3_ACCESS_KEY=test",
		"MEZ_S3_SECRET_KEY=test",
	)
	if err := h1.WaitReady(30 * time.Second); err != nil {
		t.Fatalf("h1 ready: %v", err)
	}
	time.Sleep(5 * time.Second) // deixa reconciler processar alguns
	h1.Kill9()
	h1.Stop(false)

	// Segunda instância: deve drenar via boot sweep.
	h2 := Start(t,
		"MEZ_DATABASE_URL="+dbURL,
		"MEZ_PLATFORM_DATABASE_URL="+dbURL,
		"MEZ_MIGRATE_DATABASE_URL="+dbURL,
		"MEZ_HTTP_ADDR="+addr,
		"MEZ_RECONCILE_INTERVAL=2s",
		"MEZ_MASTER_KEY=test-key-32-bytes-base64-aaaaaa",
		"MEZ_SESSION_SECRET=test-session-secret-32-bytes",
	)
	if err := h2.WaitReady(30 * time.Second); err != nil {
		t.Fatalf("h2 ready: %v", err)
	}
	if err := WaitForReconcile(t, dbURL, 0, 35*time.Second); err != nil {
		t.Errorf("reconcile not complete: %v", err)
	}
}

// itoa converte int em string (helper para evitar import strconv).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
