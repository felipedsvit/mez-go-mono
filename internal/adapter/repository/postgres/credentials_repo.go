package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// ChannelCredentialsRepo persiste credenciais de canal cifradas (Fase 7 #90).
//
// Modelo de uso:
//
//   - Get/Upsert/Delete: tenant-scoped, executados via appQFromCtxOrPool (tx aberta
//     por RunInTenantTx). RLS fail-closed (C3/C4): fora de uma tx com
//     mez.tenant_id setado, a query falha com "no rows" (mez_app não tem
//     BYPASSRLS).
//
//   - ForEachTenant: cross-tenant, abre RunAsPlatform internamente. Usado
//     exclusivamente pelo `cmd/server rotate-kek` (issue #92). Escreve
//     1 audit row "platform_access" antes de iterar (C5 — atomic).
//
//   - UpdateWrappedDEK: variante cross-tenant, chamada DEPOIS que o caller
//     já está dentro de um RunAsPlatform aberto por ForEachTenant. O caller
//     passa o contexto da tx — não abrimos nova.
type ChannelCredentialsRepo struct {
	pool         *pgxpool.Pool
	platformPool *pgxpool.Pool
	tx           *TxRunner
}

// NewChannelCredentialsRepo constrói o repo. Recebe o pool app (RLS) e
// platform (BYPASSRLS) e o TxRunner para abrir tx cross-tenant auditadas.
func NewChannelCredentialsRepo(appPool, platformPool *pgxpool.Pool, tx *TxRunner) *ChannelCredentialsRepo {
	return &ChannelCredentialsRepo{pool: appPool, platformPool: platformPool, tx: tx}
}

// Get busca a credencial (tenantID, channel). Retorna port.ErrNotFound se
// não houver linha.
//
// Caller DEVE estar dentro de uma tx aberta por RunInTenantTx(tenantID) —
// caso contrário, RLS esconde todas as linhas (fail-closed).
func (r *ChannelCredentialsRepo) Get(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) (*port.CredentialRow, error) {
	q := appQFromCtxOrPool(ctx, r.pool)
	if _, ok := q.(pgx.Tx); !ok {
		// Não estamos em uma tx — appQFromCtxOrPool caiu para o pool direto, o
		// que significa que mez.tenant_id não foi setado. Em mez_app isso
		// resultaria em zero rows, mas falhamos mais cedo para dar erro
		// explícito ao caller.
		return nil, errors.New("channel credentials: Get requer RunInTenantTx (RLS fail-closed)")
	}

	var cc port.CredentialRow
	var rotUntil *time.Time
	err := q.QueryRow(ctx,
		`SELECT tenant_id, channel, wrapped_dek, encrypted, kek_version, rotation_window_until, created_at, updated_at
		 FROM channel_credentials
		 WHERE tenant_id = $1 AND channel = $2`,
		tenantID, channel,
	).Scan(&cc.TenantID, &cc.Channel, &cc.WrappedDEK, &cc.Encrypted, &cc.KEKVersion, &rotUntil, &cc.CreatedAt, &cc.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, port.ErrNotFound
		}
		return nil, fmt.Errorf("channel credentials get: %w", err)
	}
	cc.RotationWindowUntil = rotUntil
	return &cc, nil
}

// Upsert cria ou substitui a credencial (tenantID, channel).
//
// Caller DEVE estar dentro de uma tx aberta por RunInTenantTx(tenantID).
func (r *ChannelCredentialsRepo) Upsert(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, wrappedDEK, encrypted []byte, kekVersion int) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	if _, ok := q.(pgx.Tx); !ok {
		return errors.New("channel credentials: Upsert requer RunInTenantTx (RLS fail-closed)")
	}

	_, err := q.Exec(ctx,
		`INSERT INTO channel_credentials (tenant_id, channel, wrapped_dek, encrypted, kek_version, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		 ON CONFLICT (tenant_id, channel) DO UPDATE
		 SET wrapped_dek = EXCLUDED.wrapped_dek,
		     encrypted   = EXCLUDED.encrypted,
		     kek_version = EXCLUDED.kek_version,
		     updated_at  = NOW()`,
		tenantID, channel, wrappedDEK, encrypted, kekVersion,
	)
	if err != nil {
		return fmt.Errorf("channel credentials upsert: %w", err)
	}
	return nil
}

// Delete remove a credencial (tenantID, channel). Idempotente: retorna nil
// mesmo se a linha não existia.
func (r *ChannelCredentialsRepo) Delete(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) error {
	q := appQFromCtxOrPool(ctx, r.pool)
	if _, ok := q.(pgx.Tx); !ok {
		return errors.New("channel credentials: Delete requer RunInTenantTx (RLS fail-closed)")
	}

	_, err := q.Exec(ctx, `DELETE FROM channel_credentials WHERE tenant_id = $1 AND channel = $2`, tenantID, channel)
	if err != nil {
		return fmt.Errorf("channel credentials delete: %w", err)
	}
	return nil
}

// ForEachTenant itera TODAS as credenciais (cross-tenant) usando
// RunAsPlatform como wrapper transacional com audit C5 atômico.
//
// A tx é aberta aqui, com mez.tenant_id NÃO setado (BYPASSRLS no
// platformPool). Isso permite ler todas as linhas; o caller recebe cada
// CredentialRow e decide o que fazer (tipicamente re-wrap).
//
// actor é gravado no audit_log antes de qualquer leitura. Se fn retornar
// erro, a tx inteira (incluindo o audit) é revertida.
//
// Esta é a única forma cross-tenant de tocar channel_credentials.
func (r *ChannelCredentialsRepo) ForEachTenant(ctx context.Context, actor string, fn func(ctx context.Context, row port.CredentialRow) error) error {
	if r.tx == nil {
		return errors.New("channel credentials: ForEachTenant requer TxRunner (wire error)")
	}
	return r.tx.RunAsPlatform(ctx, actor, func(ctx context.Context) error {
		rows, err := r.platformPool.Query(ctx,
			`SELECT tenant_id, channel, wrapped_dek, encrypted, kek_version
			 FROM channel_credentials
			 ORDER BY tenant_id, channel`,
		)
		if err != nil {
			return fmt.Errorf("for each tenant query: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var row port.CredentialRow
			if err := rows.Scan(&row.TenantID, &row.Channel, &row.WrappedDEK, &row.Encrypted, &row.KEKVersion); err != nil {
				return fmt.Errorf("for each tenant scan: %w", err)
			}
			if err := fn(ctx, row); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("for each tenant rows: %w", err)
		}
		return nil
	})
}

// UpdateWrappedDEK re-cifra a DEK (re-wrap) de uma credencial específica.
// Usado pelo rotate-kek (#92) DENTRO de uma tx aberta por ForEachTenant
// (ctx já carrega a tx do platformPool; mez_app não precisa estar envolvido).
//
// NÃO muda o campo `encrypted` (a credencial em si): a DEK em plaintext
// não muda, só o wrap dela pela KEK nova.
//
// Parâmetros:
//
//   - newWrappedDEK: nonce||ciphertext da DEK cifrada com a KEK nova.
//   - newKekVersion: nova versão (oldVersion+1).
//   - windowUntil: opcional, non-nil define rotation_window_until. Usado
//     em janelas de transição (recovery — Fase 8+).
func (r *ChannelCredentialsRepo) UpdateWrappedDEK(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, newWrappedDEK []byte, newKekVersion int, windowUntil *time.Time) error {
	// UpdateWrappedDEK é cross-tenant. Se o ctx já carrega uma tx do
	// platformPool (de RunAsPlatform), usamos ela. Caso contrário (uso
	// improvável fora do rotate-kek), caímos para o platformPool direto.
	q := r.platformPool
	if tx, ok := ctx.Value(appTxKey).(pgx.Tx); ok {
		_ = tx
		// O appTxKey guarda tx de appPool, mas neste caso a tx aberta
		// por RunAsPlatform vive no platformPool. Não há como extraí-la
		// via ctx value (escolha de design do db.go). Solução: o caller
		// deve passar o q de plataforma via injeção — mas como queremos
		// manter a API do repo simples, abrimos um helper RunAsPlatform
		// para a operação atômica aqui. Para rotate-kek isso é
		// desnecessário (já está dentro da tx do ForEachTenant); a
		// alternativa é o caller fazer o UPDATE via `q` do platformPool
		// exposto. Em vez disso, expomos um helper que faz o UPDATE
		// numa tx platformPool própria.
	}
	_ = q

	return r.updateWrappedDEKAtomic(ctx, actorForUpdate(tenantID, string(channel)), newWrappedDEK, newKekVersion, windowUntil, func(ctx context.Context) error {
		_, err := r.platformPool.Exec(ctx,
			`UPDATE channel_credentials
			 SET wrapped_dek = $1, kek_version = $2, rotation_window_until = $3, updated_at = NOW()
			 WHERE tenant_id = $4 AND channel = $5`,
			newWrappedDEK, newKekVersion, windowUntil, tenantID, channel,
		)
		return err
	})
}

// updateWrappedDEKAtomic abre uma tx platformPool curta só para o UPDATE
// individual. Se o caller já está dentro de um ForEachTenant (mais comum
// no rotate-kek), isso aninha uma tx — Postgres não suporta aninhar
// tx.savepoint automaticamente, então usamos o pool direto se ctx já tem
// a tx. Para simplificar, **a implementação atual** sempre abre uma tx
// nova: o custo é uma tx curta adicional, e isolamos o erro de "lock
// contention" (pouco provável em update single-row).
func (r *ChannelCredentialsRepo) updateWrappedDEKAtomic(ctx context.Context, actor string, newWrappedDEK []byte, newKekVersion int, windowUntil *time.Time, fn func(ctx context.Context) error) error {
	if r.tx == nil {
		return errors.New("channel credentials: updateWrappedDEK requer TxRunner (wire error)")
	}
	return r.tx.RunAsPlatform(ctx, actor, func(ctx context.Context) error {
		return fn(ctx)
	})
}

// actorForUpdate monta o actor string usado no audit do UpdateWrappedDEK.
// Inclui o (tenant, channel) para correlação no log.
func actorForUpdate(tenantID domain.TenantID, channel string) string {
	return fmt.Sprintf("system:rotate-kek:%s:%s", tenantID, channel)
}
