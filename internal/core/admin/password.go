package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — OWASP 2024 cheat sheet (issue #16).
//   - 64 MiB memory
//   - 3 iterations
//   - 2 parallelism
//   - 16-byte salt
//   - 32-byte key
const (
	argonMemory  uint32 = 64 * 1024
	argonTime    uint32 = 3
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// HashPassword hashes the plaintext password with Argon2id and returns the
// PHC-format string suitable for storage:
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt-b64>$<hash-b64>
func HashPassword(plain string) (string, error) {
	if len(plain) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("read salt: %w", err)
	}

	hash := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	enc := base64.RawStdEncoding.EncodeToString
	phc := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		enc(salt), enc(hash),
	)
	return phc, nil
}

// VerifyPassword verifies that the plaintext matches the PHC-format hash.
// Uses constant-time comparison via subtle.ConstantTimeCompare on the
// recomputed hash bytes.
func VerifyPassword(hash, plain string) bool {
	parts := strings.Split(hash, "$")
	// PHC format: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	if len(parts) != 6 {
		return false
	}
	if parts[1] != "argon2id" {
		return false
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false
	}
	if version != argon2.Version {
		return false
	}

	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}

	dec := base64.RawStdEncoding.DecodeString
	salt, err := dec(parts[4])
	if err != nil {
		return false
	}
	expected, err := dec(parts[5])
	if err != nil {
		return false
	}

	computed := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(expected))) //nolint:gosec // key length from PHC-encoded output, bounded by hash output size
	return subtle.ConstantTimeCompare(computed, expected) == 1
}
