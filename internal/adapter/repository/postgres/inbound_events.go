// Package postgres — InboundEventsRepo.
//
// Recupera mensagens em estado "received" para o Reconciler (#39) e o
// routing consumer (#37) processarem. Implementa o lado de leitura do
// pipeline inbound: o status "received" é o ponto de entrada; o
// reconciler avança para "routed" após Assign.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// InboundEventsRepo oferece operações cross-tenant sobre messages em
// estado "received" e "routed". É a única peça que precisa enxergar todos
// os tenants (porque reconciler e relay varrem tudo).
type InboundEventsRepo struct {
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
}

// NewInboundEventsRepo cria o repo.
func NewInboundEventsRepo(appPool, platformPool *pgxpool.Pool) *InboundEventsRepo {
	return &InboundEventsRepo{appPool: appPool, platformPool: platformPool}
}

// SelectUnroutedMessages retorna mensagens em status='received' para
// processamento pelo Reconciler. A query é cross-tenant (mez_platform)
// porque o reconciler varre todos os tenants no boot e periodicamente.
//
// Usa FOR UPDATE SKIP LOCKED para que múltiplos workers (reconciler +
// routing consumer) não peguem a mesma mensagem.
func (r *InboundEventsRepo) SelectUnroutedMessages(ctx context.Context, batchSize int) ([]domain.Message, error) {
	rows, err := r.platformPool.Query(ctx,
		`SELECT id, tenant_id, channel, conversation_id, contact_id,
		        direction, type, status, body, provider_msg_id,
		        created_at, updated_at
		 FROM messages
		 WHERE status = 'received'
		 ORDER BY created_at
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("select unrouted: %w", err)
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var m domain.Message
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.Channel, &m.ConversationID, &m.ContactID,
			&m.Direction, &m.Type, &m.Status, &m.Body, &m.ProviderMsgID,
			&m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan unrouted: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows unrouted: %w", err)
	}
	return msgs, nil
}

// MarkRouted avança a mensagem para status='routed' e seta routed_at.
// Cross-tenant (mez_platform) porque o reconciler e o routing consumer
// podem estar processando qualquer tenant.
func (r *InboundEventsRepo) MarkRouted(ctx context.Context, id domain.MessageID) error {
	tag, err := r.platformPool.Exec(ctx,
		`UPDATE messages
		 SET status = 'routed',
		     routed_at = NOW(),
		     updated_at = NOW()
		 WHERE id = $1 AND status = 'received'`,
		string(id),
	)
	if err != nil {
		return fmt.Errorf("mark routed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// pode já ter sido roteada por outro consumer; idempotente.
		return nil
	}
	return nil
}

// MarkNotified avança a mensagem para status='notified' (broadcast WS entregue).
// Idempotente.
func (r *InboundEventsRepo) MarkNotified(ctx context.Context, id domain.MessageID) error {
	_, err := r.platformPool.Exec(ctx,
		`UPDATE messages
		 SET status = 'notified',
		     notified_at = NOW(),
		     updated_at = NOW()
		 WHERE id = $1 AND status IN ('routed', 'notified')`,
		string(id),
	)
	if err != nil {
		return fmt.Errorf("mark notified: %w", err)
	}
	return nil
}

// CountUnrouted retorna o número de mensagens em status='received'
// (métrica para reconciler_lag gauge).
func (r *InboundEventsRepo) CountUnrouted(ctx context.Context) (int, error) {
	var n int
	err := r.platformPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM messages WHERE status = 'received'`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count unrouted: %w", err)
	}
	return n, nil
}
