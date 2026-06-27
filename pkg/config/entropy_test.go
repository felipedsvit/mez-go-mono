package config

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"strings"
	"testing"
)

func TestShannonEntropy(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want float64 // aproximado
	}{
		{"empty", "", 0},
		{"single char", "a", 0},
		{"all same 32", strings.Repeat("a", 32), 0},
		{"ab alternating", strings.Repeat("ab", 16), 1.0},
		{"abc repeating", strings.Repeat("abc", 11), 1.58}, // log2(3) ≈ 1.585
		{"hex random 64", randomHex(t, 64), 3.5},           // ≥ 3.5 esperado (256 símbolos)
		{"base64 random 64", randomB64(t, 64), 4.0},        // ≥ 4 esperado (64 símbolos)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShannonEntropy(c.in)
			// Para aleatórios, checar ≥ want; para determinísticos, exato
			if c.in == "" || len(setRunes(c.in)) == 1 {
				if got != 0 {
					t.Fatalf("expected 0, got %f", got)
				}
				return
			}
			if len(setRunes(c.in)) == 2 {
				if math.Abs(got-1.0) > 0.01 {
					t.Fatalf("expected ~1.0, got %f", got)
				}
				return
			}
			if got < c.want-0.01 {
				t.Fatalf("expected >= %f, got %f", c.want, got)
			}
		})
	}
}

func TestIsStrongSecret(t *testing.T) {
	// Fraco: 32 chars all-same
	if IsStrongSecret(strings.Repeat("a", 32), 32) {
		t.Fatal("secret all-same deve ser fraco")
	}
	// Fraco: 32 chars com pouca variedade
	if IsStrongSecret("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab", 32) {
		t.Fatal("secret com 1 byte diferente deve ser fraco")
	}
	// Fraco: curto
	if IsStrongSecret("abc", 32) {
		t.Fatal("secret curto deve ser fraco")
	}
	// Forte: 64 chars random hex (16 símbolos × 64 chars → ~4 bits)
	if !IsStrongSecret(randomHex(t, 64), 32) {
		t.Fatal("secret hex 64 chars random deve ser forte")
	}
	// Forte: 64 chars base64 (64 símbolos × 64 chars → ~6 bits)
	if !IsStrongSecret(randomB64(t, 64), 32) {
		t.Fatal("secret base64 64 chars random deve ser forte")
	}
}

func TestMinEntropyThreshold(t *testing.T) {
	// Verificar que o threshold 3.5 está calibrado para filtrar o
	// que precisamos (não humanos, não padrões repetitivos).
	weakExamples := []string{
		strings.Repeat("a", 32),
		strings.Repeat("ab", 16),           // 2 chars
		"password123password123password12", // 24 chars, 9 unique
		"qwertyqwertyqwertyqwertyqwerty",   // 30 chars, 6 unique
	}
	for _, s := range weakExamples {
		if IsStrongSecret(s, 32) {
			t.Errorf("expected weak: %q (entropy=%.2f)", s, ShannonEntropy(s))
		}
	}
}

func randomHex(t *testing.T, nChars int) string {
	t.Helper()
	b := make([]byte, (nChars+1)/2)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	s := hex.EncodeToString(b)
	if len(s) > nChars {
		s = s[:nChars]
	}
	return s
}

func randomB64(t *testing.T, nChars int) string {
	t.Helper()
	b := make([]byte, nChars)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	out := make([]byte, nChars)
	for i := 0; i < nChars; i++ {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

func setRunes(s string) map[rune]struct{} {
	m := make(map[rune]struct{})
	for _, c := range s {
		m[c] = struct{}{}
	}
	return m
}
