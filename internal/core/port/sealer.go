package port

import "context"

// Sealer abstracts key wrapping/unwrapping (envelope encryption) for
// per-tenant data encryption keys (DEKs). The actual implementation lives
// in an adapter, while usecases and ports depend on this interface only.
type Sealer interface {
	// Wrap encrypts plaintext under a master key and returns the wrapped form.
	Wrap(ctx context.Context, plaintext []byte) ([]byte, error)
	// Unwrap decrypts a wrapped DEK and returns the plaintext key.
	Unwrap(ctx context.Context, wrapped []byte) ([]byte, error)
}

// Keyring is a higher-level helper for symmetric encryption with a per-tenant
// data key. The implementation is responsible for fetching, wrapping and
// unwrapping keys.
type Keyring interface {
	Encrypt(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error)
}
