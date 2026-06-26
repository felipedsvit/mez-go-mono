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
