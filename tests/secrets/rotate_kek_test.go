//go:build integration

// Tests E2E do rotate-kek (Fase 7 #92). Cobre:
//   - Re-wrap completo: kek_version 1→2, wrapped_dek muda, encrypted
//     inalterado, Resolve com KEK nova funciona, KEK antiga falha.
//   - Dry-run não persiste nada.
//   - Audit log: 1 started + N tenant + 1 complete.
//
// Helpers compartilhados (fixture, auditAdapter) ficam em keyring_test.go.

package secrets

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
)

// TestE2E_RotateKEK_ReWrap valida o caminho completo: cria 3 tenants × 4
// canais = 12 credenciais, roda Rotate via Go (não shell), e verifica:
//   - kek_version 1→2 em todas as linhas
//   - wrapped_dek mudou (re-wrap com KEK nova)
//   - encrypted INALTERADO (a DEK em si não muda)
//   - Resolve pós-rotação funciona com a KEK nova
//   - Audit log: 1 started + 12 tenant + 1 complete
func TestE2E_RotateKEK_ReWrap(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	oldKEKB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	newKEKB64 := "WSDTxjAgQYKmvZmX15PCpu7AhejlBPk5z82tPGCevw4="

	kr := newKeyring(t, fx, oldKEKB64)
	oldSeal, err := adaptercrypto.NewLocalSealer(oldKEKB64)
	require.NoError(t, err)
	newSeal, err := adaptercrypto.NewLocalSealer(newKEKB64)
	require.NoError(t, err)

	tenants := []domain.TenantID{
		fx.seedTenant(t, ctx, "alpha"),
		fx.seedTenant(t, ctx, "beta"),
		fx.seedTenant(t, ctx, "gamma"),
	}
	channels := []domain.Channel{
		domain.ChannelWABA, domain.ChannelIG, domain.ChannelMSG, domain.ChannelTGBot,
	}

	// 1. Cria 12 credenciais.
	type row struct {
		tenantID   domain.TenantID
		channel    domain.Channel
		encrypted  []byte
		wrappedDEK []byte
	}
	var before []row
	for _, tid := range tenants {
		for _, ch := range channels {
			require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tid, func(ctx context.Context) error {
				return kr.SetCredentials(ctx, tid, ch, []byte(fmt.Sprintf("plaintext-%s-%s", tid, ch)))
			}))
		}
	}
	for _, tid := range tenants {
		for _, ch := range channels {
			require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tid, func(ctx context.Context) error {
				cc, err := fx.credsRepo.Get(ctx, tid, ch)
				if err != nil {
					return err
				}
				before = append(before, row{
					tenantID: tid, channel: ch,
					encrypted:  append([]byte(nil), cc.Encrypted...),
					wrappedDEK: append([]byte(nil), cc.WrappedDEK...),
				})
				return nil
			}))
		}
	}
	require.Len(t, before, 12)

	// 2. Roda Rotate.
	auditRepo := &auditAdapter{pool: fx.platformPool}
	report, rerr := secrets.Rotate(ctx, fx.credsRepo, auditRepo, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKB64,
		NewKEKBase64: newKEKB64,
		Actor:        "operator:e2e",
		InvalidateFn: func(t domain.TenantID) { kr.Invalidate(t) },
	})
	require.NoError(t, rerr)
	require.Empty(t, report.Errors)
	require.Equal(t, 3, report.Tenants)
	require.Equal(t, 12, report.Channels)
	require.Equal(t, 1, report.OldVersion)
	require.Equal(t, 2, report.NewVersion)

	// 3. Valida pós-rotação.
	for _, b := range before {
		require.NoError(t, fx.txRunner.RunInTenantTx(ctx, b.tenantID, func(ctx context.Context) error {
			cc, err := fx.credsRepo.Get(ctx, b.tenantID, b.channel)
			if err != nil {
				return err
			}
			// encrypted inalterado
			if string(cc.Encrypted) != string(b.encrypted) {
				return fmt.Errorf("%s/%s: encrypted mudou", b.tenantID, b.channel)
			}
			// wrapped_dek mudou
			if string(cc.WrappedDEK) == string(b.wrappedDEK) {
				return fmt.Errorf("%s/%s: wrapped_dek NÃO mudou", b.tenantID, b.channel)
			}
			// kek_version 1→2
			if cc.KEKVersion != 2 {
				return fmt.Errorf("%s/%s: kek_version=%d, esperava 2", b.tenantID, b.channel, cc.KEKVersion)
			}
			// Decifra com KEK NOVA — deve dar o plaintext original.
			pt, derr := newSeal.DecryptForTenant(cc.WrappedDEK, cc.Encrypted)
			if derr != nil {
				return fmt.Errorf("%s/%s: decrypt new KEK: %w", b.tenantID, b.channel, derr)
			}
			want := fmt.Sprintf("plaintext-%s-%s", b.tenantID, b.channel)
			if string(pt) != want {
				return fmt.Errorf("%s/%s: got %q want %q", b.tenantID, b.channel, pt, want)
			}
			// E com a KEK ANTIGA o wrapped_dek NOVO não decifra (sanity).
			if _, derr := oldSeal.DecryptForTenant(cc.WrappedDEK, cc.Encrypted); derr == nil {
				return fmt.Errorf("%s/%s: wrapped_dek novo decifrou com KEK antiga (impossível)", b.tenantID, b.channel)
			}
			return nil
		}))
	}

	// 4. Audit log: started + 12 tenant + 1 complete.
	started := auditRepo.byAction(admin.ActionRotateKEKStarted)
	require.Len(t, started, 1)
	tenantActions := auditRepo.byAction(admin.ActionRotateKEKTenant)
	require.Len(t, tenantActions, 12)
	complete := auditRepo.byAction(admin.ActionRotateKEKComplete)
	require.Len(t, complete, 1)
}

// TestE2E_RotateKEK_DryRun valida que dry-run não persiste nada.
func TestE2E_RotateKEK_DryRun(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	oldKEKB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	newKEKB64 := "WSDTxjAgQYKmvZmX15PCpu7AhejlBPk5z82tPGCevw4="

	kr := newKeyring(t, fx, oldKEKB64)
	tenant := fx.seedTenant(t, ctx, "T")
	channel := domain.ChannelWABA

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, []byte("x"))
	}))

	var beforeWrapped []byte
	var beforeKekVer int
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		cc, err := fx.credsRepo.Get(ctx, tenant, channel)
		if err != nil {
			return err
		}
		beforeWrapped = append([]byte(nil), cc.WrappedDEK...)
		beforeKekVer = cc.KEKVersion
		return nil
	}))

	auditRepo := &auditAdapter{pool: fx.platformPool}
	report, err := secrets.Rotate(ctx, fx.credsRepo, auditRepo, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKB64,
		NewKEKBase64: newKEKB64,
		DryRun:       true,
		Actor:        "operator:e2e",
	})
	require.NoError(t, err)
	require.True(t, report.DryRun)
	require.Equal(t, 1, report.Channels)

	// Estado inalterado no DB.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		cc, err := fx.credsRepo.Get(ctx, tenant, channel)
		if err != nil {
			return err
		}
		if string(cc.WrappedDEK) != string(beforeWrapped) {
			return fmt.Errorf("dry-run mutou wrapped_dek")
		}
		if cc.KEKVersion != beforeKekVer {
			return fmt.Errorf("dry-run mutou kek_version")
		}
		return nil
	}))
}

// TestE2E_RotateKEK_PostRotateDecrypt valida que, após rotação, um Keyring
// instanciado com a KEK NOVA consegue decifrar; e o Keyring com a KEK
// ANTIGA falha (esperado — wrapped_dek mudou).
func TestE2E_RotateKEK_PostRotateDecrypt(t *testing.T) {
	fx := newFixture(t)
	defer fx.close()
	ctx := context.Background()

	oldKEKB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	newKEKB64 := "WSDTxjAgQYKmvZmX15PCpu7AhejlBPk5z82tPGCevw4="

	// Keyring começa com KEK antiga.
	kr := newKeyring(t, fx, oldKEKB64)
	tenant := fx.seedTenant(t, ctx, "T")
	channel := domain.ChannelTGBot

	plaintext := []byte("my-secret-token")
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		return kr.SetCredentials(ctx, tenant, channel, plaintext)
	}))

	// Resolve pré-rotação.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, plaintext, got)
		return nil
	}))

	// Roda rotação (com InvalidateFn para limpar o cache).
	auditRepo := &auditAdapter{pool: fx.platformPool}
	_, err := secrets.Rotate(ctx, fx.credsRepo, auditRepo, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKB64,
		NewKEKBase64: newKEKB64,
		Actor:        "operator:e2e",
		InvalidateFn: func(t domain.TenantID) { kr.Invalidate(t) },
	})
	require.NoError(t, err)

	// kr novo (com KEK NOVA) decifra normalmente.
	newSeal, err := adaptercrypto.NewLocalSealer(newKEKB64)
	require.NoError(t, err)
	krNew := secrets.New(fx.credsRepo, newSeal, zerolog.Nop())

	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		got, err := krNew.ResolveCredentials(ctx, tenant, channel)
		require.NoError(t, err)
		require.Equal(t, plaintext, got)
		return nil
	}))

	// kr velho (KEK ANTIGA) não decifra mais — wrapped_dek nova, KEK errada.
	require.NoError(t, fx.txRunner.RunInTenantTx(ctx, tenant, func(ctx context.Context) error {
		_, err := kr.ResolveCredentials(ctx, tenant, channel)
		require.Error(t, err, "kr com KEK antiga deve falhar ao decifrar wrapped_dek novo")
		return nil
	}))
}
