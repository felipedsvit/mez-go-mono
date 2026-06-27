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
//
// Issue #134 (C6 audit, DREAD 8.6): o actor (ID/email) usado no audit
// vem **exclusivamente** do JWT (sub/email claims), injetado pelo
// BearerAuth middleware. O header X-Admin-Email que permitia atribuir
// o ato a email arbitrário foi removido.

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
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

// actorFromContext extrai o actor do request (para audit). Issue #134:
// agora vem **exclusivamente** do JWT, injetado pelo BearerAuth
// middleware via ContextWithActor. O path do header X-Admin-Email
// (que permitia atribuir o ato a email arbitrário) foi removido.
//
// Se o actor não estiver presente, retorna erro 401 — defesa em
// profundidade caso um handler seja montado sem auth.
func actorFromContext(r *http.Request) (cdomain.Actor, error) {
	a, ok := ActorFromContext(r.Context())
	if !ok || (a.ID == "" && a.Email == "") {
		return cdomain.Actor{}, errors.New("actor required (JWT sub/email missing)")
	}
	return cdomain.Actor{
		ID:    cdomain.AdminUserID(a.ID),
		Email: a.Email,
		IP:    r.RemoteAddr,
	}, nil
}

func (h *BackupHandlers) startBackup(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant id required")
		return
	}
	actor, err := actorFromContext(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	res, err := h.backup.Export(r.Context(), ucbackup.ExportRequest{
		TenantID:     tenantID,
		Actor:        actor,
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
	actor, err := actorFromContext(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	res, err := h.backup.Restore(r.Context(), ucbackup.RestoreRequest{
		TenantID: tenantID,
		BackupID: body.BackupID,
		Actor:    actor,
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
	actor, err := actorFromContext(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	res, err := h.backup.Reset(r.Context(), ucbackup.ResetRequest{
		TenantID:      tenantID,
		Actor:         actor,
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
