// Package backup — reset.go: reset com confirmação dupla (issue #83).
//
// Confirmação dupla (D16 do PLAN):
//   1. Texto literal "RESET" (não pode estar vazio).
//   2. Senha do admin re-checada (Argon2 contra admin_users.password_hash).
//
// Operação:
//   1. Whatsmeow Manager.Disconnect(tenantID) — derruba o client imediatamente.
//   2. RunAsPlatform → DELETE FROM whatsapp_* (cross-tenant; mez_app não vê
//      sem tenant_id setado, e RLS é fail-closed no mez_app).
//   3. RunInTenantTx → DELETE FROM <tabela> WHERE tenant_id=$1 para as outras.
//   4. S3 DeletePrefix para tenants/<id>/ (mídia) e tenants/<id>/backups/.
//   5. Audit log (D17).

package backup

import (
	"context"
	"fmt"
	"time"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/auth"
)

// AdminVerifier é a abstração para verificar a senha do admin (Argon2).
// Implementada por usecase/auth.LoginService (LoginLocal) — fail-closed
// após lockout.
type AdminVerifier interface {
	LoginLocal(ctx context.Context, input auth.LoginInput) (auth.LoginResult, error)
}

// ResetRequest é o input do reset.
type ResetRequest struct {
	TenantID     string
	Actor        cdomain.Actor
	ConfirmText  string // deve ser literal "RESET"
	AdminPassword string
}

// ResetResult é o output.
type ResetResult struct {
	JobID        string `json:"job_id"`
	TablesCleared int   `json:"tables_cleared"`
	S3Removed    int    `json:"s3_removed"`
}

// Reset inicia o job em goroutine.
func (s *Service) Reset(ctx context.Context, req ResetRequest, verifier AdminVerifier) (*ResetResult, error) {
	if req.TenantID == "" {
		return nil, fmt.Errorf("tenant_id é obrigatório")
	}
	if req.Actor.Email == "" {
		return nil, fmt.Errorf("actor.email é obrigatório")
	}
	if req.ConfirmText != "RESET" {
		return nil, ErrResetRequiresConfirmText
	}
	if req.AdminPassword == "" {
		return nil, ErrResetRequiresPassword
	}

	// Verifica senha ANTES de enfileirar o job (fail-fast).
	if verifier != nil {
		_, err := verifier.LoginLocal(ctx, auth.LoginInput{
			Email:    req.Actor.Email,
			Password: req.AdminPassword,
			IP:       req.Actor.IP,
		})
		if err != nil {
			return nil, fmt.Errorf("senha do admin inválida: %w", err)
		}
	}

	job := &Job{
		ID:        newJobID(),
		Kind:      JobReset,
		TenantID:  req.TenantID,
		Actor:     req.Actor.Email,
		State:     StatePending,
		StartedAt: now(),
		UpdatedAt: now(),
	}
	s.jobs.Put(job)

	go s.runReset(job, req)

	return &ResetResult{JobID: job.ID}, nil
}

func (s *Service) runReset(job *Job, req ResetRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			s.markFailed(job, fmt.Errorf("panic: %v", r))
		}
	}()

	updateJob(job, func() {
		job.State = StateRunning
		job.CurrentStep = "disconnect whatsmeow"
		job.UpdatedAt = now()
	})
	s.jobs.Put(job)

	// 1. Desconecta o client whatsmeow (best-effort — pode não existir).
	if s.disconnector != nil {
		if err := s.disconnector.Disconnect(ctx, domain.TenantID(req.TenantID)); err != nil {
			s.log.Warn().Err(err).Str("tenant", req.TenantID).Msg("reset: whatsmeow disconnect")
		}
	}

	// 2. Tabelas whatsmeow (cross-tenant) — usa platformPool (BYPASSRLS).
	//    whatsapp_history, whatsapp_session_keys, whatsapp_account_state.
	whatsmeowTables := []string{
		"whatsapp_history",
		"whatsapp_session_keys",
		"whatsapp_account_state",
	}
	crossCleared := 0
	if s.platformPool != nil {
		for _, t := range whatsmeowTables {
			tag, err := s.platformPool.Exec(ctx,
				"DELETE FROM "+t+" WHERE tenant_id = $1", req.TenantID)
			if err != nil {
				s.markFailed(job, fmt.Errorf("platform delete %s: %w", t, err))
				return
			}
			crossCleared += int(tag.RowsAffected())
		}
	}

	// 3. Outras tabelas via RunInTenantTx (GUC mez.tenant_id setado).
	//    As 6 tabelas tenant-escopadas (excluindo whatsmeow já feito):
	tenantTables := []string{
		"messages",
		"conversations",
		"outbound_events",
		"inbound_events",
		"channel_credentials",
		"contacts",
	}
	tenantCleared := 0
	tx, txCtx, err := s.tx.BeginTenantTx(ctx, domain.TenantID(req.TenantID))
	if err != nil {
		s.markFailed(job, fmt.Errorf("begin tenant tx: %w", err))
		return
	}
	for _, t := range tenantTables {
		tag, err := tx.Exec(txCtx, "DELETE FROM "+t+" WHERE tenant_id = $1", req.TenantID)
		if err != nil {
			_ = tx.Rollback(ctx)
			s.markFailed(job, fmt.Errorf("delete %s: %w", t, err))
			return
		}
		tenantCleared += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		s.markFailed(job, fmt.Errorf("commit tenant tx: %w", err))
		return
	}

	// 4. S3: limpa tenants/<id>/ (mídia) + tenants/<id>/backups/ (backups).
	mediaPrefix := fmt.Sprintf("tenants/%s/", req.TenantID)
	backupPrefix := fmt.Sprintf("tenants/%s/backups/", req.TenantID)
	mediaRemoved, err := s.store.DeletePrefix(ctx, s.store.MediaBucket(), mediaPrefix)
	if err != nil {
		s.log.Warn().Err(err).Msg("reset: delete media prefix")
	}
	backupRemoved, err := s.store.DeletePrefix(ctx, s.store.BackupBucket(), backupPrefix)
	if err != nil {
		s.log.Warn().Err(err).Msg("reset: delete backup prefix")
	}
	totalS3 := mediaRemoved + backupRemoved

	updateJob(job, func() {
		job.ProgressPct = 90
		job.UpdatedAt = now()
	})

	// 5. Audit log — Issue #148 (H5, CWE-778): atômico via
	// RunAsPlatform. C5 row (platform:access) commitada com a
	// mutation row (tenant.reset com metadata rico).
	if err := s.runAsPlatform(ctx, req.Actor, cdomain.ActionTenantReset, req.TenantID, "tenant", req.TenantID,
		map[string]any{
			"tables_cleared": crossCleared + tenantCleared,
			"media_removed":  mediaRemoved,
			"backup_removed": backupRemoved,
			"s3_removed":     totalS3,
		},
		func(ctx context.Context) error {
			if s.audit == nil {
				return nil
			}
			entry := &cdomain.AuditEntry{
				ActorID:    req.Actor.ID,
				ActorEmail: req.Actor.Email,
				Action:     cdomain.ActionTenantReset,
				TargetType: "tenant",
				TargetID:   req.TenantID,
				TenantID:   req.TenantID,
				Metadata: map[string]any{
					"tables_cleared": crossCleared + tenantCleared,
					"media_removed":  mediaRemoved,
					"backup_removed": backupRemoved,
					"s3_removed":     totalS3,
				},
				IP:        req.Actor.IP,
				CreatedAt: now(),
			}
			return s.audit.Record(ctx, entry)
		}); err != nil {
		s.log.Warn().Err(err).Str("tenant", req.TenantID).Msg("backup: audit atômico falhou (best-effort segue)")
	}

	finished := now()
	updateJob(job, func() {
		job.State = StateDone
		job.ProgressPct = 100
		job.CurrentStep = "done"
		job.FinishedAt = &finished
		job.UpdatedAt = finished
		job.Tables = append(job.Tables, TableProgress{
			Name:  fmt.Sprintf("cleared %d rows", crossCleared+tenantCleared),
			State: StateDone,
		})
	})
	s.jobs.Put(job)
}
