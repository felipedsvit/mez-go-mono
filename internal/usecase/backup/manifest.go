// Package backup é o use case de backup/restore/reset por-tenant (Fase 6).
//
// Issues cobertas:
//   - #81: export NDJSON por tenant (REPEATABLE READ + S3 multipart)
//   - #82: restore idempotente com ordem topológica (FK deferíveis)
//   - #83: reset com confirmação dupla + disconnect whatsmeow
//
// Não portado do pai (mez-go): o pai não tem este package. O design segue
// o mesmo padrão (NDJSON + S3) do backup da V2 do pai, mas re-implementado
// para o mono: 1 client/tenant, sem NATS, sem sharding.
package backup

import (
	"encoding/json"
	"time"
)

// SchemaVersion é a versão do schema de backup. Incrementar quando o
// formato NDJSON ou a lista de tabelas backupadas mudar de forma não
// retrocompatível.
const SchemaVersion = 1

// Manifest é gravado em S3 ao final de um export bem-sucedido. O restore
// usa-o para validar compatibilidade (C7) e conhecer a topologia.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	BackupID      string          `json:"backup_id"`
	TenantID      string          `json:"tenant_id"`
	CreatedAt     time.Time       `json:"created_at"`
	CreatedBy     string          `json:"created_by"` // email do admin
	Source        Source          `json:"source"`
	Tables        []TableInfo     `json:"tables"`
	MediaFiles    int             `json:"media_files"`
	TotalRows     int64           `json:"total_rows"`
	NDJSONKey     string          `json:"ndjson_key"` // chave S3 do arquivo NDJSON
	MediaPrefix   string          `json:"media_prefix"`
	Notes         ManifestNotes   `json:"notes"`
	Extensions    map[string]any  `json:"extensions,omitempty"` // espaço para metadata extra sem bump de schema
}

// Source identifica a origem do backup.
type Source struct {
	MezgoMonoVersion string `json:"mezgo_mono_version"`
	PostgresVersion  string `json:"postgres_version"`
	AppPoolHash      string `json:"app_pool_hash,omitempty"` // checksum opcional
}

// TableInfo descreve uma tabela exportada (ordem topológica respeitada no
// restore).
type TableInfo struct {
	Name  string `json:"name"`
	Rows  int64  `json:"rows"`
	Bytes int64  `json:"bytes"`
}

// ManifestNotes é metadata de observabilidade.
type ManifestNotes struct {
	Partial       bool   `json:"partial,omitempty"`        // ex.: S3 NDJSON ok, mídia falhou
	MediaMissing  bool   `json:"media_missing,omitempty"`  // mídia S3 indisponível no momento
	FailureReason string `json:"failure_reason,omitempty"` // preenchido se Partial=true
}

// Marshal serializa o manifest para JSON. Centralizado para usar em testes
// e nos uploads S3.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Unmarshal faz o inverso de Marshal.
func Unmarshal(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
