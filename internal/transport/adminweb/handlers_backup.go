// Package adminweb — handlers_backup.go: UI de backup/restore por tenant (#86).
//
// Páginas:
//   - GET  /admin/tenants/{id}/backup         → formulário
//   - POST /admin/tenants/{id}/backup         → inicia export
//   - GET  /admin/tenants/{id}/backup/status  → fragmento htmx (poll 2s)
//   - GET  /admin/tenants/{id}/backup/list    → lista de backups existentes no S3
//   - POST /admin/tenants/{id}/restore        → inicia restore (form com backup_id)
//
// UX: htmx poll a cada 2s do status do job. CSRF token injetado no PageData
// pelo middleware e incluído nos forms.

package adminweb

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	ucadmin "github.com/felipedsvit/mez-go-mono/internal/usecase/admin"
	ucbackup "github.com/felipedsvit/mez-go-mono/internal/usecase/backup"
)

func (s *Server) handleBackupPage(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	principal := principalOrEmpty(r)

	// Lista backups existentes do S3.
	backups, _ := s.listBackupsForTenant(r.Context(), tenantID)

	data := PageData{
		Title:     "Backup — " + tenantID,
		Principal: principal,
		Now:       time.Now(),
		Data: map[string]any{
			"TenantID": tenantID,
			"Backups":  backups,
		},
	}
	s.renderPage(w, "backup.html", data)
}

func (s *Server) handleBackupStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	principal := principalOrEmpty(r)

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}
	res, err := s.backup.Export(r.Context(), ucbackup.ExportRequest{
		TenantID:     tenantID,
		Actor:        cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		IncludeMedia: true,
	})
	if err != nil {
		data := PageData{
			Title:     "Backup — " + tenantID,
			Principal: principal,
			Now:       time.Now(),
			Error:     "Erro ao iniciar backup: " + err.Error(),
			Data:      map[string]any{"TenantID": tenantID},
		}
		s.renderPage(w, "backup.html", data)
		return
	}

	// Redireciona para a página de status (htmx poll).
	s.redirect(w, r, fmt.Sprintf("/admin/tenants/%s/backup?job_id=%s&backup_id=%s",
		tenantID, res.JobID, res.BackupID))
}

func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		http.Error(w, "job_id required", http.StatusBadRequest)
		return
	}
	job, err := s.backup.Job(jobID)
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	// Renderiza fragmento (apenas content do template, sem base).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.render.Render(w, s.tpls, "backup_status.html", job); err != nil {
		s.log.Error().Err(err).Msg("render backup status")
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	backups, _ := s.listBackupsForTenant(r.Context(), tenantID)
	data := PageData{
		Title: "Backups — " + tenantID,
		Now:   time.Now(),
		Data:  backups,
	}
	s.renderPage(w, "backup_list.html", data)
}

func (s *Server) handleRestoreStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	backupID := r.FormValue("backup_id")
	principal := principalOrEmpty(r)

	actor := ucadmin.Actor{
		ID:    principal.UserID,
		Email: principal.Email,
		IP:    r.RemoteAddr,
	}
	res, err := s.backup.Restore(r.Context(), ucbackup.RestoreRequest{
		TenantID: tenantID,
		BackupID: backupID,
		Actor:    cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
	})
	if err != nil {
		s.renderPage(w, "backup.html", PageData{
			Title:     "Backup — " + tenantID,
			Principal: principal,
			Now:       time.Now(),
			Error:     "Erro ao iniciar restore: " + err.Error(),
			Data:      map[string]any{"TenantID": tenantID},
		})
		return
	}
	s.redirect(w, r, fmt.Sprintf("/admin/tenants/%s/backup?job_id=%s",
		tenantID, res.JobID))
}

// listBackupsForTenant lista os manifest.json disponíveis no prefixo
// tenants/<id>/backups/.
func (s *Server) listBackupsForTenant(ctx context.Context, tenantID string) ([]map[string]any, error) {
	// Implementação simplificada: lista objetos no bucket de backup sob
	// tenants/<id>/backups/ e agrupa por backupID. Para o primeiro
	// release, retornamos o backup atual do job store. Refactor: usar
	// o S3 Listing + parsear cada manifest.json.
	store := s.backup
	if store == nil {
		return nil, nil
	}
	// Lista os últimos jobs de export finalizados.
	jobs := store.ListJobs(50)
	out := []map[string]any{}
	for _, j := range jobs {
		if j.Kind != ucbackup.JobExport || j.TenantID != tenantID {
			continue
		}
		if j.State != ucbackup.StateDone {
			continue
		}
		out = append(out, map[string]any{
			"BackupID":   j.BackupID,
			"CreatedAt":  j.StartedAt,
			"Actor":      j.Actor,
			"State":      j.State,
			"TablesDone": len(j.Tables),
		})
	}
	return out, nil
}
