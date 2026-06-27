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

// Wrap cifra a DEK em claro sob a KEK. Retorna nonce||ciphertext (formato
// consumível por Unwrap). É o equivalente público de wrapDEK, exposto para
// que adapters (ex.: LocalSealer) possam delegar sem expor o envelope
// internals.
func (e *Envelope) Wrap(dek []byte) ([]byte, error) {
	return e.wrapDEK(dek)
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

// Unwrap decifra uma DEK wrapped e devolve plaintext (32 bytes). Retorna
// erro de GCM auth tag se a KEK estiver errada ou o ciphertext adulterado.
func (e *Envelope) Unwrap(wrapped []byte) ([]byte, error) {
	return e.unwrapDEK(wrapped)
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

// SealSystem cifra plaintext usando a KEK diretamente (sem DEK/tenant).
// Usado para system_settings (Fase 10): valores cifrados pela master key
// do platform, lidos apenas por mez_platform/admin.
//
// Formato: nonce||ciphertext (mesmo padrão do Envelope.Encrypt).
// Retorna bytes raw (não base64) — caller pode codificar conforme storage.
func (e *Envelope) SealSystem(plaintext []byte) ([]byte, error) {
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
	return aesgcm.Seal(nonce, nonce, plaintext, nil), nil
}

// OpenSystem decifra um valor SealSystem-encrypted. Verifica auth tag
// do GCM — falha se a KEK estiver errada ou o ciphertext adulterado.
func (e *Envelope) OpenSystem(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.kek)
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
		return nil, fmt.Errorf("open system: %w", err)
	}
	return plaintext, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
