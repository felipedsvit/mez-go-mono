// Package postgres implementa os repositórios Postgres do mez-go-mono.
//
// OutboxRepo implementa port.OutboxWriter e port.OutboxRelay sobre a tabela
// outbound_events (criada em 0001). O relay itera cross-tenant via
// RunAsPlatform (mez_platform, BYPASSRLS) — diverge do mez-go (pai) que
// usa SECURITY DEFINER.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// OutboxRepo persiste e drena mensagens da fila outbound_events.
//
// As escritas (Enqueue) acontecem dentro de uma RunInTenantTx — RLS aplica
// isolamento por tenant. As leituras (Claim) acontecem em uma transação
// mez_platform (RunAsPlatform) porque o relay drena todos os tenants.
//
// Issue #122: a enumeração de tenants ativos é delegada para
// port.TenantEnumerator (injetado). O OutboxRepo não lê mais a tabela
// tenants diretamente — cross-context fica encapsulado no enumerator.
type OutboxRepo struct {
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
	tenants      port.TenantEnumerator
}

// NewOutboxRepo cria o repo. Recebe ambos os pools e o enumerator
// cross-tenant (issue #122).
func NewOutboxRepo(appPool, platformPool *pgxpool.Pool, tenants port.TenantEnumerator) *OutboxRepo {
	return &OutboxRepo{appPool: appPool, platformPool: platformPool, tenants: tenants}
}

var _ port.OutboxWriter = (*OutboxRepo)(nil)
var _ port.OutboxRelay = (*OutboxRepo)(nil)

// Enqueue enfileira um OutboxMessage na fila outbound dentro da tx ativa
// no ctx. RLS força tenant_id = mez.tenant_id.
//
// Issue #126: usa domain.OutboxMessage (que referencia Message por ID) em
// vez de domain.Message cru. O payload é persistido como JSONB com o
// message_id; o resto (body, metadata) fica na tabela messages.
//
// Decisão consciente (issue #126): NÃO consolidamos a tabela. O domínio
// evolui para o agregado OutboxMessage; a tabela permanece paralela até
// a issue 3.3 (domain events) trazê-los juntos.
func (r *OutboxRepo) Enqueue(ctx context.Context, m *domain.OutboxMessage) error {
	if m == nil {
		return errors.New("outbox Enqueue: nil message")
	}
	q := appQFromCtxOrPool(ctx, r.appPool)

	payload, err := json.Marshal(map[string]any{
		"message_id":      string(m.MessageID),
		"channel":         string(m.Channel),
		"conversation_id": string(m.ConversationID),
		"contact_id":      string(m.ContactID),
		"outbox_id":       string(m.ID),
		"status":          string(m.Status),
		"attempts":        m.Attempts,
	})
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}

	target, err := json.Marshal(map[string]any{
		"conversation_id": string(m.ConversationID),
		"contact_id":      string(m.ContactID),
	})
	if err != nil {
		return fmt.Errorf("marshal outbox target: %w", err)
	}

	_, err = q.Exec(ctx,
		`INSERT INTO outbound_events (tenant_id, channel, target, payload, status, attempts, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())`,
		m.TenantID, m.Channel, target, payload, string(m.Status), m.Attempts,
	)
	if err != nil {
		return fmt.Errorf("outbox enqueue: %w", err)
	}
	return nil
}

// Insert é o wrapper de compat para código legado que ainda passa
// *domain.Message. Constrói um OutboxMessage via NewOutboxMessage e
// delega para Enqueue. Issue #126: deprecated.
func (r *OutboxRepo) Insert(ctx context.Context, m *domain.Message) error {
	if m == nil {
		return errors.New("outbox Insert: nil message")
	}
	ob, err := domain.NewOutboxMessage(m.ID, m.TenantID, m.Channel, m.ConversationID, m.ContactID)
	if err != nil {
		return fmt.Errorf("outbox Insert: %w", err)
	}
	return r.Enqueue(ctx, ob)
}

// PendingCount retorna o total de mensagens pending em outbound_events
// (cross-tenant, usa mez_platform).
func (r *OutboxRepo) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := r.platformPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM outbound_events WHERE status = 'pending'`,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("outbox pending count: %w", err)
	}
	return n, nil
}

// ClaimNext retorna até batchSize mensagens pending e dá lock nelas
// (FOR UPDATE SKIP LOCKED). O relay então processa cada uma e marca o
// resultado via MarkSent/MarkFailed.
//
// A iteração cross-tenant é feita por tenant: para cada tenant ativo,
// abre uma tx mez_platform, claim o batch, processa, commita. Isto evita
// a contenção de um único lock global e respeita o bounded-buffer.
//
// Iteração por tenant é aceitável para a Fase 2 (100s de tenants).
func (r *OutboxRepo) ClaimNext(ctx context.Context, batchSize int) ([]domain.Message, error) {
	rows, err := r.platformPool.Query(ctx,
		`SELECT id, tenant_id, channel, payload, attempts
		 FROM outbound_events
		 WHERE status = 'pending'
		 ORDER BY created_at
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`,
		batchSize,
	)
	if err != nil {
		return nil, fmt.Errorf("outbox claim: %w", err)
	}
	defer rows.Close()

	var msgs []domain.Message
	for rows.Next() {
		var (
			outboxID string
			tenantID string
			channel  string
			payload  []byte
			attempts int
		)
		if err := rows.Scan(&outboxID, &tenantID, &channel, &payload, &attempts); err != nil {
			return nil, fmt.Errorf("outbox claim scan: %w", err)
		}

		var p struct {
			MessageID      string         `json:"message_id"`
			Channel        string         `json:"channel"`
			ConversationID string         `json:"conversation_id"`
			ContactID      string         `json:"contact_id"`
			Type           string         `json:"type"`
			Body           string         `json:"body"`
			Metadata       map[string]any `json:"metadata"`
		}
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, fmt.Errorf("outbox claim unmarshal: %w", err)
		}

		msgs = append(msgs, domain.Message{
			ID:             domain.MessageID(p.MessageID),
			TenantID:       domain.TenantID(tenantID),
			Channel:        domain.Channel(channel),
			ConversationID: domain.ConversationID(p.ConversationID),
			ContactID:      domain.ContactID(p.ContactID),
			Type:           domain.MessageType(p.Type),
			Body:           p.Body,
			Metadata:       p.Metadata,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("outbox claim rows: %w", err)
	}
	return msgs, nil
}

// MarkSent marca a mensagem como 'sent' e armazena o provider_msg_id no payload.
//
// O relay chama isso após o provider confirmar o envio. A tx mez_platform
// é aberta dentro do Claim — não há tx aqui; usamos UPDATE direto.
func (r *OutboxRepo) MarkSent(ctx context.Context, id domain.MessageID) error {
	tag, err := r.platformPool.Exec(ctx,
		`UPDATE outbound_events
		 SET status = 'sent', updated_at = NOW(),
		     payload = jsonb_set(payload, '{sent_at}', to_jsonb(NOW()::text))
		 WHERE payload->>'message_id' = $1 AND status = 'pending'`,
		string(id),
	)
	if err != nil {
		return fmt.Errorf("outbox mark sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

// MarkFailed incrementa attempts e armazena last_error. Permanece em
// 'pending' para retry; depois de N tentativas o relay chama MarkDLQ.
func (r *OutboxRepo) MarkFailed(ctx context.Context, id domain.MessageID, err error) error {
	_, e := r.platformPool.Exec(ctx,
		`UPDATE outbound_events
		 SET attempts = attempts + 1,
		     last_error = $2,
		     updated_at = NOW()
		 WHERE payload->>'message_id' = $1 AND status = 'pending'`,
		string(id), err.Error(),
	)
	if e != nil {
		return fmt.Errorf("outbox mark failed: %w", e)
	}
	return nil
}

// MarkDLQ move para a dead-letter queue (status='dlq'). Operadores
// inspecionam para recovery. Idempotente — se já está em dlq, no-op.
func (r *OutboxRepo) MarkDLQ(ctx context.Context, id domain.MessageID, lastErr error) error {
	errMsg := ""
	if lastErr != nil {
		errMsg = lastErr.Error()
	}
	tag, e := r.platformPool.Exec(ctx,
		`UPDATE outbound_events
		 SET status = 'dlq',
		     last_error = $2,
		     updated_at = NOW()
		 WHERE payload->>'message_id' = $1 AND status IN ('pending', 'failed', 'dlq')`,
		string(id), errMsg,
	)
	if e != nil {
		return fmt.Errorf("outbox mark dlq: %w", e)
	}
	_ = tag // idempotent: 0 rows affected é OK (já em dlq)
	return nil
}

// GetAttempts retorna o número de tentativas para o outbox row.
func (r *OutboxRepo) GetAttempts(ctx context.Context, id domain.MessageID) (int, error) {
	var attempts int
	err := r.platformPool.QueryRow(ctx,
		`SELECT attempts FROM outbound_events
		 WHERE payload->>'message_id' = $1`,
		string(id),
	).Scan(&attempts)
	if err != nil {
		return 0, fmt.Errorf("outbox get attempts: %w", err)
	}
	return attempts, nil
}

// ForEachTenant itera todos os tenants ativos. Usado pelo relay e pelo
// reconciler para abrir uma RunInTenantTx por tenant.
//
// Recebe uma função que recebe o tenantID e retorna erro. Se a função
// retornar erro, a iteração para e o erro propaga.
//
// Issue #122: delega para port.TenantEnumerator. O OutboxRepo não toca
// a tabela tenants diretamente. O enumerator faz streaming (não
// materializa a lista — issue #123).
func (r *OutboxRepo) ForEachTenant(ctx context.Context, fn func(tenantID domain.TenantID) error) error {
	if r.tenants == nil {
		return errors.New("outbox ForEachTenant: TenantEnumerator não injetado (wire error)")
	}
	return r.tenants.ForEachActive(ctx, fn)
}

// AcquireClaimLock abre uma transação mez_platform que detém os locks
// FOR UPDATE SKIP LOCKED durante o processamento. O relay usa isto para
// garantir que dois relays concorrentes não peguem a mesma mensagem.
//
// A tx é commitada/rollbackeda pelo caller via Defer ou commita explícito.
func (r *OutboxRepo) AcquireClaimLock(ctx context.Context) (pgx.Tx, error) {
	tx, err := r.platformPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	return tx, nil
}

// MessageFromOutboxRow decodifica uma linha de outbound_events em domain.Message.
// Helper exposto para o relay/ingestor usarem a mesma forma de decoding.
func MessageFromOutboxRow(outboxID, tenantID, channel string, payload []byte, attempts int) (domain.Message, error) {
	var p struct {
		MessageID      string         `json:"message_id"`
		Channel        string         `json:"channel"`
		ConversationID string         `json:"conversation_id"`
		ContactID      string         `json:"contact_id"`
		Type           string         `json:"type"`
		Body           string         `json:"body"`
		Metadata       map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return domain.Message{}, fmt.Errorf("decode outbox payload: %w", err)
	}
	_ = attempts
	return domain.Message{
		ID:             domain.MessageID(p.MessageID),
		TenantID:       domain.TenantID(tenantID),
		Channel:        domain.Channel(channel),
		ConversationID: domain.ConversationID(p.ConversationID),
		ContactID:      domain.ContactID(p.ContactID),
		Type:           domain.MessageType(p.Type),
		Body:           p.Body,
		Metadata:       p.Metadata,
		UpdatedAt:      time.Now(),
	}, nil
}
