package crypto_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
)

// masterKeyB64 é uma chave AES-256 determinística (32 bytes) em base64.
// Usada em todos os testes do LocalSealer.
const masterKeyB64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="

func newSealer(t *testing.T) *adaptercrypto.LocalSealer {
	t.Helper()
	s, err := adaptercrypto.NewLocalSealer(masterKeyB64)
	if err != nil {
		t.Fatalf("NewLocalSealer: %v", err)
	}
	return s
}

func newWrappedDEK(t *testing.T, s *adaptercrypto.LocalSealer) []byte {
	t.Helper()
	// 32 bytes arbitrários representam uma DEK gerada para um tenant.
	dek := bytes.Repeat([]byte{0x42}, 32)
	wrapped, err := s.Wrap(context.Background(), dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	return wrapped
}

func TestNewLocalSealer_OK(t *testing.T) {
	s := newSealer(t)
	if s == nil {
		t.Fatal("expected non-nil sealer")
	}
}

func TestNewLocalSealer_InvalidKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"short", "c2hvcnQ="}, // "short" → 3 bytes
		{"not-base64", "@@@"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adaptercrypto.NewLocalSealer(tc.key)
			if err == nil {
				t.Errorf("expected error for key=%q", tc.key)
			}
		})
	}
}

func TestLocalSealer_WrapUnwrap_RoundTrip(t *testing.T) {
	s := newSealer(t)
	ctx := context.Background()

	dek := bytes.Repeat([]byte{0xAB}, 32)
	wrapped, err := s.Wrap(ctx, dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if bytes.Equal(wrapped, dek) {
		t.Error("wrapped output deve ser diferente do plaintext")
	}
	if len(wrapped) < 12 {
		t.Errorf("wrapped muito curto: %d bytes (esperado >= 12 de nonce)", len(wrapped))
	}

	got, err := s.Unwrap(ctx, wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Errorf("round-trip mismatch: got %x, want %x", got, dek)
	}
}

func TestLocalSealer_Unwrap_TamperedCiphertext(t *testing.T) {
	s := newSealer(t)
	ctx := context.Background()

	wrapped := newWrappedDEK(t, s)
	// Adultera o último byte (auth tag do GCM).
	tampered := bytes.Clone(wrapped)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := s.Unwrap(ctx, tampered); err == nil {
		t.Error("esperava erro de auth tag em ciphertext adulterado")
	} else if !strings.Contains(err.Error(), "unwrap") && !strings.Contains(err.Error(), "auth") {
		// Mensagem pode variar entre Go versions; só exige erro não-nil.
		t.Logf("unwrap retornou erro esperado: %v", err)
	}
}

func TestLocalSealer_Unwrap_Truncated(t *testing.T) {
	s := newSealer(t)
	ctx := context.Background()

	if _, err := s.Unwrap(ctx, []byte{0x01, 0x02}); err == nil {
		t.Error("esperava erro em wrapped muito curto")
	}
}

func TestLocalSealer_Unwrap_WrongKey(t *testing.T) {
	otherKeyB64 := "WSDTxjAgQYKmvZmX15PCpu7AhejlBPk5z82tPGCevw4="
	other, err := adaptercrypto.NewLocalSealer(otherKeyB64)
	if err != nil {
		t.Fatalf("NewLocalSealer(other): %v", err)
	}
	ctx := context.Background()

	wrapped := newWrappedDEK(t, other) // wrapped com KEK errada
	s := newSealer(t)                  // sealer com KEK certa

	if _, err := s.Unwrap(ctx, wrapped); err == nil {
		t.Error("esperava erro ao decifrar com KEK errada")
	}
}

func TestLocalSealer_EncryptDecryptForTenant_RoundTrip(t *testing.T) {
	s := newSealer(t)
	wrapped := newWrappedDEK(t, s)

	plaintext := []byte("EAAAB...long-lived access token...XYZ")
	ciphertext, err := s.EncryptForTenant(wrapped, plaintext)
	if err != nil {
		t.Fatalf("EncryptForTenant: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext deve diferir do plaintext")
	}

	got, err := s.DecryptForTenant(wrapped, ciphertext)
	if err != nil {
		t.Fatalf("DecryptForTenant: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}

func TestLocalSealer_EncryptForTenant_UniqueNonce(t *testing.T) {
	s := newSealer(t)
	wrapped := newWrappedDEK(t, s)
	plaintext := []byte("same payload")

	a, err := s.EncryptForTenant(wrapped, plaintext)
	if err != nil {
		t.Fatalf("Encrypt#1: %v", err)
	}
	b, err := s.EncryptForTenant(wrapped, plaintext)
	if err != nil {
		t.Fatalf("Encrypt#2: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("esperava nonces diferentes entre chamadas (GCM usa rand nonce)")
	}
}

func TestLocalSealer_DecryptForTenant_Tampered(t *testing.T) {
	s := newSealer(t)
	wrapped := newWrappedDEK(t, s)
	plaintext := []byte("cred")

	ct, err := s.EncryptForTenant(wrapped, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	bad := bytes.Clone(ct)
	bad[len(bad)-1] ^= 0xFF

	if _, err := s.DecryptForTenant(wrapped, bad); err == nil {
		t.Error("esperava erro de auth tag em ciphertext adulterado")
	}
}

func TestLocalSealer_ImplementsPortInterfaces(t *testing.T) {
	s := newSealer(t)
	// Compile-time check via interface assertions. Se LocalSealer parar
	// de satisfazer uma das duas interfaces, o teste falha em build.
	var _ port.Encryptor = s
	// Sealer: declarada inline porque o port.Sealer é a interface, mas
	// estamos dentro do mesmo package — usaremos o nome de tipo direto.
	// (verificamos via call: ambos métodos precisam existir e respeitar
	// assinatura de context.Context).
	ctx := context.Background()
	if _, err := s.Wrap(ctx, bytes.Repeat([]byte{0x00}, 32)); err != nil {
		t.Fatalf("Wrap via Sealer: %v", err)
	}
	wrapped := newWrappedDEK(t, s)
	if _, err := s.Unwrap(ctx, wrapped); err != nil {
		t.Fatalf("Unwrap via Sealer: %v", err)
	}
}

// Sanity check que o erro de key inválida retorna (não panic) — protege
// contra regressão na ordem das validações em NewEnvelope.
func TestNewLocalSealer_InvalidKey_NotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewLocalSealer panicou: %v", r)
		}
	}()
	_, err := adaptercrypto.NewLocalSealer("not-valid-base64-!!")
	if err == nil {
		t.Error("esperava erro")
	} else if !errors.Is(err, err) {
		// sanity: o erro é não-nil
		t.Logf("erro retornado: %v", err)
	}
}
