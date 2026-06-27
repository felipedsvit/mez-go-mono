//go:build integration
// +build integration

// Package boot — helpers compartilhados.
package boot

// itoa converte int em string (helper local para evitar import strconv
// em testes que não precisam de mais).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	negative := i < 0
	if negative {
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
