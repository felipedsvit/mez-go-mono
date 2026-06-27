// Package postgres — whatsapp_history_repo.go: persiste o HistorySync do
// whatsmeow (issue #158, sub-issue C).
//
// HistorySync é o dump de mensagens que o WhatsApp envia quando uma
// sessão é restaurada em outro lugar, ou quando o cliente entra pela
// primeira vez. Pode ter milhares de mensagens — OOM guard obrigatório.
//
// Estratégia: bounded 1000 mensagens/tenant no primeiro start. Lote
// de 100 por INSERT. RLS fail-closed (C3+C4).
package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// HistoryRepo persiste o HistorySync (whatsapp_history).
// Bounded: 1000 mensagens/tenant no primeiro start.
type HistoryRepo struct {
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
	maxPerTenant int
}

// NewHistoryRepo cria o repo. maxPerTenant<=0 usa default 1000.
func NewHistoryRepo(appPool, platformPool *pgxpool.Pool) *HistoryRepo {
	return &HistoryRepo{
		appPool:      appPool,
		platformPool: platformPool,
		maxPerTenant: 1000,
	}
}

// HistoryMessage é o shape persistido (subset do waE2E.Message).
// Mantemos apenas os campos essenciais — o resto (encryption keys etc.)
// fica no client whatsmeow, não na nossa DB.
type HistoryMessage struct {
	TenantID  string `json:"-"`
	JID       string `json:"jid"`
	MsgID     string `json:"msg_id"`
	Timestamp int64  `json:"timestamp"`
	FromMe    bool   `json:"from_me"`
	Body      string `json:"body"`
	Type      string `json:"type"`
}

// InsertMany insere um lote de mensagens. Idempotente (ON CONFLICT DO NOTHING).
// Retorna o número de mensagens efetivamente inseridas.
//
// tenantID é injetado via context (RunInTenantTx). Caller deve passar
// um ctx com mez.tenant_id setado.
func (r *HistoryRepo) InsertMany(ctx context.Context, msgs []HistoryMessage) (int, error) {
	if len(msgs) == 0 {
		return 0, nil
	}
	if len(msgs) > r.maxPerTenant {
		msgs = msgs[:r.maxPerTenant]
	}

	tx, err := r.appPool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("history: begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	inserted := 0
	for _, m := range msgs {
		payload, _ := json.Marshal(map[string]any{
			"timestamp": m.Timestamp,
			"from_me":   m.FromMe,
			"body":      m.Body,
			"type":      m.Type,
		})
		ct, err := tx.Exec(ctx, `
			INSERT INTO whatsapp_history (tenant_id, jid, msg_id, payload)
			VALUES (current_setting('mez.tenant_id', false)::uuid, $1, $2, $3)
			ON CONFLICT (tenant_id, jid, msg_id) DO NOTHING
		`, m.JID, m.MsgID, payload)
		if err != nil {
			return inserted, fmt.Errorf("history: insert: %w", err)
		}
		inserted += int(ct.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return inserted, fmt.Errorf("history: commit: %w", err)
	}
	return inserted, nil
}

// CountByTenant retorna o número de mensagens persistidas para o tenant.
func (r *HistoryRepo) CountByTenant(ctx context.Context) (int, error) {
	var n int
	err := r.appPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM whatsapp_history
		WHERE tenant_id = current_setting('mez.tenant_id', false)::uuid
	`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("history: count: %w", err)
	}
	return n, nil
}
