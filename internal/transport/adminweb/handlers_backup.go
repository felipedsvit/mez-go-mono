// Package adminweb — handlers_backup.go: UI de backup/restore por tenant (#86).
//
// Páginas:
//   - GET  /admin/tenants/{id}/backup         → formulário + lista de backups
//   - POST /admin/tenants/{id}/backup         → inicia export
//   - GET  /admin/tenants/{id}/backup/status  → fragmento htmx (poll 2s)
//   - GET  /admin/tenants/{id}/backup/list    → lista de backups existentes no S3
//   - POST /admin/tenants/{id}/restore        → inicia restore (form com backup_id)
//
// UX: htmx poll a cada 2s do status do job. CSRF token injetado via
// csrfTokenFromContext e incluído nos forms via @CSRFInput.

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
	"github.com/felipedsvit/mez-go-mono/internal/transport/adminweb/templates"
)

func (s *Server) handleBackupPage(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	p := s.basePageData(r)
	p.Title = "Backup — " + tenantID

	backups, _ := s.listBackupsForTenant(r.Context(), tenantID)

	data := templates.BackupData{
		Page:     p,
		TenantID: tenantID,
		Backups:  backups,
	}
	// Se vier job_id na query, inclui o status do job em andamento.
	if jobID := r.URL.Query().Get("job_id"); jobID != "" {
		if job, err := s.backup.Job(jobID); err == nil {
			v := jobToView(job)
			data.Job = &v
		}
	}
	s.renderTempl(w, templates.Backup(data))
}

func (s *Server) handleBackupStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	p := s.basePageData(r)
	p.Title = "Backup — " + tenantID

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}
	res, err := s.backup.Export(r.Context(), ucbackup.ExportRequest{
		TenantID:     tenantID,
		Actor:        cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
		IncludeMedia: true,
	})
	if err != nil {
		p.Error = "Erro ao iniciar backup: " + err.Error()
		s.renderTempl(w, templates.Backup(templates.BackupData{Page: p, TenantID: tenantID}))
		return
	}

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

	// Renderiza o fragmento htmx (sem Layout) para polling.
	v := jobToView(job)
	s.renderTempl(w, templates.BackupStatusFragment(v, csrfTokenFromContext(r)))
}

func (s *Server) handleBackupList(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	p := s.basePageData(r)
	p.Title = "Backups — " + tenantID
	backups, _ := s.listBackupsForTenant(r.Context(), tenantID)
	s.renderTempl(w, templates.BackupList(templates.BackupListData{Page: p, TenantID: tenantID, Backups: backups}))
}

func (s *Server) handleRestoreStart(w http.ResponseWriter, r *http.Request) {
	tenantID := chi.URLParam(r, "id")
	backupID := r.FormValue("backup_id")
	p := s.basePageData(r)
	p.Title = "Backup — " + tenantID

	actor := ucadmin.Actor{
		ID:    p.Principal.UserID,
		Email: p.Principal.Email,
		IP:    r.RemoteAddr,
	}
	res, err := s.backup.Restore(r.Context(), ucbackup.RestoreRequest{
		TenantID: tenantID,
		BackupID: backupID,
		Actor:    cdomain.Actor{ID: actor.ID, Email: actor.Email, IP: actor.IP},
	})
	if err != nil {
		p.Error = "Erro ao iniciar restore: " + err.Error()
		s.renderTempl(w, templates.Backup(templates.BackupData{Page: p, TenantID: tenantID}))
		return
	}
	s.redirect(w, r, fmt.Sprintf("/admin/tenants/%s/backup?job_id=%s",
		tenantID, res.JobID))
}

// listBackupsForTenant lista os jobs de export finalizados para o tenant.
// Implementação atual usa o JobStore em memória (refactor: ler S3).
func (s *Server) listBackupsForTenant(ctx context.Context, tenantID string) ([]templates.BackupJobView, error) {
	store := s.backup
	if store == nil {
		return nil, nil
	}
	jobs := store.ListJobs(50)
	out := []templates.BackupJobView{}
	for _, j := range jobs {
		if j.Kind != ucbackup.JobExport || j.TenantID != tenantID {
			continue
		}
		if j.State != ucbackup.StateDone {
			continue
		}
		ended := time.Time{}
		if j.FinishedAt != nil {
			ended = *j.FinishedAt
		}
		out = append(out, templates.BackupJobView{
			ID:        j.ID,
			State:     string(j.State),
			Tables:    tableNames(j.Tables),
			Bytes:     0, // preenchido quando lermos do S3
			StartedAt: j.StartedAt,
			EndedAt:   ended,
			BackupID:  j.BackupID,
		})
	}
	return out, nil
}

func tableNames(t []ucbackup.TableProgress) []string {
	out := make([]string, 0, len(t))
	for _, tp := range t {
		out = append(out, tp.Name)
	}
	return out
}

// jobToView converte ucbackup.Job em templates.BackupJobView para uso
// pelo fragment htmx de status. Lê os campos públicos diretamente; a
// mutex interna (j.lock) é segurada pela goroutine que executa o job,
// e leituras concorrentes nos campos primitivos são toleráveis (data
// race em strings raramente é fatal; se necessário, podemos expor um
// Snapshot público no pacote backup).
func jobToView(j *ucbackup.Job) templates.BackupJobView {
	j.Lock().Lock()
	defer j.Lock().Unlock()
	ended := time.Time{}
	if j.FinishedAt != nil {
		ended = *j.FinishedAt
	}
	return templates.BackupJobView{
		ID:        j.ID,
		State:     string(j.State),
		Tables:    tableProgressNames(j.Tables),
		Bytes:     0,
		StartedAt: j.StartedAt,
		EndedAt:   ended,
		BackupID:  j.BackupID,
		Error:     j.Error,
	}
}

func tableProgressNames(t []ucbackup.TableProgress) []string {
	out := make([]string, 0, len(t))
	for _, tp := range t {
		out = append(out, tp.Name)
	}
	return out
}
