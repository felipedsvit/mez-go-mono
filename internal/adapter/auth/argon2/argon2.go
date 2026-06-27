package argon2

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

const MaxPlaintextBytes = 64

type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltBytes   uint32
	KeyBytes    uint32
}

func DefaultParams() Params {
	return Params{
		Memory:      64 * 1024, // 64 MiB
		Iterations:  3,
		Parallelism: 2,
		SaltBytes:   16,
		KeyBytes:    32,
	}
}

type Hasher struct {
	params Params
}

func New(p Params) *Hasher {
	if p.Memory == 0 {
		p.Memory = DefaultParams().Memory
	}
	if p.Iterations == 0 {
		p.Iterations = DefaultParams().Iterations
	}
	if p.Parallelism == 0 {
		p.Parallelism = DefaultParams().Parallelism
	}
	if p.SaltBytes == 0 {
		p.SaltBytes = DefaultParams().SaltBytes
	}
	if p.KeyBytes == 0 {
		p.KeyBytes = DefaultParams().KeyBytes
	}
	return &Hasher{params: p}
}

func (h *Hasher) Hash(ctx context.Context, plaintext string) (string, error) {
	if len(plaintext) > MaxPlaintextBytes {
		return "", admin.ErrPasswordTooLong
	}

	salt, err := generateRandomBytes(h.params.SaltBytes)
	if err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(plaintext), salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyBytes)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		h.params.Memory, h.params.Iterations, h.params.Parallelism, saltB64, hashB64)

	return encoded, nil
}

func (h *Hasher) Verify(ctx context.Context, encoded, plaintext string) (bool, error) {
	salt, hash, err := parseEncoded(encoded)
	if err != nil {
		return false, nil
	}

	computed := argon2.IDKey([]byte(plaintext), salt, h.params.Iterations, h.params.Memory, h.params.Parallelism, h.params.KeyBytes)

	return subtle.ConstantTimeCompare(hash, computed) == 1, nil
}

func parseEncoded(encoded string) (salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return nil, nil, fmt.Errorf("invalid format")
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, err
	}

	hash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, err
	}

	return salt, hash, nil
}

func generateRandomBytes(n uint32) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
