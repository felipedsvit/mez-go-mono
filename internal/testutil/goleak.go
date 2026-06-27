//go:build !integration
// +build !integration

// Package testutil — helpers compartilhados para testes.
//
// VerifyNone é um wrapper sobre goleak.VerifyNone para ser usado em
// subtests. Não use em arquivos _test com build tag `integration` (o
// testcontainer tem goroutines de cleanup que o goleak detecta como leak).
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
