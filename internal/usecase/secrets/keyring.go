// Package secrets — keyring.go: orquestrador de criptografia por tenant
// (Fase 7 #91).
//
// Keyring é o ponto de entrada para Encrypt/Decrypt de credenciais de
// canal. Ele compõe:
//
//   - CredentialsRepository: persiste (wrapped_dek, encrypted) por tenant.
//   - port.Encryptor:        cifra/decifra usando a DEK wrapped.
//   - dekCache:              cache in-memory de DEK (TTL 5min).
//
// O Keyring NÃO implementa nenhuma interface port.* — ele é um orquestrador
// concreto. A interface port.Encryptor fica no adapter (LocalSealer).
//
// Thread-safety: o cache tem mutex interno. O repo é stateless (delegado
// ao pgxpool). Não há outro estado mutável, então o Keyring é safe para
// uso concorrente.
package secrets

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// ErrCredentialsNotFound é retornado quando (tenant, channel) não tem
// credencial configurada. Mantido para compatibilidade com EnvCredentials
// (carryover Fase 3 #50).
var ErrCredentialsNotFound = errors.New("secrets: credenciais não configuradas")

// CredentialsRepository é a abstração que o Keyring consome para persistir
// e ler credenciais. Implementado por *postgres.ChannelCredentialsRepo (#90).
//
// Get/Upsert/Delete exigem RunInTenantTx no ctx (RLS fail-closed).
type CredentialsRepository interface {
	Get(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) (*domain.ChannelCredentials, error)
	Upsert(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, wrappedDEK, encrypted []byte, kekVersion int) error
	Delete(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) error
}

// GenerateDEKFn gera uma nova DEK (32 bytes) e a devolve wrapped pela KEK.
// Injetada como opção para que testes possam usar DEKs determinísticas.
// Default: usa port.Sealer.Wrap indiretamente via Keyring.
type GenerateDEKFn func(ctx context.Context) (dek, wrappedDEK []byte, kekVersion int, err error)

// KeyringOption configura o Keyring (functional options, padrão AGENTS.md).
type KeyringOption func(*Keyring)

// WithCacheTTL ajusta o TTL do cache in-memory de DEK. Default: 5min.
func WithCacheTTL(ttl time.Duration) KeyringOption {
	return func(k *Keyring) {
		k.cache.ttl = ttl
		if ttl > 0 {
			// Reaproveita o map existente (não reseta entries atuais).
			// Put() aplica novo TTL nas próximas inserções.
		}
	}
}

// WithGenerateDEK injeta o gerador de DEK. Útil para testes determinísticos.
func WithGenerateDEK(fn GenerateDEKFn) KeyringOption {
	return func(k *Keyring) {
		k.genDEK = fn
	}
}

// WithCurrentKEKVersion define a versão atual da KEK (default 1).
// Injetada no SetCredentials para registrar a kek_version inicial.
func WithCurrentKEKVersion(v int) KeyringOption {
	return func(k *Keyring) {
		k.kekVersion = v
	}
}

// Keyring orquestra encrypt/decrypt de credenciais por tenant.
type Keyring struct {
	repo       CredentialsRepository
	encryptor  port.Encryptor
	cache      *dekCache
	genDEK     GenerateDEKFn
	kekVersion int
	log        zerolog.Logger
}

// New constrói o Keyring com cache TTL 5min e KEK version 1 (defaults).
func New(repo CredentialsRepository, encryptor port.Encryptor, log zerolog.Logger, opts ...KeyringOption) *Keyring {
	k := &Keyring{
		repo:       repo,
		encryptor:  encryptor,
		cache:      newDEKCache(0), // 0 → default 5min
		kekVersion: 1,
		log:        log.With().Str("component", "secrets.Keyring").Logger(),
	}
	for _, o := range opts {
		o(k)
	}
	if k.genDEK == nil {
		k.genDEK = k.defaultGenDEK
	}
	return k
}

// SetCredentials grava (ou substitui) a credencial plaintext para
// (tenantID, channel). Gera nova DEK aleatória, cifra com KEK, cifra o
// plaintext com a DEK, persiste ambos, e invalida o cache.
//
// Idempotente: chamar duas vezes para o mesmo (tenant, channel) substitui
// a credencial e a DEK (rotação manual).
func (k *Keyring) SetCredentials(ctx context.Context, tenantID domain.TenantID, channel domain.Channel, plaintext []byte) error {
	dek, wrappedDEK, _, err := k.genDEK(ctx)
	if err != nil {
		return fmt.Errorf("set credentials: generate DEK: %w", err)
	}
	defer zero(dek)

	encrypted, err := k.encryptor.EncryptForTenant(wrappedDEK, plaintext)
	if err != nil {
		return fmt.Errorf("set credentials: encrypt plaintext: %w", err)
	}

	if err := k.repo.Upsert(ctx, tenantID, channel, wrappedDEK, encrypted, k.kekVersion); err != nil {
		return fmt.Errorf("set credentials: upsert: %w", err)
	}

	// Invalida o cache local para forçar re-fetch com a nova DEK no
	// próximo Encrypt/Decrypt.
	k.cache.Invalidate(string(tenantID))
	return nil
}

// ResolveCredentials decifra a credencial (tenant, channel) e devolve o
// plaintext. Erro ErrCredentialsNotFound se não houver credencial.
func (k *Keyring) ResolveCredentials(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) ([]byte, error) {
	cc, err := k.repo.Get(ctx, tenantID, channel)
	if err != nil {
		if errors.Is(err, port.ErrNotFound) {
			return nil, fmt.Errorf("%w: tenant=%s channel=%s", ErrCredentialsNotFound, tenantID, channel)
		}
		return nil, fmt.Errorf("resolve credentials: get: %w", err)
	}

	dek, _, ok := k.cache.Get(string(tenantID), cc.KEKVersion)
	if !ok {
		// Cache miss: decifra wrapped_dek e popula o cache.
		var unwrapErr error
		dek, unwrapErr = k.decryptWrappedDEK(ctx, cc.WrappedDEK)
		if unwrapErr != nil {
			return nil, fmt.Errorf("resolve credentials: unwrap DEK: %w", unwrapErr)
		}
		k.cache.Put(string(tenantID), dek, cc.WrappedDEK, cc.KEKVersion)
	}

	plaintext, err := k.encryptor.DecryptForTenant(cc.WrappedDEK, cc.Encrypted)
	if err != nil {
		return nil, fmt.Errorf("resolve credentials: decrypt: %w", err)
	}
	return plaintext, nil
}

// Delete remove a credencial. Invalida o cache para evitar uso de
// plaintext stale.
func (k *Keyring) Delete(ctx context.Context, tenantID domain.TenantID, channel domain.Channel) error {
	if err := k.repo.Delete(ctx, tenantID, channel); err != nil {
		return err
	}
	k.cache.Invalidate(string(tenantID))
	return nil
}

// Invalidate expurga o cache do tenant. Chamado pelo RotateKEK após
// re-wrap (issue #92) para forçar re-fetch do wrapped_dek novo.
func (k *Keyring) Invalidate(tenantID domain.TenantID) {
	k.cache.Invalidate(string(tenantID))
}

// KekVersion retorna a versão atual da KEK configurada (para a CLI).
func (k *Keyring) KekVersion() int {
	return k.kekVersion
}

// SetKekVersion atualiza a versão (usado pela CLI rotate-kek após ler
// MEZ_MASTER_KEY_NEW — passamos a usar v+1 implicitamente via o campo).
// Apenas a parte de cache é tocada aqui; o repo já gravou kek_version
// correto no UpdateWrappedDEK.
func (k *Keyring) SetKekVersion(v int) {
	k.kekVersion = v
}

// decryptWrappedDEK é helper que decifra o wrappedDEK usando o port.Sealer
// via type assertion (LocalSealer satisfaz Sealer; outros Encryptor
// podem não satisfazer). Fallback: erro explícito.
func (k *Keyring) decryptWrappedDEK(ctx context.Context, wrappedDEK []byte) ([]byte, error) {
	if sealer, ok := k.encryptor.(port.Sealer); ok {
		return sealer.Unwrap(ctx, wrappedDEK)
	}
	// Sem Sealer: o caller deveria ter passado um encryptor que não tem
	// Unwrap. Erro explícito.
	return nil, errors.New("secrets: encryptor não implementa port.Sealer (Unwrap indisponível)")
}

// defaultGenDEK é a implementação padrão de GenerateDEKFn: usa o Sealer
// (se disponível) para gerar DEK aleatória de 32 bytes e wrappear.
func (k *Keyring) defaultGenDEK(ctx context.Context) (dek, wrappedDEK []byte, kekVersion int, err error) {
	sealer, ok := k.encryptor.(port.Sealer)
	if !ok {
		return nil, nil, 0, errors.New("secrets: encryptor não implementa port.Sealer; forneça WithGenerateDEK")
	}
	dek = make([]byte, 32)
	if _, rerr := io.ReadFull(rand.Reader, dek); rerr != nil {
		return nil, nil, 0, fmt.Errorf("rand DEK: %w", rerr)
	}
	wrapped, werr := sealer.Wrap(ctx, dek)
	if werr != nil {
		return nil, nil, 0, fmt.Errorf("wrap DEK: %w", werr)
	}
	return dek, wrapped, k.kekVersion, nil
}
