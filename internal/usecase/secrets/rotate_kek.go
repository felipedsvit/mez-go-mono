// Package secrets — rotate_kek.go: re-wrap de DEKs de todos os tenants
// (Fase 7 #92).
//
// Algoritmo:
//
//  1. Valida que ambas as KEKs (antiga e nova) têm 32 bytes.
//  2. Para cada (tenant, channel) em channel_credentials:
//     a. Unwrap(DEK, KEK_old) → DEK em claro (defer zero).
//     b. Wrap(DEK, KEK_new) → new_wrapped_dek.
//     c. Se DryRun: registra no Report e continua.
//     d. UpdateWrappedDEK(new_wrapped_dek, kek_version+1).
//     e. Audit "rotate_kek_tenant" com metadata (tenant, channel, v_old, v_new).
//     f. InvalidateFn(tenant) — expurga o cache do Keyring (issue #91).
//  3. Audit "rotate_kek_complete" com totais.
//  4. Retorna Report{Tenants, Channels, DurationMs, Errors}.
//
// Erros em linhas individuais NÃO abortam o lote — são coletados em
// Report.Errors. A rotação é best-effort por linha: o operator pode
// re-rodar `rotate-kek` após corrigir a causa (ex.: tenant com credencial
// corrompida).
//
// Auditoria:
//
//   - 1 linha "rotate_kek_started" (CLI, antes de iterar).
//   - N linhas "rotate_kek_tenant" (1 por linha re-wrapada).
//   - 1 linha "rotate_kek_complete" (no fim, com totais).
//
// As linhas "started" e "complete" são emitidas via audit.Record (best-
// effort, fora de tx). "rotate_kek_tenant" é emitida via ForEachTenant,
// que abre RunAsPlatform e grava C5 atomic per-row. Se uma linha
// individual falhar, o audit da próxima é gravado normalmente.
package secrets

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// SealerFactory é injetável para testes. Default: newSealerFromKEK usa
// adaptercrypto.NewLocalSealerFromKEK. Em produção, o caller passa a
// função ou usa o helper newSealerFromKEK diretamente.
type SealerFactory func(kek []byte) (port.Sealer, error)

// defaultSealerFactory cria um LocalSealer a partir da KEK em bytes raw.
func defaultSealerFactory(kek []byte) (port.Sealer, error) {
	return adaptercrypto.NewLocalSealerFromKEK(kek)
}

// ErrInvalidKEKLength é retornado quando OldKEK ou NewKEK não tem 32 bytes.
var ErrInvalidKEKLength = errors.New("secrets: KEK deve ter 32 bytes (AES-256)")

// ErrEmptyKEK é retornado quando KEK é string vazia (não fornecida).
var ErrEmptyKEK = errors.New("secrets: KEK não pode ser vazia")

// AuditRepository é a abstração que o Rotate grava audit rows. Implementado
// por *admin.AuditRepo (carryover Fase 1).
type AuditRepository interface {
	Record(ctx context.Context, entry *admin.AuditEntry) error
}

// ChannelCredentialsCrossTenant é o subconjunto de CredentialsRepository
// usado pelo Rotate — apenas a iteração cross-tenant (ForEachTenant) e
// o update do wrapped_dek. Não expõe Get/Upsert/Delete (esses são
// tenant-scoped e vivem no Keyring).
type ChannelCredentialsCrossTenant interface {
	ForEachTenant(ctx context.Context, actor string, fn func(ctx context.Context, row port.CredentialRow) error) error
	UpdateWrappedDEK(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, newWrappedDEK []byte, newKekVersion int, windowUntil *time.Time) error
}

// SealerPair encapsula os dois sealers: antigo (para unwrap) e novo
// (para wrap). Ambos satisfazem port.Sealer.
type SealerPair struct {
	Old port.Sealer
	New port.Sealer
}

// RotateKEKOpts configura uma operação de rotação.
type RotateKEKOpts struct {
	// OldKEKBase64: KEK atual em base64 (32 bytes decodificados).
	OldKEKBase64 string
	// NewKEKBase64: KEK nova em base64 (32 bytes decodificados).
	NewKEKBase64 string
	// DryRun: se true, não persiste nada. Apenas conta o que seria feito.
	DryRun bool
	// Actor: string livre para identificar quem disparou (ex.: "operator:joao").
	Actor string
	// InvalidateFn: callback opcional chamado após cada (tenant, channel)
	// re-wrapado, dentro da tx do ForEachTenant. Default: no-op.
	// Em produção: keyring.Invalidate(tenantID).
	InvalidateFn func(tenantID domain.TenantID)
	// SealerFactory: constrói port.Sealer a partir de KEK raw. Default:
	// adaptercrypto.NewLocalSealerFromKEK.
	SealerFactory SealerFactory
	// Now: clock injetável para testes. Default: time.Now.
	Now func() time.Time
}

// RotationError é uma falha em uma linha (tenant, channel) específica.
// Coletada em Report.Errors; não aborta a rotação.
type RotationError struct {
	TenantID domain.TenantID
	Channel  domain.Channel
	Op       string // "unwrap" | "wrap" | "update" | "audit"
	Err      error
}

func (e *RotationError) Error() string {
	return fmt.Sprintf("rotate_kek: %s tenant=%s channel=%s: %v", e.Op, e.TenantID, e.Channel, e.Err)
}

func (e *RotationError) Unwrap() error { return e.Err }

// Report sumariza o resultado da rotação. Tenants e Channels contam pares
// (tenant_id, channel) únicos processados com sucesso. Errors contém
// RotationError por linha que falhou.
type Report struct {
	StartedAt   time.Time
	DurationMs  int64
	Tenants     int
	Channels    int
	DryRun      bool
	OldVersion  int
	NewVersion  int
	Errors      []RotationError
}

// rotateKEK é o nome interno. Exposto como Rotate via wrapper público.
func rotateKEK(
	ctx context.Context,
	repo ChannelCredentialsCrossTenant,
	auditRepo AuditRepository,
	opts RotateKEKOpts,
) (Report, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	oldKEK, err := decodeKEK(opts.OldKEKBase64, "OldKEK")
	if err != nil {
		return Report{}, err
	}
	newKEK, err := decodeKEK(opts.NewKEKBase64, "NewKEK")
	if err != nil {
		return Report{}, err
	}
	_ = newKEK // usado para construir o new sealer externamente

	rpt := Report{
		StartedAt: now(),
		DryRun:    opts.DryRun,
	}

	// Audit "started" antes de iterar (best-effort).
	if auditRepo != nil {
		_ = auditRepo.Record(ctx, &admin.AuditEntry{
			ID:         uuid.NewString(),
			ActorEmail: opts.Actor,
			Action:     admin.ActionRotateKEKStarted,
			TargetType: "kek",
			Metadata: map[string]any{
				"dry_run": opts.DryRun,
			},
			CreatedAt: now(),
		})
	}

	oldSealer, err := opts.buildSealer(oldKEK)
	if err != nil {
		return rpt, fmt.Errorf("build old sealer: %w", err)
	}
	newSealer, err := opts.buildSealer(newKEK)
	if err != nil {
		return rpt, fmt.Errorf("build new sealer: %w", err)
	}

	tenantSet := make(map[domain.TenantID]struct{})

	err = repo.ForEachTenant(ctx, "system:rotate-kek", func(ctx context.Context, row port.CredentialRow) error {
		tenantSet[row.TenantID] = struct{}{}

		// a. Unwrap
		dek, uerr := oldSealer.Unwrap(ctx, row.WrappedDEK)
		if uerr != nil {
			rpt.Errors = append(rpt.Errors, RotationError{
				TenantID: row.TenantID, Channel: row.Channel, Op: "unwrap", Err: uerr,
			})
			return nil // continua
		}

		// b. Wrap
		newWrapped, werr := newSealer.Wrap(ctx, dek)
		// zero(DEK) sempre — mesmo em caso de erro de wrap, não vamos
		// deixar a chave em claro viva após este frame.
		zero(dek)
		if werr != nil {
			rpt.Errors = append(rpt.Errors, RotationError{
				TenantID: row.TenantID, Channel: row.Channel, Op: "wrap", Err: werr,
			})
			return nil
		}

		newVersion := row.KEKVersion + 1
		oldVersion := row.KEKVersion

		// c. Dry-run: conta e segue.
		if opts.DryRun {
			rpt.Channels++
			rpt.OldVersion = oldVersion
			rpt.NewVersion = newVersion
			return nil
		}

		// d. UpdateWrappedDEK (cross-tenant, audit C5).
		uperr := repo.UpdateWrappedDEK(ctx, row.TenantID, row.Channel, newWrapped, newVersion, nil)
		if uperr != nil {
			rpt.Errors = append(rpt.Errors, RotationError{
				TenantID: row.TenantID, Channel: row.Channel, Op: "update", Err: uperr,
			})
			return nil
		}

		// e. Audit "rotate_kek_tenant" — best-effort, fora de tx (cada
		// UpdateWrappedDEK já gera seu próprio platform_access via RunAsPlatform).
		if auditRepo != nil {
			_ = auditRepo.Record(ctx, &admin.AuditEntry{
				ID:         uuid.NewString(),
				ActorEmail: opts.Actor,
				Action:     admin.ActionRotateKEKTenant,
				TargetType: "channel_credentials",
				TargetID:   string(row.Channel),
				TenantID:   string(row.TenantID),
				Metadata: map[string]any{
					"old_kek_version": oldVersion,
					"new_kek_version": newVersion,
				},
				CreatedAt: now(),
			})
		}

		// f. InvalidateFn — expurga cache do Keyring para forçar re-fetch.
		if opts.InvalidateFn != nil {
			opts.InvalidateFn(row.TenantID)
		}

		rpt.Channels++
		rpt.OldVersion = oldVersion
		rpt.NewVersion = newVersion
		return nil
	})
	if err != nil {
		return rpt, fmt.Errorf("for each tenant: %w", err)
	}

	rpt.Tenants = len(tenantSet)
	rpt.DurationMs = now().Sub(rpt.StartedAt).Milliseconds()

	// Audit "complete" no fim (best-effort).
	if auditRepo != nil {
		_ = auditRepo.Record(ctx, &admin.AuditEntry{
			ID:         uuid.NewString(),
			ActorEmail: opts.Actor,
			Action:     admin.ActionRotateKEKComplete,
			TargetType: "kek",
			Metadata: map[string]any{
				"tenants":     rpt.Tenants,
				"channels":    rpt.Channels,
				"errors":      len(rpt.Errors),
				"duration_ms": rpt.DurationMs,
				"old_version": rpt.OldVersion,
				"new_version": rpt.NewVersion,
				"dry_run":     rpt.DryRun,
			},
			CreatedAt: now(),
		})
	}

	return rpt, nil
}

// decodeKEK decodifica base64 e valida 32 bytes. Retorna ErrInvalidKEKLength
// ou ErrEmptyKEK conforme o caso.
func decodeKEK(b64, name string) ([]byte, error) {
	if b64 == "" {
		return nil, fmt.Errorf("%w: %s", ErrEmptyKEK, name)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("secrets: decode %s: %w", name, err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("%w: %s tem %d bytes (esperado 32)", ErrInvalidKEKLength, name, len(raw))
	}
	return raw, nil
}

// buildSealer resolve a SealerFactory. Default: defaultSealerFactory.
func (o RotateKEKOpts) buildSealer(kek []byte) (port.Sealer, error) {
	f := o.SealerFactory
	if f == nil {
		f = defaultSealerFactory
	}
	return f(kek)
}

// Rotate é a entrypoint público do usecase. Espelha a função interna
// rotateKEK para que callers externos usem um nome convencional.
func Rotate(
	ctx context.Context,
	repo ChannelCredentialsCrossTenant,
	auditRepo AuditRepository,
	opts RotateKEKOpts,
) (Report, error) {
	return rotateKEK(ctx, repo, auditRepo, opts)
}
