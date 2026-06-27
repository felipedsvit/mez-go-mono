// Package config — helpers de entropy para validar secrets de bootstrap.
//
// Issue #142 (Sprint 0B H6b audit): além do length check (já feito em
// ValidateServe para APIJWTSecret), rejeitar segredos com baixa entropia
// estimada. Um secret de 32 chars com todos os bytes iguais tem
// Shannon entropy = 0 — claramente gerado por humano ou bug.
//
// Limiar empírico de 3.5 bits/char filtra segredos previsíveis sem
// gerar friction em secrets gerados por openssl rand (entropy ~5.5).
package config

import "math"

// MinEntropyBits é o threshold abaixo do qual um secret é considerado
// fraco. 3.5 é empírico: secrets random de 32+ chars têm ≥ 4.5; segredos
// humanos tipicamente < 3.0. Ver TestShannonEntropy para casos.
const MinEntropyBits = 3.5

// ShannonEntropy retorna a entropia de Shannon em bits/char de s.
// Fórmula: H = -Σ p(c) * log2(p(c)) para cada char único c em s.
//
// Retorna 0 para string vazia.
func ShannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	counts := make(map[rune]int)
	for _, c := range s {
		counts[c]++
	}
	length := float64(len(s))
	var h float64
	for _, n := range counts {
		p := float64(n) / length
		h -= p * math.Log2(p)
	}
	return h
}

// IsStrongSecret retorna true se s tem pelo menos minBytes de length
// E Shannon entropy ≥ MinEntropyBits. Usado em ValidateServe como gate
// adicional ao length check.
func IsStrongSecret(s string, minBytes int) bool {
	if len(s) < minBytes {
		return false
	}
	return ShannonEntropy(s) >= MinEntropyBits
}
