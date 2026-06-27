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

// encodedParts é o resultado de parseEncoded. Issue #156 (M9 audit,
// Sprint 0C): Verify precisa dos params ORIGINAIS (m, t, p) do hash
// para recalcular, não os params do hasher atual. Sem isso, aumentar
// params no futuro tranca todos os users existentes (forward migration
// impossível sem password reset week).
type encodedParts struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
	salt        []byte
	hash        []byte
}

func (h *Hasher) Verify(ctx context.Context, encoded, plaintext string) (bool, error) {
	parts, err := parseEncoded(encoded)
	if err != nil {
		return false, nil
	}

	// Issue #156 (M9): usa params do hash encoded, não h.params.
	// CWE-757 (Selection of Less-Secure Algorithm During Negotiation).
	computed := argon2.IDKey(
		[]byte(plaintext),
		parts.salt,
		parts.iterations,
		parts.memory,
		parts.parallelism,
		uint32(len(parts.hash)),
	)

	return subtle.ConstantTimeCompare(parts.hash, computed) == 1, nil
}

func parseEncoded(encoded string) (encodedParts, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return encodedParts{}, fmt.Errorf("invalid format")
	}

	// Formato: $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
	// parts[0]="" parts[1]="argon2id" parts[2]="v=19" parts[3]="m=...,t=...,p=..." parts[4]=salt parts[5]=hash
	if parts[1] != "argon2id" {
		return encodedParts{}, fmt.Errorf("unsupported variant: %q", parts[1])
	}

	var p encodedParts
	var mFlag, tFlag, pFlag bool
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "m":
			fmt.Sscanf(v, "%d", &p.memory)
			mFlag = true
		case "t":
			fmt.Sscanf(v, "%d", &p.iterations)
			tFlag = true
		case "p":
			fmt.Sscanf(v, "%d", &p.parallelism)
			pFlag = true
		}
	}
	if !mFlag || !tFlag || !pFlag {
		return encodedParts{}, fmt.Errorf("missing params: m=%v t=%v p=%v", mFlag, tFlag, pFlag)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return encodedParts{}, fmt.Errorf("decode salt: %w", err)
	}
	p.salt = salt

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return encodedParts{}, fmt.Errorf("decode hash: %w", err)
	}
	p.hash = hash

	return p, nil
}

func generateRandomBytes(n uint32) ([]byte, error) {
	b := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
