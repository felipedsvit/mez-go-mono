//go:build !integration
// +build !integration

package backup

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"

	cdomain "github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/testutil"
)

// TestMain garante goleak.VerifyTestMain para o package.
func TestMain(m *testing.M) {
	testutil.VerifyTestMain(m)
}

// fakeAudit captura audit rows gravadas via Record.
type fakeAudit struct {
	rows []*cdomain.AuditEntry
	err  error
}

func (f *fakeAudit) Record(ctx context.Context, entry *cdomain.AuditEntry) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, entry)
	return nil
}

func (f *fakeAudit) RecordWithTx(ctx context.Context, tx cdomain.Tx, entry *cdomain.AuditEntry) error {
	return f.Record(ctx, entry)
}

func (f *fakeAudit) List(ctx context.Context, filter cdomain.AuditFilter) ([]cdomain.AuditEntry, error) {
	return nil, nil
}

// TestRunAsPlatform_BestEffortFallbackWithoutAdminDB valida o
// fallback legacy (Issue #148): sem adminDB, o audit é best-effort.
// A fn é executada; o audit row C5 é gravado via Record (não
// atômico). Se a fn falhar, o audit fica "fantasma".
func TestRunAsPlatform_BestEffortFallbackWithoutAdminDB(t *testing.T) {
	audit := &fakeAudit{}
	s := &Service{
		log:    zerolog.Nop(),
		audit:  audit,
		// adminDB: nil — fallback legacy
	}
	actor := cdomain.Actor{ID: "u1", Email: "admin@local"}

	fnCalled := false
	err := s.runAsPlatform(context.Background(), actor, cdomain.ActionTenantBackup,
		"backup-1", "backup", "tenant-1",
		map[string]any{"tables": 5},
		func(ctx context.Context) error {
			fnCalled = true
			return nil
		})
	if err != nil {
		t.Fatalf("runAsPlatform retornou erro: %v", err)
	}
	if !fnCalled {
		t.Error("fn não foi chamada")
	}
	if got := len(audit.rows); got != 1 {
		t.Errorf("audit rows = %d, want 1", got)
	}
	if got := audit.rows[0].Action; got != cdomain.ActionPlatformAccess {
		t.Errorf("audit action = %v, want %v", got, cdomain.ActionPlatformAccess)
	}
}

// TestRunAsPlatform_BestEffortPropagatesFnError valida que o
// fallback best-effort propaga erro da fn (mas o audit C5 já foi
// gravado — limitation documentada em Issue #148).
func TestRunAsPlatform_BestEffortPropagatesFnError(t *testing.T) {
	audit := &fakeAudit{}
	s := &Service{log: zerolog.Nop(), audit: audit}
	actor := cdomain.Actor{ID: "u1", Email: "admin@local"}

	wantErr := errors.New("fn falhou")
	err := s.runAsPlatform(context.Background(), actor, cdomain.ActionTenantBackup,
		"backup-1", "backup", "tenant-1", nil,
		func(ctx context.Context) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	// Audit C5 JÁ foi gravado antes da fn (limitation; em prod
	// este é o "audit fantasma" que o Issue #148 visa eliminar).
	if got := len(audit.rows); got != 1 {
		t.Errorf("audit rows = %d (best-effort legacy), want 1", got)
	}
}

// TestRunAsPlatform_NoAuditNoPanic valida que sem audit repo, o
// fallback não panica — apenas roda a fn.
func TestRunAsPlatform_NoAuditNoPanic(t *testing.T) {
	s := &Service{log: zerolog.Nop(), audit: nil} // sem audit
	actor := cdomain.Actor{ID: "u1", Email: "admin@local"}

	fnCalled := false
	err := s.runAsPlatform(context.Background(), actor, cdomain.ActionTenantBackup,
		"backup-1", "backup", "tenant-1", nil,
		func(ctx context.Context) error { fnCalled = true; return nil })
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !fnCalled {
		t.Error("fn não foi chamada")
	}
}

// TestRecordAudit_NilAuditNoPanic valida que recordAudit é noop
// quando audit == nil (não é erro).
func TestRecordAudit_NilAuditNoPanic(t *testing.T) {
	s := &Service{log: zerolog.Nop(), audit: nil}
	actor := cdomain.Actor{ID: "u1", Email: "admin@local"}
	s.recordAudit(context.Background(), actor, cdomain.ActionTenantBackup, "b1", "t1", nil)
	// Sem panic = OK.
}
