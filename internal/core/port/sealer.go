package port

import "context"

// Sealer abstrai wrapping/unwrapping de chaves (envelope encryption) para
// data encryption keys (DEKs) por tenant. A implementação concreta vive em
// um adapter; usecases e ports dependem apenas desta interface.
//
// Por que Sealer e Encryptor são separados?
//
//   - Sealer opera em "chaves": recebe plaintext (32 bytes) e devolve
//     wrapped (KEK cifrou). Não conhece credenciais de tenant, não conhece
//     payload de negócio.
//
//   - Encryptor opera em "dados": recebe wrapped DEK + plaintext arbitrário
//     e devolve ciphertext (DEK do tenant cifrou). Encapsula a operação
//     "decifra DEK → cifra dados → esquece DEK", expondo apenas entrada/
//     saída. Usado pelo Keyring para hot-path de Encrypt/Decrypt.
type Sealer interface {
	// Wrap cifra plaintext sob a KEK (master key) e devolve a forma wrapped.
	Wrap(ctx context.Context, plaintext []byte) ([]byte, error)
	// Unwrap decifra uma DEK wrapped e devolve a chave em plaintext (32 bytes).
	Unwrap(ctx context.Context, wrapped []byte) ([]byte, error)
}

// Encryptor cifra/decifra dados por tenant usando uma DEK wrapped. É a
// abstração de alto nível consumida pelo Keyring: o caller entrega o
// wrappedDEK (persistido) e o plaintext, sem nunca manusear a DEK em claro.
//
// Substitui a antiga interface `Keyring` (Fase 7 #88) — a renomeação
// explicita que o port é só de criptografia, não de cache nem de lookup
// (essas responsabilidades vivem em usecase/secrets.Keyring).
type Encryptor interface {
	// EncryptForTenant cifra plaintext sob a DEK identificada por wrappedDEK.
	// Retorna nonce||ciphertext no formato esperado pelo DecryptForTenant.
	EncryptForTenant(wrappedDEK, plaintext []byte) ([]byte, error)
	// DecryptForTenant decifra ciphertext usando a DEK identificada por wrappedDEK.
	// Retorna erro de auth tag se o ciphertext foi adulterado ou a DEK errada.
	DecryptForTenant(wrappedDEK, ciphertext []byte) ([]byte, error)
}
