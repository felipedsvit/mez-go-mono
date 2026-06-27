// Package postgres — whatsapp_account_state.go: CRUD do warmup state.
//
// Tabela: whatsapp_account_state (migration 0004).
// FORCE RLS (C3) + mez_app sem BYPASSRLS (C4) — escrita requer
// RunInTenantTx. mez_platform pode ler cross-tenant (RunAsPlatform).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// AccountState é o estado de warmup/saúde de uma conta whatsmeow.
type AccountState struct {
	TenantID      domain.TenantID
	JID           string
	DayAnchor     time.Time
	DaySentCount  int
	HealthScore   int
	TimelockUntil time.Time
	BannedAt      time.Time
	LastError     string
}

// WhatsAppStateRepo persiste o warmup state por (tenant, jid).
type WhatsAppStateRepo struct {
	appPool      *pgxpool.Pool
	platformPool *pgxpool.Pool
}

// NewWhatsAppStateRepo cria o repo. Recebe os 2 pools: appPool (RLS)
// para path normal e platformPool (BYPASSRLS) para RunAsPlatform cross-tenant.
func NewWhatsAppStateRepo(appPool, platformPool *pgxpool.Pool) *WhatsAppStateRepo {
	return &WhatsAppStateRepo{appPool: appPool, platformPool: platformPool}
}

// LoadState carrega o estado de um (tenant, jid). Retorna zero value
// (HealthScore=100) se não existir.
func (r *WhatsAppStateRepo) LoadState(ctx context.Context, tenantID domain.TenantID, jid string) (AccountState, error) {
	q := appQFromCtx(ctx, r.appPool)
	var st AccountState
	var dayAnchor time.Time
	var timelock, banned *time.Time
	err := q.QueryRow(ctx,
		`SELECT tenant_id, jid, day_anchor, day_sent_count, health_score,
		        timelock_until, banned_at, COALESCE(last_error, '')
		 FROM whatsapp_account_state
		 WHERE tenant_id = $1 AND jid = $2`,
		tenantID, jid,
	).Scan(&st.TenantID, &st.JID, &dayAnchor, &st.DaySentCount, &st.HealthScore,
		&timelock, &banned, &st.LastError)
	if err != nil {
		// pgx.ErrNoRows → estado zero.
		if errors.Is(err, pgx.ErrNoRows) {
			return AccountState{
				TenantID:    tenantID,
				JID:         jid,
				DayAnchor:   time.Now().UTC().Truncate(24 * time.Hour),
				HealthScore: 100,
			}, nil
		}
		return AccountState{}, fmt.Errorf("load state: %w", err)
	}
	st.DayAnchor = dayAnchor
	if timelock != nil {
		st.TimelockUntil = *timelock
	}
	if banned != nil {
		st.BannedAt = *banned
	}
	return st, nil
}

// SaveState upsert do estado.
func (r *WhatsAppStateRepo) SaveState(ctx context.Context, st AccountState) error {
	q := appQFromCtx(ctx, r.appPool)
	var timelock, banned *time.Time
	if !st.TimelockUntil.IsZero() {
		timelock = &st.TimelockUntil
	}
	if !st.BannedAt.IsZero() {
		banned = &st.BannedAt
	}
	_, err := q.Exec(ctx,
		`INSERT INTO whatsapp_account_state
		    (tenant_id, jid, day_anchor, day_sent_count, health_score,
		     timelock_until, banned_at, last_error, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		 ON CONFLICT (tenant_id, jid) DO UPDATE SET
		    day_anchor = EXCLUDED.day_anchor,
		    day_sent_count = EXCLUDED.day_sent_count,
		    health_score = EXCLUDED.health_score,
		    timelock_until = EXCLUDED.timelock_until,
		    banned_at = EXCLUDED.banned_at,
		    last_error = EXCLUDED.last_error,
		    updated_at = NOW()`,
		st.TenantID, st.JID, st.DayAnchor, st.DaySentCount, st.HealthScore,
		timelock, banned, st.LastError,
	)
	if err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// LoadWarmupState implementa a interface whatsappStateSaver (subset).
func (r *WhatsAppStateRepo) LoadWarmupState(ctx context.Context, tenant, jid string) (int, time.Time, time.Time, int) {
	q := appQFromCtx(ctx, r.appPool)
	var ds, h int
	var anchor, tl time.Time
	err := q.QueryRow(ctx,
		`SELECT day_sent_count, day_anchor, COALESCE(timelock_until, 'epoch'::timestamptz), health_score
		 FROM whatsapp_account_state
		 WHERE tenant_id = $1 AND jid = $2`,
		tenant, jid,
	).Scan(&ds, &anchor, &tl, &h)
	if err != nil {
		return 0, time.Now().UTC().Truncate(24 * time.Hour), time.Time{}, 100
	}
	return ds, anchor, tl, h
}

// SaveWarmupState implementa a interface whatsappStateSaver (subset).
func (r *WhatsAppStateRepo) SaveWarmupState(ctx context.Context, tenant, jid string, ds int, anchor, timelock time.Time, health int) error {
	q := appQFromCtx(ctx, r.appPool)
	var tl *time.Time
	if !timelock.IsZero() {
		tl = &timelock
	}
	_, err := q.Exec(ctx,
		`INSERT INTO whatsapp_account_state
		    (tenant_id, jid, day_anchor, day_sent_count, health_score, timelock_until, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (tenant_id, jid) DO UPDATE SET
		    day_anchor = EXCLUDED.day_anchor,
		    day_sent_count = EXCLUDED.day_sent_count,
		    health_score = EXCLUDED.health_score,
		    timelock_until = EXCLUDED.timelock_until,
		    updated_at = NOW()`,
		tenant, jid, anchor, ds, health, tl,
	)
	if err != nil {
		return fmt.Errorf("save warmup state: %w", err)
	}
	return nil
}
