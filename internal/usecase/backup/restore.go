// Package backup — restore.go: restore idempotente por tenant (issue #82).
//
// Algoritmo:
//  1. Download manifest.json do S3 → valida schema_version (C7).
//  2. Compara com a versão atual do schema_migrations:
//     - manifest > current → recusa (ErrSchemaDowngrade, C7).
//     - manifest ≤ current → segue (sem DDL no restore).
//  3. Download NDJSON do S3.
//  4. Parse NDJSON linha-a-linha; cada registro contém o nome da tabela
//     (campo _table) e os dados.
//  5. Agrupa por tabela na ordem topológica (pais antes de filhas).
//  6. Abre tx DEFER CONSTRAINTS (C6) + batch upsert com ON CONFLICT (id)
//     DO UPDATE — idempotente (rodar 2x = mesmo resultado).
//  7. Audit log.

package backup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/repository/postgres"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/storage/s3"
	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
)

// RestoreRequest é o input do restore.
type RestoreRequest struct {
	TenantID string
	BackupID string
	Actor    cdomain.Actor
	// DryRun: se true, valida o backup e reporta o que seria feito, sem
	// inserir nada.
	DryRun bool
}

// RestoreResult é o output do restore.
type RestoreResult struct {
	JobID    string `json:"job_id"`
	BackupID string `json:"backup_id"`
	Skipped  bool   `json:"skipped"`     // true se NDJSON vazio
	Inserted int64  `json:"inserted"`    // total de linhas upserted
	DryRun   bool   `json:"dry_run"`
}

// Restore valida o manifest de forma síncrona (C7) e depois inicia o job
// em goroutine para o trabalho pesado.
func (s *Service) Restore(ctx context.Context, req RestoreRequest) (*RestoreResult, error) {
	if req.TenantID == "" {
		return nil, fmt.Errorf("tenant_id é obrigatório")
	}
	if req.BackupID == "" {
		return nil, fmt.Errorf("backup_id é obrigatório")
	}
	if req.Actor.Email == "" {
		return nil, fmt.Errorf("actor.email é obrigatório")
	}

	// Validação síncrona (C7): download manifest + checar schema_version
	// antes de criar o job, para retornar o erro direto ao caller.
	bucket := s.store.BackupBucket()
	manifestKey := fmt.Sprintf("tenants/%s/backups/%s/manifest.json",
		req.TenantID, req.BackupID)
	mr, err := s.store.DownloadStream(ctx, bucket, manifestKey)
	if err != nil {
		if errors.Is(err, s3.ErrNotFound) {
			return nil, ErrBackupNotFound
		}
		return nil, fmt.Errorf("download manifest: %w", err)
	}
	manifestData, err := io.ReadAll(mr)
	mr.Close()
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := Unmarshal(manifestData)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.SchemaVersion > SchemaVersion {
		return nil, ErrSchemaDowngrade
	}

	var currentVersion int
	if s.platformPool != nil {
		err = s.platformPool.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion)
	} else {
		err = s.pgxPool.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&currentVersion)
	}
	if err != nil {
		return nil, fmt.Errorf("read schema version: %w", err)
	}
	if manifest.SchemaVersion > currentVersion {
		return nil, ErrSchemaDowngrade
	}

	job := &Job{
		ID:        newJobID(),
		Kind:      JobRestore,
		TenantID:  req.TenantID,
		Actor:     req.Actor.Email,
		State:     StatePending,
		BackupID:  req.BackupID,
		StartedAt: now(),
		UpdatedAt: now(),
	}
	s.jobs.Put(job)

	go s.runRestore(job, req, manifest, currentVersion)

	return &RestoreResult{JobID: job.ID, BackupID: req.BackupID}, nil
}

func (s *Service) runRestore(job *Job, req RestoreRequest, manifest *Manifest, currentVersion int) {
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
	})
	s.jobs.Put(job)

	// Manifest e schema_version já validados em Restore(); vai direto ao NDJSON.
	bucket := s.store.BackupBucket()

	if req.DryRun {
		finished := now()
		updateJob(job, func() {
			job.State = StateDone
			job.ProgressPct = 100
			job.CurrentStep = "dry-run complete"
			job.FinishedAt = &finished
			job.UpdatedAt = finished
		})
		s.jobs.Put(job)
		s.recordAudit(ctx, req.Actor, cdomain.ActionTenantRestore, req.BackupID, req.TenantID, map[string]any{
			"dry_run":          true,
			"manifest_version": manifest.SchemaVersion,
			"current_version":  currentVersion,
			"tables":           len(manifest.Tables),
		})
		return
	}

	// 3. Download NDJSON.
	updateJob(job, func() {
		job.CurrentStep = "download ndjson"
		job.UpdatedAt = now()
	})
	s.jobs.Put(job)

	nr, err := s.store.DownloadStream(ctx, bucket, manifest.NDJSONKey)
	if err != nil {
		s.markFailed(job, fmt.Errorf("download ndjson: %w", err))
		return
	}
	defer nr.Close()

	// 4. Parse NDJSON, agrupando por tabela.
	updateJob(job, func() {
		job.CurrentStep = "parse ndjson"
		job.UpdatedAt = now()
	})
	s.jobs.Put(job)

	batches := make(map[string]*tableBatch)
	order := []string{}
	rowCount := 0

	scanner := bufio.NewScanner(nr)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024) // até 16 MiB por linha
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			s.markFailed(job, fmt.Errorf("parse ndjson line: %w", err))
			return
		}
		tableName, _ := rec["_table"].(string)
		if tableName == "" {
			tableName = inferTableName(rec)
		}
		if tableName == "" {
			s.markFailed(job, fmt.Errorf("ndjson: registro sem tabela identificável"))
			return
		}
		batch, ok := batches[tableName]
		if !ok {
			cols := make([]string, 0, len(rec))
			for k := range rec {
				if k != "_table" {
					cols = append(cols, k)
				}
			}
			order = appendInOrder(order, tableName, backupableTables)
			batch = &tableBatch{cols: cols}
			batches[tableName] = batch
		}
		values := make([]any, 0, len(batch.cols))
		for _, c := range batch.cols {
			values = append(values, rec[c])
		}
		batch.rows = append(batch.rows, values)
		rowCount++
	}
	if err := scanner.Err(); err != nil {
		s.markFailed(job, fmt.Errorf("scan ndjson: %w", err))
		return
	}

	if rowCount == 0 {
		finished := now()
		updateJob(job, func() {
			job.State = StateDone
			job.ProgressPct = 100
			job.CurrentStep = "empty backup, skipped"
			job.FinishedAt = &finished
			job.UpdatedAt = finished
		})
		s.jobs.Put(job)
		return
	}

	// 5. Abre tx DEFER constraints (C6) + executa batch upserts.
	updateJob(job, func() {
		job.CurrentStep = "insert into db"
		job.UpdatedAt = now()
	})
	s.jobs.Put(job)

	tx, txCtx, err := s.tx.BeginTenantTx(ctx, domain.TenantID(req.TenantID),
		postgres.WithDeferConstraints())
	if err != nil {
		s.markFailed(job, fmt.Errorf("begin tx: %w", err))
		return
	}
	defer tx.Rollback(ctx)

	var totalInserted int64
	for idx, table := range order {
		batch := batches[table]
		if batch == nil || len(batch.rows) == 0 {
			continue
		}
		inserted, err := s.upsertBatch(txCtx, tx, table, batch.cols, batch.rows)
		if err != nil {
			s.markFailed(job, fmt.Errorf("upsert %s: %w", table, err))
			return
		}
		totalInserted += inserted

		updateJob(job, func() {
			job.Tables = append(job.Tables, TableProgress{
				Name:  table,
				State: StateDone,
				Rows:  inserted,
			})
			job.ProgressPct = ((idx + 1) * 100) / len(order)
			job.UpdatedAt = now()
		})
		s.jobs.Put(job)
	}

	if err := tx.Commit(ctx); err != nil {
		s.markFailed(job, fmt.Errorf("commit restore: %w", err))
		return
	}

	s.recordAudit(ctx, req.Actor, cdomain.ActionTenantRestore, req.BackupID, req.TenantID, map[string]any{
		"manifest_version": manifest.SchemaVersion,
		"current_version":  currentVersion,
		"tables":           len(manifest.Tables),
		"inserted":         totalInserted,
	})

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

// tableBatch agrupa linhas para upsert em batch.
type tableBatch struct {
	cols []string
	rows [][]any
}

// upsertBatch faz INSERT ... ON CONFLICT (id) DO UPDATE em lotes de 100.
// Idempotente: aplicar 2x com mesmo NDJSON produz o mesmo estado.
func (s *Service) upsertBatch(ctx context.Context, tx pgx.Tx, table string, cols []string, rows [][]any) (int64, error) {
	const batchSize = 100
	var total int64

	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]

		// Constrói: INSERT INTO t (col1, col2, ...) VALUES ($1, $2, ...), ($N, $N+1, ...), ...
		// ON CONFLICT (id) DO UPDATE SET col1=EXCLUDED.col1, ...
		var sb strings.Builder
		sb.WriteString("INSERT INTO ")
		sb.WriteString(pgQuoteIdent(table))
		sb.WriteString(" (")
		for i, c := range cols {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(pgQuoteIdent(c))
		}
		sb.WriteString(") VALUES ")

		args := make([]any, 0, len(chunk)*len(cols))
		for r, row := range chunk {
			if r > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("(")
			for c := range cols {
				if c > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(fmt.Sprintf("$%d", len(args)+1))
				args = append(args, pgValueFromJSON(row[c]))
			}
			sb.WriteString(")")
		}

		// ON CONFLICT (id) DO UPDATE SET col1=EXCLUDED.col1, ...
		// Assume que toda tabela tem PK "id" (padrão do projeto).
		sb.WriteString(" ON CONFLICT (id) DO UPDATE SET ")
		first := true
		for _, c := range cols {
			if c == "id" {
				continue
			}
			if !first {
				sb.WriteString(", ")
			}
			first = false
			sb.WriteString(pgQuoteIdent(c))
			sb.WriteString(" = EXCLUDED.")
			sb.WriteString(pgQuoteIdent(c))
		}

		tag, err := tx.Exec(ctx, sb.String(), args...)
		if err != nil {
			return total, fmt.Errorf("exec batch: %w", err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// pgValueFromJSON converte um valor lido do JSON de volta para um tipo
// aceito pelo pgx. Tipos:
//   - string UUID → uuid.UUID
//   - time.Time (RFC3339) → time.Time
//   - []byte (JSON object) → json.RawMessage
//   - map[string]any (metadata) → json.Marshal (string)
func pgValueFromJSON(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		// UUID (formato 8-4-4-4-12 hex).
		if len(x) == 36 && x[8] == '-' && x[13] == '-' && x[18] == '-' && x[23] == '-' {
			if u, err := uuid.Parse(x); err == nil {
				return u
			}
		}
		// Timestamp.
		if t, ok := tryParseTime(x); ok {
			return t
		}
		return x
	case map[string]any:
		// JSONB round-trip: serializa de volta para string JSON.
		data, err := json.Marshal(x)
		if err != nil {
			return v
		}
		return string(data)
	case []any:
		// JSONB array: idem.
		data, err := json.Marshal(x)
		if err != nil {
			return v
		}
		return string(data)
	case [16]byte:
		return x
	default:
		return v
	}
}

// tryParseTime tenta parsear uma string como timestamp.
func tryParseTime(s string) (time.Time, bool) {
	if len(s) < 19 {
		return time.Time{}, false
	}
	// Tenta formatos comuns.
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// inferTableName é fallback quando o registro NDJSON não tem _table.
func inferTableName(rec map[string]any) string {
	hasField := func(name string) bool {
		_, ok := rec[name]
		return ok
	}
	switch {
	case hasField("jid") && hasField("day_sent_count"):
		return "whatsapp_account_state"
	case hasField("jid") && hasField("wrapped_dek"):
		return "whatsapp_session_keys"
	case hasField("jid") && hasField("message_id"):
		return "whatsapp_history"
	case hasField("wrapped_dek") && hasField("encrypted"):
		return "channel_credentials"
	case hasField("conversation_id") && hasField("body"):
		return "messages"
	case hasField("contact_id") && hasField("status"):
		return "conversations"
	case hasField("phone") && hasField("name") && hasField("channel"):
		return "contacts"
	case hasField("payload") && hasField("source"):
		return "inbound_events"
	case hasField("target") && hasField("attempts"):
		return "outbound_events"
	}
	return ""
}

// appendInOrder insere tableName em `order` respeitando a posição relativa
// de `topoOrder`. Usado para que restore processe pais antes de filhas.
func appendInOrder(order []string, tableName string, topoOrder []string) []string {
	for _, e := range order {
		if e == tableName {
			return order
		}
	}
	pos := -1
	for i, t := range topoOrder {
		if t == tableName {
			pos = i
			break
		}
	}
	if pos < 0 {
		return append(order, tableName)
	}
	for i, e := range order {
		for j, t := range topoOrder {
			if t == e && j > pos {
				out := append([]string{}, order[:i]...)
				out = append(out, tableName)
				out = append(out, order[i:]...)
				return out
			}
		}
	}
	return append(order, tableName)
}
