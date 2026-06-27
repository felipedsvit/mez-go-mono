// Package crypto — local_sealer.go: adapter que implementa port.Sealer e
// port.Encryptor usando a KEK local (master key em env var).
//
// Envelope encryption (C9 do PLAN.md §2):
//
//   - KEK = master key de 32 bytes (MEZ_MASTER_KEY, base64).
//   - DEK = 32 bytes aleatórios por tenant; gerados em SetCredentials.
//   - DEK cifrada por KEK é persistida como wrapped_dek (nonce||ciphertext).
//   - Credencial (Meta token, Telegram bot token) é cifrada por DEK e
//     persistida como encrypted.
//
// Por que DEK/tenant e não DEK único?
//
// Se todas as credenciais compartilhassem uma única DEK, a rotação da
// KEK (cmd/server rotate-kek) forçaria re-cifrar todas as credenciais de
// todos os tenants — O(N_credenciais). Com DEK/tenant, rotacionamos só
// o wrap da DEK (N_tenants linhas), que é O(N_tenants) e o ciphertext
// de cada credencial permanece válido (a DEK em si não muda).
//
// Trade-off: a DEK em claro vive em memória (no Keyring cache) durante
// o uso. Mitigações: TTL 5 min, `zero(dek)` após uso, single-process.
//
// Esta implementação é a única no 1.0 (não portamos
// `internal/adapter/secret/sealer/vault.go` do pai). A interface
// port.Encryptor permite plugar `VaultTransitEncryptor` pós-1.0 sem
// mexer no Keyring/usecase.
package crypto

import (
	"context"
	"encoding/base64"
	"fmt"

	pkgcrypto "github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

// LocalSealer implementa port.Sealer e port.Encryptor usando uma KEK
// (master key) local carregada na construção.
//
// Thread-safe: Envelope é imutável após NewEnvelope; não há estado mutável
// no adapter. Concorrência é responsabilidade dos callers (Keyring).
type LocalSealer struct {
	env *pkgcrypto.Envelope
}

// NewLocalSealer constrói o sealer a partir da master key em base64.
// Retorna erro se a chave não tiver 32 bytes decodificados.
func NewLocalSealer(masterKeyB64 string) (*LocalSealer, error) {
	env, err := pkgcrypto.NewEnvelope(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("local sealer: %w", err)
	}
	return &LocalSealer{env: env}, nil
}

// Envelope devolve o *pkgcrypto.Envelope subjacente. Usado para
// SealSystem/OpenSystem (Fase 10 — system_settings cifrado direto
// pela KEK, sem DEK/tenant).
func (s *LocalSealer) Envelope() *pkgcrypto.Envelope {
	return s.env
}

// Wrap cifra a DEK em claro com a KEK e devolve nonce||ciphertext.
// A saída tem nonce (12 bytes) + ciphertext (32 bytes + 16 tag) = 60 bytes.
func (s *LocalSealer) Wrap(ctx context.Context, plaintext []byte) ([]byte, error) {
	_ = ctx // LocalSealer não usa context (operação CPU-bound síncrona).
	return s.env.Wrap(plaintext)
}

// Unwrap decifra uma DEK wrapped e devolve a chave em plaintext (32 bytes).
// Retorna erro de GCM auth tag se a KEK estiver errada ou o ciphertext
// tiver sido adulterado.
func (s *LocalSealer) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error) {
	_ = ctx
	return s.env.Unwrap(wrapped)
}

// EncryptForTenant cifra plaintext sob a DEK identificada por wrappedDEK.
// Internamente: Unwrap(wrappedDEK) → DEK → Encrypt(plaintext) → esquece DEK.
func (s *LocalSealer) EncryptForTenant(wrappedDEK, plaintext []byte) ([]byte, error) {
	return s.env.Encrypt(wrappedDEK, plaintext)
}

// DecryptForTenant decifra ciphertext usando a DEK identificada por wrappedDEK.
// Retorna erro de GCM auth tag se a DEK ou o ciphertext estiverem errados.
func (s *LocalSealer) DecryptForTenant(wrappedDEK, ciphertext []byte) ([]byte, error) {
	return s.env.Decrypt(wrappedDEK, ciphertext)
}

// NewLocalSealerFromKEK constrói o sealer a partir da KEK em bytes raw
// (32 bytes). É o entrypoint usado pelo `cmd/server rotate-kek` (#92) e
// por testes que precisam injetar uma KEK específica.
//
// Preferir NewLocalSealer quando a KEK está em base64 (env var padrão).
func NewLocalSealerFromKEK(kek []byte) (*LocalSealer, error) {
	if len(kek) != 32 {
		return nil, fmt.Errorf("local sealer: KEK deve ter 32 bytes (got %d)", len(kek))
	}
	// Reaproveita o Envelope construindo a master key em base64. Envelope
	// não expõe construtor de bytes raw (mantém invariante: toda KEK é
	// string em algum ponto do boot). A codificação é determinística
	// e o custo é desprezível (rotação é offline).
	env, err := pkgcrypto.NewEnvelope(base64.StdEncoding.EncodeToString(kek))
	if err != nil {
		return nil, fmt.Errorf("local sealer: %w", err)
	}
	return &LocalSealer{env: env}, nil
}

// Compile-time guarantees: LocalSealer satisfaz ambas as interfaces de port.
var (
	_ interface {
		Wrap(ctx context.Context, plaintext []byte) ([]byte, error)
		Unwrap(ctx context.Context, wrapped []byte) ([]byte, error)
	} = (*LocalSealer)(nil)
)
