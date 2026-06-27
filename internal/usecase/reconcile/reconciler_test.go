package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/rs/zerolog"
	"github.com/felipedsvit/mez-go-mono/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.VerifyTestMain(m)
}

type fakeRepo struct {
	mu      sync.Mutex
	pending []domain.Message
	marked  map[domain.MessageID]bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{marked: make(map[domain.MessageID]bool)}
}

func (f *fakeRepo) SelectUnroutedMessages(_ context.Context, batchSize int) ([]domain.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, nil
	}
	n := batchSize
	if n > len(f.pending) {
		n = len(f.pending)
	}
	batch := append([]domain.Message(nil), f.pending[:n]...)
	f.pending = f.pending[n:]
	return batch, nil
}

func (f *fakeRepo) MarkRouted(_ context.Context, id domain.MessageID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marked[id] = true
	return nil
}

func (f *fakeRepo) CountUnrouted(_ context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.pending), nil
}

func (f *fakeRepo) push(m domain.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, m)
}

func TestReconcileAll_DrainsAll(t *testing.T) {
	repo := newFakeRepo()
	repo.push(domain.Message{ID: "m1", TenantID: "t1"})
	repo.push(domain.Message{ID: "m2", TenantID: "t1"})
	repo.push(domain.Message{ID: "m3", TenantID: "t2"})

	var assigned []domain.MessageID
	var mu sync.Mutex
	assign := func(_ context.Context, m domain.Message) error {
		mu.Lock()
		defer mu.Unlock()
		assigned = append(assigned, m.ID)
		return nil
	}

	r := New(repo, assign, Config{Interval: time.Hour, BatchSize: 2}, zerolog.Nop())
	n, err := r.ReconcileAll(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if n != 3 {
		t.Errorf("processed = %d, want 3", n)
	}
	if len(assigned) != 3 {
		t.Errorf("assigned = %d, want 3", len(assigned))
	}
	if !repo.marked["m1"] || !repo.marked["m2"] || !repo.marked["m3"] {
		t.Error("not all marked routed")
	}
}

func TestReconcileAll_AssignErrorContinues(t *testing.T) {
	repo := newFakeRepo()
	repo.push(domain.Message{ID: "m1"})
	repo.push(domain.Message{ID: "m2"})

	assign := func(_ context.Context, m domain.Message) error {
		if m.ID == "m1" {
			return errors.New("simulated assign failure")
		}
		return nil
	}

	r := New(repo, assign, Config{Interval: time.Hour, BatchSize: 10}, zerolog.Nop())
	n, err := r.ReconcileAll(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// m1 falhou (não é contado), m2 sucesso.
	if n != 1 {
		t.Errorf("processed = %d, want 1 (m2 only)", n)
	}
	if repo.marked["m1"] {
		t.Error("m1 should not be marked routed after assign failure")
	}
	if !repo.marked["m2"] {
		t.Error("m2 should be marked routed")
	}
}

func TestReconciler_StopIsIdempotent(t *testing.T) {
	repo := newFakeRepo()
	r := New(repo, func(_ context.Context, _ domain.Message) error { return nil },
		Config{Interval: time.Millisecond}, zerolog.Nop())

	r.Stop()
	r.Stop() // não deve panic
}
