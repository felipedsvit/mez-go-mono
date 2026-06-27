// Package testutil — helpers compartilhados para testes.
//
// VerifyNone e VerifyTestMain são wrappers sobre goleak para detecção
// de leak de goroutines. Não use goleak em arquivos _test com build tag
// `integration` (o testcontainer tem goroutines de cleanup que o goleak
// detecta como leak). O wrapper aqui é seguro para uso geral; testes
// de integração que precisem ignorar goroutines conhecidas devem usar
// `goleak.IgnoreTopFunction` como option.
package testutil

import (
	"testing"

	"go.uber.org/goleak"
)

// VerifyNone falha o teste se houver goroutines em leak. Equivalente a
// `goleak.VerifyNone(t)`, mas permite passar opções extras (ex:
// goleak.IgnoreCurrent, goleak.IgnoreTopFunction).
func VerifyNone(t *testing.T, opts ...goleak.Option) {
	t.Helper()
	goleak.VerifyNone(t, opts...)
}

// VerifyTestMain é um wrapper sobre goleak.VerifyTestMain. Use em
// `func TestMain(m *testing.M)` no primeiro arquivo *_test.go de cada
// pacote crítico (Fase 8 #100/105).
//
// Exemplo:
//
//	func TestMain(m *testing.M) { testutil.VerifyTestMain(m) }
func VerifyTestMain(m *testing.M, opts ...goleak.Option) {
	goleak.VerifyTestMain(m, opts...)
}
