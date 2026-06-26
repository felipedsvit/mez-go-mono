package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

type Envelope struct {
	kek []byte
}

func NewEnvelope(masterKeyBase64 string) (*Envelope, error) {
	kek, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(kek) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes (got %d)", len(kek))
	}
	return &Envelope{kek: kek}, nil
}

func (e *Envelope) GenerateDEK() ([]byte, []byte, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return nil, nil, fmt.Errorf("generate DEK: %w", err)
	}
	wrapped, err := e.wrapDEK(dek)
	if err != nil {
		return nil, nil, err
	}
	return dek, wrapped, nil
}

func (e *Envelope) wrapDEK(dek []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.kek)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return aesgcm.Seal(nonce, nonce, dek, nil), nil
}

func (e *Envelope) unwrapDEK(wrapped []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.kek)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	nonceSize := aesgcm.NonceSize()
	if len(wrapped) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := wrapped[:nonceSize], wrapped[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("unwrap DEK: %w", err)
	}
	return plaintext, nil
}

func (e *Envelope) Encrypt(wrappedDEK, plaintext []byte) ([]byte, error) {
	dek, err := e.unwrapDEK(wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer zero(dek)

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return aesgcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (e *Envelope) Decrypt(wrappedDEK, ciphertext []byte) ([]byte, error) {
	dek, err := e.unwrapDEK(wrappedDEK)
	if err != nil {
		return nil, err
	}
	defer zero(dek)

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}
	nonceSize := aesgcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aesgcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
