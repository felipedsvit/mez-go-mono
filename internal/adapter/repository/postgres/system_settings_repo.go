// Package postgres — system_settings_repo.go: persistência de system_settings
// (Fase 10 #177). Cifrado com master KEK via Envelope.SealSystem.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// SystemSettingsRepo é a implementação Postgres de port.SystemSettingRepository.
//
// Acesso:
//   - appPool:     SELECT (mez_app — para o app ler config no boot)
//   - platformPool: ALL (mez_platform — admin)
//
// O repositório usa platformPool para todas as operações (cobre o caso
// admin). Leituras do app usam o método ReadOnly com appPool (que
// respeita RLS — mez_app tem SELECT).
type SystemSettingsRepo struct {
	appPool     *pgxpool.Pool
	platformPool *pgxpool.Pool
}

// NewSystemSettingsRepo cria o repo. platformPool é usado para writes
// (admin audit), appPool para reads (app startup).
func NewSystemSettingsRepo(appPool, platformPool *pgxpool.Pool) *SystemSettingsRepo {
	return &SystemSettingsRepo{
		appPool:      appPool,
		platformPool: platformPool,
	}
}

// Get lê uma setting via appPool (RLS mez_app SELECT).
// Retorna (nil, 0, nil) se não existe.
func (r *SystemSettingsRepo) Get(ctx context.Context, key string) ([]byte, int, error) {
	var encrypted []byte
	var kekVersion int

	err := r.appPool.QueryRow(ctx, `
		SELECT value_encrypted, kek_version
		FROM system_settings
		WHERE key = $1
	`, key).Scan(&encrypted, &kekVersion)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("settings get: %w", err)
	}
	return encrypted, kekVersion, nil
}

// Set persiste via platformPool (RLS mez_platform ALL + audit).
func (r *SystemSettingsRepo) Set(ctx context.Context, key string, encrypted []byte, kekVersion int, description, updatedBy string) error {
	_, err := r.platformPool.Exec(ctx, `
		INSERT INTO system_settings (key, value_encrypted, kek_version, description, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, now(), $5)
		ON CONFLICT (key) DO UPDATE SET
			value_encrypted = EXCLUDED.value_encrypted,
			kek_version = EXCLUDED.kek_version,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at,
			updated_by = EXCLUDED.updated_by
	`, key, encrypted, kekVersion, description, updatedBy)
	if err != nil {
		return fmt.Errorf("settings set: %w", err)
	}
	return nil
}

// List devolve todas as settings (metadata only — sem decifrar).
func (r *SystemSettingsRepo) List(ctx context.Context) ([]port.SystemSettingEntry, error) {
	rows, err := r.appPool.Query(ctx, `
		SELECT key, value_encrypted, kek_version, description, updated_at, updated_by
		FROM system_settings
		ORDER BY key
	`)
	if err != nil {
		return nil, fmt.Errorf("settings list: %w", err)
	}
	defer rows.Close()

	var out []port.SystemSettingEntry
	for rows.Next() {
		var (
			key         string
			encrypted   []byte
			kekVersion  int
			description *string
			updatedAt   time.Time
			updatedBy   *string
		)
		if err := rows.Scan(&key, &encrypted, &kekVersion, &description, &updatedAt, &updatedBy); err != nil {
			return nil, fmt.Errorf("settings scan: %w", err)
		}
		entry := port.SystemSettingEntry{
			Key:         key,
			Encrypted:   encrypted,
			KekVersion:  kekVersion,
			UpdatedAt:   updatedAt.UTC().Format(time.RFC3339),
		}
		if description != nil {
			entry.Description = *description
		}
		if updatedBy != nil {
			entry.UpdatedBy = *updatedBy
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

// Delete remove uma setting (admin only).
func (r *SystemSettingsRepo) Delete(ctx context.Context, key string) error {
	tag, err := r.platformPool.Exec(ctx, `DELETE FROM system_settings WHERE key = $1`, key)
	if err != nil {
		return fmt.Errorf("settings delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("settings delete: not found")
	}
	return nil
}
