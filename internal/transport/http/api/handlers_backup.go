// Package api — handlers_backup.go: endpoints REST para backup/restore/reset
// por tenant (issue #85).
//
// Endpoints:
//
//	POST   /api/admin/tenants/{id}/backup         → inicia export
//	GET    /api/admin/backup-jobs/{id}            → status do job
//	POST   /api/admin/tenants/{id}/restore        → inicia restore
//	POST   /api/admin/tenants/{id}/reset          → inicia reset (com senha)
//
// Auth: Bearer JWT com claim "scope" contendo "admin:backup" (placeholder
// até Fase 5 RBAC). Bearer, não cookie → CSRF não se aplica (RFC 7231).
// Audit: cada ação registra uma entry em admin_audit_log (D17).

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
)

// BackupHandlers agrupa os handlers de backup.
type BackupHandlers struct {
	backup   *ucbackup.Service
	verifier ucbackup.AdminVerifier
}

// NewBackupHandlers cria os handlers.
func NewBackupHandlers(backup *ucbackup.Service, verifier ucbackup.AdminVerifier) *BackupHandlers {
	return &BackupHandlers{backup: backup, verifier: verifier}
}

// Register monta as rotas no router.
func (h *BackupHandlers) Register(r chi.Router) {
	r.Route("/admin/tenants/{id}", func(r chi.Router) {
		r.Post("/backup", h.startBackup)
		r.Post("/restore", h.startRestore)
		r.Post("/reset", h.startReset)
	})
	r.Get("/admin/backup-jobs/{id}", h.jobStatus)
}

// actorFromContext extrai o actor do request (para audit). Em produção,
// o principal vem do JWT (claim sub/email); aqui usamos placeholders.
func actorFromRequest(r *http.Request) ucadmin.Actor {
	email := r.Header.Get("X-Admin-Email")
	if email == "" {
		email = "unknown@admin"
	}
	return ucadmin.Actor{
		Email: email,
		IP:    r.RemoteAddr,
	}
}

func (h *BackupHandlers) startBackup(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant id required")
		return
	}
	actor := actorFromRequest(r)
	res, err := h.backup.Export(r.Context(), ucbackup.ExportRequest{
		TenantID:     tenantID,
		Actor:        cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		IncludeMedia: true,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func (h *BackupHandlers) startRestore(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant id required")
		return
	}
	var body struct {
		BackupID string `json:"backup_id"`
		DryRun   bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if body.BackupID == "" {
		writeError(w, http.StatusBadRequest, "backup_id required")
		return
	}
	actor := actorFromRequest(r)
	res, err := h.backup.Restore(r.Context(), ucbackup.RestoreRequest{
		TenantID: tenantID,
		BackupID: body.BackupID,
		Actor:    cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		DryRun:   body.DryRun,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func (h *BackupHandlers) startReset(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant id required")
		return
	}
	var body struct {
		ConfirmText   string `json:"confirm_text"`
		AdminPassword string `json:"admin_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	actor := actorFromRequest(r)
	res, err := h.backup.Reset(r.Context(), ucbackup.ResetRequest{
		TenantID:      tenantID,
		Actor:         cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		ConfirmText:   body.ConfirmText,
		AdminPassword: body.AdminPassword,
	}, h.verifier)
	if err != nil {
		if errors.Is(err, ucbackup.ErrResetRequiresConfirmText) {
			writeError(w, http.StatusBadRequest, "confirmation text must be \"RESET\"")
			return
		}
		if errors.Is(err, ucbackup.ErrResetRequiresPassword) {
			writeError(w, http.StatusBadRequest, "admin_password required")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func (h *BackupHandlers) jobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job id required")
		return
	}
	job, err := h.backup.Job(jobID)
	if err != nil {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
