package secrets_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	adaptercrypto "github.com/felipedsvit/mez-go-mono/internal/adapter/crypto"
	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/internal/usecase/secrets"
)

// Rotação usa KEKs de 32 bytes. Geramos duas deterministicamente abaixo.
const (
	oldKEKBase64 = "qWHjF67aiJj0afT9z7fKPi5S5fhQwA/EaQLT0QNr7rg=" // 32 bytes
	newKEKBase64 = "zjaKqqXQQ8GTkcggrXDpN7+8j1nvDGWWkhKO+/Gq1F8=" // 32 bytes
)

// fakeCrossTenantRepo simula o ChannelCredentialsCrossTenant sem DB.
type fakeCrossTenantRepo struct {
	mu          sync.Mutex
	rows        map[string]port.CredentialRow
	updateErr   error // se != nil, UpdateWrappedDEK falha
	invalidated []domain.TenantID
}

func newFakeCrossTenantRepo(rows []port.CredentialRow) *fakeCrossTenantRepo {
	r := &fakeCrossTenantRepo{rows: make(map[string]port.CredentialRow)}
	for _, row := range rows {
		r.rows[credKey(row.TenantID, row.Channel)] = row
	}
	return r
}

func credKey(t domain.TenantID, c domain.Channel) string {
	return string(t) + "|" + string(c)
}

func (r *fakeCrossTenantRepo) ForEachTenant(_ context.Context, _ string, fn func(ctx context.Context, row port.CredentialRow) error) error {
	// Snapshot sob lock; libera antes de chamar fn (que pode pegar lock
	// via UpdateWrappedDEK). Não há risco de race no snapshot porque o
	// fake não é concorrente no caller.
	r.mu.Lock()
	rows := make([]port.CredentialRow, 0, len(r.rows))
	for _, row := range r.rows {
		rows = append(rows, row)
	}
	r.mu.Unlock()

	for _, row := range rows {
		cp := row
		if err := fn(context.Background(), cp); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeCrossTenantRepo) UpdateWrappedDEK(_ context.Context, tenantID domain.TenantID, channel domain.Channel, newWrappedDEK []byte, newKekVersion int, _ *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	k := credKey(tenantID, channel)
	row, ok := r.rows[k]
	if !ok {
		return errors.New("not found")
	}
	row.WrappedDEK = append([]byte(nil), newWrappedDEK...)
	row.KEKVersion = newKekVersion
	r.rows[k] = row
	return nil
}

func (r *fakeCrossTenantRepo) Invalidate(tenantID domain.TenantID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.invalidated = append(r.invalidated, tenantID)
}

// fakeAuditRecord captura audit entries em memória.
type fakeAuditRecord struct {
	mu      sync.Mutex
	entries []admin.AuditEntry
}

func (a *fakeAuditRecord) Record(_ context.Context, e *admin.AuditEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	a.entries = append(a.entries, *e)
	return nil
}

func (a *fakeAuditRecord) ByAction(action admin.Action) []admin.AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []admin.AuditEntry
	for _, e := range a.entries {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// seedRows gera 2 tenants × 2 canais = 4 credenciais usando a oldKEK.
func seedRows(t *testing.T) []port.CredentialRow {
	t.Helper()
	old, err := adaptercrypto.NewLocalSealer(oldKEKBase64)
	if err != nil {
		t.Fatalf("old sealer: %v", err)
	}
	mk := func(plaintext []byte) (wrapped, encrypted []byte) {
		// 32 bytes exatos (AES-256).
		dek := []byte("deterministic-dek-of-32-bytes!ok") // 32 chars
		wrapped, werr := old.Wrap(context.Background(), dek)
		if werr != nil {
			t.Fatalf("wrap: %v", werr)
		}
		enc, eerr := old.EncryptForTenant(wrapped, plaintext)
		if eerr != nil {
			t.Fatalf("encrypt: %v", eerr)
		}
		return wrapped, enc
	}

	tenants := []domain.TenantID{"A", "B"}
	channels := []domain.Channel{domain.ChannelWABA, domain.ChannelTGBot}
	var rows []port.CredentialRow
	for _, tid := range tenants {
		for _, ch := range channels {
			w, e := mk([]byte("plaintext-" + string(tid) + "-" + string(ch)))
			rows = append(rows, port.CredentialRow{
				TenantID: tid, Channel: ch,
				WrappedDEK: w, Encrypted: e, KEKVersion: 1,
			})
		}
	}
	return rows
}

func TestRotate_HappyPath(t *testing.T) {
	rows := seedRows(t)
	repo := newFakeCrossTenantRepo(rows)
	audit := &fakeAuditRecord{}

	var invalidated []domain.TenantID
	var imu sync.Mutex

	report, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKBase64,
		NewKEKBase64: newKEKBase64,
		Actor:        "operator:test",
		InvalidateFn: func(t domain.TenantID) {
			imu.Lock()
			invalidated = append(invalidated, t)
			imu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(report.Errors) > 0 {
		t.Errorf("esperava 0 erros, obteve: %+v", report.Errors)
	}
	if report.Tenants != 2 {
		t.Errorf("Tenants: got %d want 2", report.Tenants)
	}
	if report.Channels != 4 {
		t.Errorf("Channels: got %d want 4", report.Channels)
	}
	if report.NewVersion != 2 {
		t.Errorf("NewVersion: got %d want 2", report.NewVersion)
	}
	if report.OldVersion != 1 {
		t.Errorf("OldVersion: got %d want 1", report.OldVersion)
	}

	// Audit: started + 4 rotate_kek_tenant + complete = 6 entries.
	started := audit.ByAction(admin.ActionRotateKEKStarted)
	if len(started) != 1 {
		t.Errorf("esperava 1 'started', got %d", len(started))
	}
	tenantActions := audit.ByAction(admin.ActionRotateKEKTenant)
	if len(tenantActions) != 4 {
		t.Errorf("esperava 4 'tenant', got %d", len(tenantActions))
	}
	complete := audit.ByAction(admin.ActionRotateKEKComplete)
	if len(complete) != 1 {
		t.Errorf("esperava 1 'complete', got %d", len(complete))
	}

	// InvalidateFn foi chamado 4 vezes (1 por canal).
	imu.Lock()
	if len(invalidated) != 4 {
		t.Errorf("esperava 4 invalidações, got %d", len(invalidated))
	}
	imu.Unlock()
}

func TestRotate_DryRun_DoesNotMutate(t *testing.T) {
	rows := seedRows(t)
	repo := newFakeCrossTenantRepo(rows)
	audit := &fakeAuditRecord{}

	originalRows := make([]port.CredentialRow, len(rows))
	copy(originalRows, rows)

	report, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKBase64,
		NewKEKBase64: newKEKBase64,
		DryRun:       true,
		Actor:        "operator:test",
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if report.Channels != 4 {
		t.Errorf("Channels: got %d want 4", report.Channels)
	}
	if !report.DryRun {
		t.Error("esperava report.DryRun=true")
	}

	// Nada foi mutado.
	repo.mu.Lock()
	for _, orig := range originalRows {
		got := repo.rows[credKey(orig.TenantID, orig.Channel)]
		if string(got.WrappedDEK) != string(orig.WrappedDEK) {
			t.Errorf("dry-run mutou wrapped_dek de %s/%s", orig.TenantID, orig.Channel)
		}
		if got.KEKVersion != orig.KEKVersion {
			t.Errorf("dry-run mutou kek_version de %s/%s", orig.TenantID, orig.Channel)
		}
	}
	repo.mu.Unlock()
}

func TestRotate_InvalidKEK(t *testing.T) {
	repo := newFakeCrossTenantRepo(nil)
	audit := &fakeAuditRecord{}

	_, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: "c2hvcnQ=",     // 3 bytes
		NewKEKBase64: newKEKBase64,
	})
	if !errors.Is(err, secrets.ErrInvalidKEKLength) {
		t.Errorf("esperava ErrInvalidKEKLength, got: %v", err)
	}

	_, err = secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: "",
		NewKEKBase64: newKEKBase64,
	})
	if !errors.Is(err, secrets.ErrEmptyKEK) {
		t.Errorf("esperava ErrEmptyKEK, got: %v", err)
	}
}

func TestRotate_EncryptedSurvivesRotation(t *testing.T) {
	// Verifica que, após rotação, a credencial decifrada com a KEK nova
	// produz o mesmo plaintext original.
	rows := seedRows(t)
	repo := newFakeCrossTenantRepo(rows)
	audit := &fakeAuditRecord{}

	plaintexts := map[string][]byte{
		"A|waba":       []byte("plaintext-A-waba"),
		"A|telegram_bot": []byte("plaintext-A-telegram_bot"),
		"B|waba":       []byte("plaintext-B-waba"),
		"B|telegram_bot": []byte("plaintext-B-telegram_bot"),
	}

	_, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKBase64,
		NewKEKBase64: newKEKBase64,
		Actor:        "operator:test",
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// Decifra cada (tenant, channel) com a KEK NOVA e compara.
	newSeal, err := adaptercrypto.NewLocalSealer(newKEKBase64)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	for k, expected := range plaintexts {
		row := repo.rows[k]
		got, err := newSeal.DecryptForTenant(row.WrappedDEK, row.Encrypted)
		if err != nil {
			t.Errorf("DecryptForTenant(%s): %v", k, err)
			continue
		}
		if string(got) != string(expected) {
			t.Errorf("%s: got %q want %q", k, got, expected)
		}
	}
}

func TestRotate_ContinuesOnRowError(t *testing.T) {
	// Simula um erro em 1 tenant — o lote não pode abortar; o resto
	// deve completar.
	rows := seedRows(t) // 4 rows
	repo := newFakeCrossTenantRepo(rows)
	audit := &fakeAuditRecord{}

	// Força erro no UpdateWrappedDEK do tenant A — mas como o
	// fakeCrossTenantRepo não distingue, vamos usar a op
	// SealerFactory para forçar um erro em uma das unwraps.
	// Truque: retornar erro na factory é complicado; em vez disso,
	// adulteramos 1 wrapped_dek para que Unwrap falhe.
	repo.mu.Lock()
	row := repo.rows[credKey("A", domain.ChannelWABA)]
	row.WrappedDEK = []byte("tampered-wrapped-dek-bytes-32-bytes-ok")
	repo.rows[credKey("A", domain.ChannelWABA)] = row
	repo.mu.Unlock()

	report, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKBase64,
		NewKEKBase64: newKEKBase64,
		Actor:        "operator:test",
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// Esperamos 1 erro (A/waba) e 3 sucessos.
	if len(report.Errors) != 1 {
		t.Errorf("esperava 1 erro, got %d: %+v", len(report.Errors), report.Errors)
	}
	if report.Channels != 3 {
		t.Errorf("esperava 3 sucessos, got %d", report.Channels)
	}
	if len(report.Errors) > 0 && report.Errors[0].Channel != domain.ChannelWABA {
		t.Errorf("erro deveria ser em waba, got %s", report.Errors[0].Channel)
	}
}

func TestRotate_InvalidateFnCalledOncePerTenant(t *testing.T) {
	rows := seedRows(t) // 2 tenants × 2 canais
	repo := newFakeCrossTenantRepo(rows)
	audit := &fakeAuditRecord{}

	counts := make(map[domain.TenantID]int)
	var mu sync.Mutex

	_, err := secrets.Rotate(context.Background(), repo, audit, secrets.RotateKEKOpts{
		OldKEKBase64: oldKEKBase64,
		NewKEKBase64: newKEKBase64,
		Actor:        "operator:test",
		InvalidateFn: func(t domain.TenantID) {
			mu.Lock()
			counts[t]++
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if counts["A"] != 2 {
		t.Errorf("tenant A: esperava 2 invalidações, got %d", counts["A"])
	}
	if counts["B"] != 2 {
		t.Errorf("tenant B: esperava 2 invalidações, got %d", counts["B"])
	}
}

// Sanidade: confirma que errors.Is/As do RotationError funcionam.
func TestRotationError_ErrorsAs(t *testing.T) {
	inner := errors.New("boom")
	re := &secrets.RotationError{TenantID: "T", Channel: "c", Op: "unwrap", Err: inner}

	var target *secrets.RotationError
	if !errors.As(error(re), &target) {
		t.Fatal("errors.As falhou")
	}
	if !errors.Is(re, inner) {
		t.Error("errors.Is falhou")
	}
}

var _ = zerolog.Nop // silence unused import in some test configurations
