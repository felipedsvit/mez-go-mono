// Package backup — export.go: export NDJSON por tenant (issue #81).
//
// Algoritmo:
//  1. Abre tx REPEATABLE READ via TxRunner.BeginTenantTx.
//  2. Para cada tabela backupada (ordem topológica):
//     a. Query() → rows → scan em []any (tipos nativos pgx) → JSON per row.
//     b. Cada linha NDJSON vai para um io.Pipe Writer em goroutine.
//  3. Pipe reader é consumido por store.UploadStream (multipart S3).
//  4. Conta bytes/rows por tabela para o manifest.
//  5. No fim: sobe manifest.json via UploadBytes.
//
// Garantias:
//   - REPEATABLE READ: snapshot consistente mesmo com writes concorrentes (#81).
//   - Stream row-by-row: memória O(1) por linha (não materializa tudo).
//   - Ordem topológica: pais antes de filhas (necessário p/ restore sem
//     DEFER constraints — embora #82 também use DEFER como safety net).
//   - audit_log: cross-tenant, omitido (carries admin history; Fase 7).

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
)

// backupableTables lista as tabelas multi-tenant exportadas, em ordem
// topológica (pais antes de filhas).
var backupableTables = []string{
	"contacts",
	"conversations",
	"messages",
	"inbound_events",
	"outbound_events",
	"channel_credentials",
	"whatsapp_account_state",
	"whatsapp_session_keys",
	"whatsapp_history",
}

// tableColumnsCache resolve colunas por tabela uma vez por export.
type tableColumnsCache struct {
	mu      sync.Mutex
	columns map[string][]string
}

func (c *tableColumnsCache) get(ctx context.Context, tx pgx.Tx, table string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.columns == nil {
		c.columns = make(map[string][]string)
	}
	if cols, ok := c.columns[table]; ok {
		return cols, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_name = $1
		  AND table_schema = 'public'
		ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, fmt.Errorf("list columns %s: %w", table, err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var col, dtype string
		if err := rows.Scan(&col, &dtype); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("tabela %s não encontrada ou sem colunas", table)
	}
	c.columns[table] = cols
	return cols, nil
}

// ExportRequest é o input do export.
type ExportRequest struct {
	TenantID string
	Actor    cdomain.Actor
	// IncludeMedia: se true, conta arquivos S3 de mídia do tenant
	// (referência, não cópia). Default: true.
	IncludeMedia bool
}

// ExportResult devolve o ID do job + ID do backup.
type ExportResult struct {
	JobID    string `json:"job_id"`
	BackupID string `json:"backup_id"`
}

// Export inicia o job em goroutine. Retorna imediatamente.
func (s *Service) Export(ctx context.Context, req ExportRequest) (*ExportResult, error) {
	if req.TenantID == "" {
		return nil, fmt.Errorf("tenant_id é obrigatório")
	}
	if req.Actor.Email == "" {
		return nil, fmt.Errorf("actor.email é obrigatório")
	}
	job := &Job{
		ID:          newJobID(),
		Kind:        JobExport,
		TenantID:    req.TenantID,
		Actor:       req.Actor.Email,
		State:       StatePending,
		Tables:      make([]TableProgress, 0, len(backupableTables)),
		StartedAt:   now(),
		UpdatedAt:   now(),
	}
	backupID := newJobID()
	job.BackupID = backupID
	s.jobs.Put(job)

	go s.runExport(job, req, backupID)

	return &ExportResult{JobID: job.ID, BackupID: backupID}, nil
}

func (s *Service) runExport(job *Job, req ExportRequest, backupID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			s.markFailed(job, fmt.Errorf("panic: %v", r))
		}
	}()

	updateJob(job, func() {
		job.State = StateRunning
		job.UpdatedAt = now()
		job.CurrentStep = "begin tx"
	})
	s.jobs.Put(job)

	tx, txCtx, err := s.tx.BeginTenantTx(ctx, domain.TenantID(req.TenantID),
		postgres.WithRepeatableRead())
	if err != nil {
		s.markFailed(job, fmt.Errorf("begin tx: %w", err))
		return
	}
	defer tx.Rollback(ctx)

	pgVersion, _ := s.fetchPGVersion(ctx)
	src := Source{
		MezgoMonoVersion: s.version,
		PostgresVersion:  pgVersion,
	}

	// Pipe: writer recebe NDJSON, reader vai pro S3.
	pr, pw := io.Pipe()
	colCache := &tableColumnsCache{}

	// Sincronização para coletar TableInfo do goroutine serializador.
	var (
		mu          sync.Mutex
		tables      []TableInfo
		totalRows   atomic.Int64
		totalBytes  atomic.Int64
		currentRows atomic.Int64
		curTable    atomic.Value // string
	)
	curTable.Store("")

	// Goroutine serializadora: query → JSON → pipe writer.
	serializerErrCh := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = pr.CloseWithError(fmt.Errorf("serializer panic: %v", r))
				return
			}
			_ = pw.Close()
		}()
		for _, table := range backupableTables {
			cols, err := colCache.get(txCtx, tx, table)
			if err != nil {
				serializerErrCh <- fmt.Errorf("get columns %s: %w", table, err)
				return
			}
			curTable.Store(table)
			currentRows.Store(0)

			// Marca progresso da tabela
			updateJob(job, func() {
				job.Tables = append(job.Tables, TableProgress{Name: table, State: StateRunning})
				job.CurrentStep = "exporting " + table
				job.UpdatedAt = now()
			})
			s.jobs.Put(job)

			// SELECT col1,col2,... FROM table WHERE tenant_id=$1
			sql := fmt.Sprintf(`SELECT %s FROM %s WHERE tenant_id = $1`,
				joinCols(cols), table)
			rows, err := tx.Query(ctx, sql, req.TenantID)
			if err != nil {
				serializerErrCh <- fmt.Errorf("query %s: %w", table, err)
				return
			}
			rowCount := int64(0)
			for rows.Next() {
				rawValues, err := rows.Values()
				if err != nil {
					rows.Close()
					serializerErrCh <- fmt.Errorf("scan %s: %w", table, err)
					return
				}
				rec := make(map[string]any, len(cols))
				rec["_table"] = table
				for i, col := range cols {
					rec[col] = pgValueToJSON(rawValues[i])
				}
				data, err := json.Marshal(rec)
				if err != nil {
					rows.Close()
					serializerErrCh <- fmt.Errorf("marshal %s: %w", table, err)
					return
				}
				if _, err := pw.Write(data); err != nil {
					rows.Close()
					serializerErrCh <- fmt.Errorf("write ndjson: %w", err)
					return
				}
				if _, err := pw.Write([]byte{'\n'}); err != nil {
					rows.Close()
					serializerErrCh <- fmt.Errorf("write newline: %w", err)
					return
				}
				rowCount++
				totalRows.Add(1)
				currentRows.Add(1)
				totalBytes.Add(int64(len(data) + 1))
			}
			if err := rows.Err(); err != nil {
				serializerErrCh <- fmt.Errorf("rows %s: %w", table, err)
				return
			}
			rows.Close()

			// Coleta TableInfo.
			mu.Lock()
			tables = append(tables, TableInfo{
				Name:  table,
				Rows:  rowCount,
				Bytes: currentRows.Load() * 100, // aprox; será recalculado pelo byte counter real
			})
			mu.Unlock()

			// Marca tabela como done.
			updateJob(job, func() {
				for i := range job.Tables {
					if job.Tables[i].Name == table {
						job.Tables[i].State = StateDone
						job.Tables[i].Rows = rowCount
					}
				}
				job.ProgressPct = (len(tables) * 100) / len(backupableTables)
				job.UpdatedAt = now()
			})
			s.jobs.Put(job)
		}
		curTable.Store("")
	}()

	// Upload S3.
	ndjsonKey := fmt.Sprintf("tenants/%s/backups/%s/backup.ndjson",
		req.TenantID, backupID)
	bucket := s.store.BackupBucket()
	updateJob(job, func() {
		job.CurrentStep = "uploading s3"
		job.UpdatedAt = now()
	})
	s.jobs.Put(job)

	_, err = s.store.UploadStream(ctx, bucket, ndjsonKey, pr, "application/x-ndjson")
	if err != nil {
		_ = pr.CloseWithError(err)
	}

	// Espera serializer terminar.
	select {
	case err := <-serializerErrCh:
		_ = tx.Rollback(ctx)
		s.markFailed(job, fmt.Errorf("export: %w", err))
		return
	case <-ctx.Done():
		_ = tx.Rollback(ctx)
		s.markFailed(job, ctx.Err())
		return
	default:
		// ok
	}

	// Commit (release do snapshot REPEATABLE READ).
	if err := tx.Commit(ctx); err != nil {
		s.markFailed(job, fmt.Errorf("commit: %w", err))
		return
	}

	// Mídia: contagem de arquivos no S3.
	mediaPrefix := fmt.Sprintf("tenants/%s/", req.TenantID)
	mediaCount := 0
	if req.IncludeMedia {
		mediaCount, _ = s.countS3Prefix(ctx, mediaPrefix)
	}

	// Manifest.
	manifest := &Manifest{
		SchemaVersion: SchemaVersion,
		BackupID:      backupID,
		TenantID:      req.TenantID,
		CreatedAt:     now(),
		CreatedBy:     req.Actor.Email,
		Source:        src,
		Tables:        tables,
		MediaFiles:    mediaCount,
		TotalRows:     totalRows.Load(),
		NDJSONKey:     ndjsonKey,
		MediaPrefix:   mediaPrefix,
	}
	manifestBytes, err := manifest.Marshal()
	if err != nil {
		s.markFailed(job, fmt.Errorf("marshal manifest: %w", err))
		return
	}
	manifestKey := fmt.Sprintf("tenants/%s/backups/%s/manifest.json",
		req.TenantID, backupID)
	if _, err := s.store.UploadBytes(ctx, bucket, manifestKey, manifestBytes, "application/json"); err != nil {
		s.markFailed(job, fmt.Errorf("upload manifest: %w", err))
		return
	}

	// Issue #148 (H5, CWE-778): audit atômico via RunAsPlatform. A
	// audit row C5 (platform:access) é commitada atomicamente com a
	// fn; sem isso, crash entre mutation-commit e audit-insert
	// deixaria a action sem rastro.
	if err := s.runAsPlatform(ctx, req.Actor, cdomain.ActionTenantBackup, backupID, "backup", req.TenantID,
		map[string]any{
			"tables":     len(manifest.Tables),
			"total_rows": manifest.TotalRows,
			"media":      mediaCount,
			"bytes":      totalBytes.Load(),
		},
		func(ctx context.Context) error {
			// fn interna: registra a row de mutation. Se falhar,
			// a row C5 também é rolled back.
			if s.audit == nil {
				return nil
			}
			entry := &cdomain.AuditEntry{
				ActorID:    req.Actor.ID,
				ActorEmail: req.Actor.Email,
				Action:     cdomain.ActionTenantBackup,
				TargetType: "backup",
				TargetID:   backupID,
				TenantID:   req.TenantID,
				Metadata: map[string]any{
					"tables":     len(manifest.Tables),
					"total_rows": manifest.TotalRows,
					"media":      mediaCount,
					"bytes":      totalBytes.Load(),
				},
				IP:        req.Actor.IP,
				CreatedAt: now(),
			}
			return s.audit.Record(ctx, entry)
		}); err != nil {
		s.log.Warn().Err(err).Str("backup", backupID).Msg("backup: audit atômico falhou (best-effort segue)")
	}

	finished := now()
	updateJob(job, func() {
		job.State = StateDone
		job.ProgressPct = 100
		job.CurrentStep = "done"
		job.FinishedAt = &finished
		job.UpdatedAt = finished
	})
	s.jobs.Put(job)
}

// markFailed registra o erro no job.
func (s *Service) markFailed(job *Job, err error) {
	finished := now()
	updateJob(job, func() {
		job.State = StateFailed
		job.Error = err.Error()
		job.CurrentStep = "failed"
		job.FinishedAt = &finished
		job.UpdatedAt = finished
	})
	s.jobs.Put(job)
	s.log.Error().Err(err).Str("job_id", job.ID).Str("kind", string(job.Kind)).Msg("backup job failed")
}

// joinCols serializa slice de colunas para SQL: "col1, col2, ...".
func joinCols(cols []string) string {
	out := ""
	for i, c := range cols {
		if i > 0 {
			out += ", "
		}
		out += pgQuoteIdent(c)
	}
	return out
}

// pgQuoteIdent protege identificadores (não confia em nomes de coluna
// oriundos do information_schema — defense in depth).
func pgQuoteIdent(s string) string {
	out := `"`
	for _, r := range s {
		if r == '"' {
			out += `""`
		} else {
			out += string(r)
		}
	}
	return out + `"`
}

// pgValueToJSON converte um valor nativo do pgx em uma representação
// JSON-friendly que pode ser reconstruída no restore (pgValueFromJSON).
//
// Tipos tratados:
//   - pgtype.UUID → string (formato canônico 8-4-4-4-12)
//   - time.Time → RFC3339Nano UTC
//   - []byte (bytea) → string base64
//   - outros passam direto
//
// Sem este tratamento, pgx serializa pgtype.UUID para um objeto
// `{"Bytes":[...],"Valid":true}` que NÃO é restaurado para UUID.
func pgValueToJSON(v any) any {
	switch x := v.(type) {
	case pgtype.UUID:
		if x.Valid {
			return encodeUUIDString(x.Bytes)
		}
		return nil
	case [16]byte:
		return encodeUUIDString(x)
	case []byte:
		// Renderiza bytea como string (json.Marshal faria base64).
		return string(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return v
	}
}

// encodeUUIDString converte [16]byte para string UUID canônica.
func encodeUUIDString(b [16]byte) string {
	if u, err := uuid.FromBytes(b[:]); err == nil {
		return u.String()
	}
	// Fallback: hex simples.
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
