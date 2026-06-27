package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// TenantEnumerator é a implementação cross-context de port.TenantEnumerator
// (issue #122). Lê a tabela `tenants` (admin context) usando o platformPool
// (BYPASSRLS).
//
// IMPORTANTE: este adapter lê a tabela do admin context. É o ÚNICO local no
// adapter de messaging que toca essa tabela. O usecase (messaging.OutboxRepo,
// reconcile.Reconciler) consome via interface port.TenantEnumerator, sem
// conhecer a tabela.
type TenantEnumerator struct {
	platformPool *pgxpool.Pool
}

// NewTenantEnumerator constrói o enumerator. Recebe o platformPool
// (mez_platform com BYPASSRLS) — é o único pool com permissão cross-tenant.
func NewTenantEnumerator(platformPool *pgxpool.Pool) *TenantEnumerator {
	return &TenantEnumerator{platformPool: platformPool}
}

// Compile-time check: TenantEnumerator satisfaz port.TenantEnumerator.
var _ interface {
	ForEachActive(ctx context.Context, fn func(tenantID domain.TenantID) error) error
} = (*TenantEnumerator)(nil)

// ForEachActive itera os tenants ativos. Streaming — fn é chamado dentro do
// loop de rows.Next() (issue #3.12 — não materializa a lista).
//
// Se fn retornar context.Canceled ou context.DeadlineExceeded, a iteração
// para imediatamente. Outros erros são envoltos com o tenantID para
// diagnóstico e propagados.
func (e *TenantEnumerator) ForEachActive(ctx context.Context, fn func(tenantID domain.TenantID) error) error {
	rows, err := e.platformPool.Query(ctx,
		`SELECT id FROM tenants WHERE active = true ORDER BY created_at`,
	)
	if err != nil {
		return fmt.Errorf("tenant enumerator query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("tenant enumerator scan: %w", err)
		}
		if err := fn(domain.TenantID(id)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("tenant enumerator rows: %w", err)
	}
	return nil
}
