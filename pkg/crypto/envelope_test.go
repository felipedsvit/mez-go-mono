package crypto_test

import (
	"bytes"
	"testing"

	"github.com/felipedsvit/mez-go-mono/pkg/crypto"
)

func TestEnvelope_EncryptDecrypt(t *testing.T) {
	// openssl rand -base64 32
	masterKeyB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	env, err := crypto.NewEnvelope(masterKeyB64)
	if err != nil {
		t.Fatal(err)
	}

	_, wrapped, err := env.GenerateDEK()
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("Hello, World! This is a secret credential.")
	ciphertext, err := env.Encrypt(wrapped, plaintext)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := env.Decrypt(wrapped, ciphertext)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("plaintext mismatch: got %s", decrypted)
	}
}

func TestNewEnvelope_InvalidKeyLength(t *testing.T) {
	// "short" base64 decodes to 3 bytes
	_, err := crypto.NewEnvelope("c2hvcnQ=")
	if err == nil {
		t.Error("expected error for short key")
	}
}

func TestEnvelope_SealOpenSystem(t *testing.T) {
	t.Parallel()

	masterKeyB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	env, err := crypto.NewEnvelope(masterKeyB64)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("postgres://mez_app:pass@host:5432/mez_whatsmeow")

	ciphertext, err := env.SealSystem(plaintext)
	if err != nil {
		t.Fatalf("SealSystem: %v", err)
	}

	decrypted, err := env.OpenSystem(ciphertext)
	if err != nil {
		t.Fatalf("OpenSystem: %v", err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("roundtrip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEnvelope_SealSystem_DifferentNonces(t *testing.T) {
	t.Parallel()

	masterKeyB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	env, _ := crypto.NewEnvelope(masterKeyB64)

	plaintext := []byte("same plaintext")

	c1, _ := env.SealSystem(plaintext)
	c2, _ := env.SealSystem(plaintext)

	if bytes.Equal(c1, c2) {
		t.Error("two Seals of same plaintext should produce different ciphertexts (different nonces)")
	}
}

func TestEnvelope_OpenSystem_TamperedFails(t *testing.T) {
	t.Parallel()

	masterKeyB64 := "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg="
	env, _ := crypto.NewEnvelope(masterKeyB64)

	ciphertext, _ := env.SealSystem([]byte("original"))

	// Adultera o último byte (parte do auth tag).
	ciphertext[len(ciphertext)-1] ^= 0xff

	_, err := env.OpenSystem(ciphertext)
	if err == nil {
		t.Error("expected GCM auth tag failure for tampered ciphertext")
	}
}

func TestEnvelope_OpenSystem_DifferentKeyFails(t *testing.T) {
	t.Parallel()

	env1, _ := crypto.NewEnvelope("qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg=")
	env2, _ := crypto.NewEnvelope("kJsYV2qOQrJM4zLp8mKp4P3Vt8sX9z0T4sX9z0T4sX9=") // 32 bytes

	ciphertext, _ := env1.SealSystem([]byte("secret"))

	_, err := env2.OpenSystem(ciphertext)
	if err == nil {
		t.Error("expected OpenSystem to fail with wrong key")
	}
}
