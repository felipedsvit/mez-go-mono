package argon2

import (
	"context"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	h := New(DefaultParams())
	ctx := context.Background()

	password := "correct-horse-battery-staple"
	encoded, err := h.Hash(ctx, password)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=") {
		t.Errorf("unexpected hash format: %s", encoded)
	}

	valid, err := h.Verify(ctx, encoded, password)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Errorf("expected valid password")
	}

	valid, err = h.Verify(ctx, encoded, "wrong-password")
	if err != nil {
		t.Fatalf("Verify wrong: %v", err)
	}
	if valid {
		t.Errorf("expected invalid password")
	}
}

func TestHashTooLong(t *testing.T) {
	h := New(DefaultParams())
	ctx := context.Background()

	longPassword := strings.Repeat("a", MaxPlaintextBytes+1)

	_, err := h.Hash(ctx, longPassword)
	if err == nil {
		t.Fatal("expected ErrPasswordTooLong")
	}
}

func TestVerifyInvalidEncoded(t *testing.T) {
	h := New(DefaultParams())
	ctx := context.Background()

	valid, err := h.Verify(ctx, "invalid-format", "password")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if valid {
		t.Errorf("expected invalid")
	}
}

func TestCustomParams(t *testing.T) {
	h := New(Params{
		Memory:      64 * 1024,
		Iterations:  1,
		Parallelism: 1,
		SaltBytes:   16,
		KeyBytes:    32,
	})
	ctx := context.Background()

	encoded, err := h.Hash(ctx, "test-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	valid, err := h.Verify(ctx, encoded, "test-password")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !valid {
		t.Errorf("expected valid password")
	}
}
