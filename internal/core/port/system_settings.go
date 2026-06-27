package port

import (
	"context"
)

// SystemSettingRepository persiste system_settings (cifrado com master KEK).
// Implementado por *postgres.SystemSettingsRepo.
//
// A chave (key) é o identificador semântico da setting (ex.:
// "whatsmeow.enabled", "ffmpeg.concurrency"). O valor é cifrado
// com a master KEK via Envelope.SealSystem antes de persistir.
type SystemSettingRepository interface {
	// Get lê uma setting e devolve os bytes cifrados (sem decifrar).
	// Retorna (nil, nil) se a setting não existe.
	Get(ctx context.Context, key string) (encrypted []byte, kekVersion int, err error)
	// Set persiste o valor cifrado. Faz UPSERT (insert-or-update).
	Set(ctx context.Context, key string, encrypted []byte, kekVersion int, description, updatedBy string) error
	// List devolve (key, encrypted, kek_version, updated_at) para auditoria.
	List(ctx context.Context) ([]SystemSettingEntry, error)
	// Delete remove uma setting (admin only). Erro se não existe.
	Delete(ctx context.Context, key string) error
}

// SystemSettingEntry é uma linha de system_settings (raw, sem decifrar).
type SystemSettingEntry struct {
	Key         string
	Encrypted   []byte
	KekVersion  int
	Description string
	UpdatedAt   string // ISO8601
	UpdatedBy   string
}

// SystemSettingEvent é publicado quando uma setting muda (hot-reload).
type SystemSettingEvent struct {
	Key       string
	// EncryptedValue é o valor novo (cifrado). Subscribers devem decifrar
	// com a KEK local antes de usar.
	EncryptedValue []byte
	KekVersion     int
	UpdatedBy      string
	// OldEncrypted (opcional) — para diffs; pode ser nil em inserts.
	OldEncrypted []byte
}
