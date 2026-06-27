// Package lifecycle — runner_test.go: cobertura do Runner.
package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/goleak"
)

// TestMain (Fase 8 #102): goleak global — falha se algum teste deixar
// goroutine em leak.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// recordingMetrics é um MetricsSink fake que registra as chamadas para
// asserções nos testes.
type recordingMetrics struct {
	bootPhase       map[string]float64
	bootDurations   map[string]float64
	shutdownDurs    map[string]float64
	bootTotal       []float64
	shutdownTotal   []float64
	mu              chan struct{} // serializa
}

func newRecordingMetrics() *recordingMetrics {
	return &recordingMetrics{
		bootPhase:     make(map[string]float64),
		bootDurations: make(map[string]float64),
		shutdownDurs:  make(map[string]float64),
		mu:            make(chan struct{}, 1),
	}
}

func (r *recordingMetrics) SetBootPhase(phase string, value float64) {
	r.mu <- struct{}{}
	r.bootPhase[phase] = value
	<-r.mu
}
func (r *recordingMetrics) ObserveBootPhase(phase string, seconds float64) {
	r.mu <- struct{}{}
	r.bootDurations[phase] = seconds
	<-r.mu
}
func (r *recordingMetrics) ObserveShutdownPhase(phase string, seconds float64) {
	r.mu <- struct{}{}
	r.shutdownDurs[phase] = seconds
	<-r.mu
}
func (r *recordingMetrics) ObserveBootTotal(seconds float64) {
	r.mu <- struct{}{}
	r.bootTotal = append(r.bootTotal, seconds)
	<-r.mu
}
func (r *recordingMetrics) ObserveShutdownTotal(seconds float64) {
	r.mu <- struct{}{}
	r.shutdownTotal = append(r.shutdownTotal, seconds)
	<-r.mu
}

func TestRunner_BootOrder(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	var order []string
	r.AddPhase(Phase{Name: "a", Start: func(context.Context) error { order = append(order, "a"); return nil }})
	r.AddPhase(Phase{Name: "b", Start: func(context.Context) error { order = append(order, "b"); return nil }})
	r.AddPhase(Phase{Name: "c", Start: func(context.Context) error { order = append(order, "c"); return nil }})

	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if len(order) != 3 || order[0] != "a" || order[1] != "b" || order[2] != "c" {
		t.Errorf("ordem do boot = %v, want [a b c]", order)
	}
}

func TestRunner_ShutdownLIFO(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	var order []string
	r.AddPhase(Phase{Name: "a", Start: func(context.Context) error { return nil }, Stop: func(context.Context) error { order = append(order, "a"); return nil }})
	r.AddPhase(Phase{Name: "b", Start: func(context.Context) error { return nil }, Stop: func(context.Context) error { order = append(order, "b"); return nil }})
	r.AddPhase(Phase{Name: "c", Start: func(context.Context) error { return nil }, Stop: func(context.Context) error { order = append(order, "c"); return nil }})

	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if len(order) != 3 || order[0] != "c" || order[1] != "b" || order[2] != "a" {
		t.Errorf("ordem do shutdown = %v, want [c b a] (LIFO)", order)
	}
}

func TestRunner_Shutdown_Idempotente(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	var calls atomic.Int32
	r.AddPhase(Phase{
		Name:  "x",
		Start: func(context.Context) error { return nil },
		Stop:  func(context.Context) error { calls.Add(1); return nil },
	})
	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	_ = r.Shutdown(context.Background())
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatalf("segundo shutdown: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("Stop chamado %d vezes, want 1 (idempotente)", calls.Load())
	}
}

func TestRunner_PanicEmStart_NaoDerruba(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	r.AddPhase(Phase{
		Name:  "boom",
		Start: func(context.Context) error { panic("kaboom") },
		Stop:  func(context.Context) error { return nil },
	})
	r.AddPhase(Phase{
		Name:  "after",
		Start: func(context.Context) error { return nil },
		Stop:  func(context.Context) error { return nil },
	})
	err := r.Boot(context.Background())
	if err == nil {
		t.Fatal("boot deveria ter falhado")
	}
	if !errors.Is(err, ErrBootFailed) {
		t.Errorf("err = %v, want ErrBootFailed", err)
	}
}

func TestRunner_TimeoutPorPhase(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	r.AddPhase(Phase{
		Name:    "slow",
		Start:   func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		Timeout: 50 * time.Millisecond,
	})
	t1 := time.Now()
	err := r.Boot(context.Background())
	dur := time.Since(t1)
	if err == nil {
		t.Fatal("boot deveria falhar com timeout")
	}
	if dur > 500*time.Millisecond {
		t.Errorf("boot levou %v, esperado <500ms (timeout 50ms)", dur)
	}
	if !errors.Is(err, ErrBootFailed) {
		t.Errorf("err = %v, want ErrBootFailed", err)
	}
}

func TestRunner_Run_RecoverPanic(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	called := atomic.Int32{}
	r.Run(context.Background(), "panic-fn", func(context.Context) error {
		called.Add(1)
		panic("boom")
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if called.Load() != 1 {
		t.Errorf("fn chamada %d vezes, want 1", called.Load())
	}
}

func TestRunner_Run_LogaErroSemMatar(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	want := errors.New("erro de teste")
	r.Run(context.Background(), "err-fn", func(context.Context) error { return want })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

func TestRunner_BootFalha_FazShutdownParcial(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	var stopped []string
	r.AddPhase(Phase{
		Name:  "a",
		Start: func(context.Context) error { return nil },
		Stop:  func(context.Context) error { stopped = append(stopped, "a"); return nil },
	})
	r.AddPhase(Phase{
		Name:  "b",
		Start: func(context.Context) error { return errors.New("b falhou") },
		Stop:  func(context.Context) error { stopped = append(stopped, "b"); return nil },
	})
	r.AddPhase(Phase{
		Name:  "c",
		Start: func(context.Context) error { return nil },
		Stop:  func(context.Context) error { stopped = append(stopped, "c"); return nil },
	})
	if err := r.Boot(context.Background()); err == nil {
		t.Fatal("boot deveria falhar")
	}
	// LIFO: b para, depois a. c nunca subiu, então não é stopped.
	if len(stopped) != 2 || stopped[0] != "b" || stopped[1] != "a" {
		t.Errorf("shutdown parcial = %v, want [b a]", stopped)
	}
}

func TestRunner_Current(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	started := make(chan struct{})
	release := make(chan struct{})
	r.AddPhase(Phase{
		Name: "blocking",
		Start: func(ctx context.Context) error {
			close(started)
			<-release
			return nil
		},
		Timeout: 5 * time.Second,
	})
	bootDone := make(chan error, 1)
	go func() { bootDone <- r.Boot(context.Background()) }()
	<-started
	if cur := r.Current(); cur != "blocking" {
		t.Errorf("Current = %q, want blocking", cur)
	}
	close(release)
	if err := <-bootDone; err != nil {
		t.Errorf("boot: %v", err)
	}
	if cur := r.Current(); cur != "" {
		t.Errorf("Current após boot = %q, want \"\"", cur)
	}
}

func TestRunner_ShutdownAposShutdown(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	r.AddPhase(Phase{Name: "a", Start: func(context.Context) error { return nil }})
	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if err := r.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown 1: %v", err)
	}
	// Boot após shutdown deve falhar com ErrShutdownInProgress.
	if err := r.Boot(context.Background()); !errors.Is(err, ErrShutdownInProgress) {
		t.Errorf("boot após shutdown: err = %v, want ErrShutdownInProgress", err)
	}
}

func TestRunner_BootJaBoot(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	r.AddPhase(Phase{Name: "a", Start: func(context.Context) error { return nil }})
	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot 1: %v", err)
	}
	// Segundo Boot sem Shutdown deve falhar.
	err := r.Boot(context.Background())
	if err == nil {
		t.Fatal("segundo boot deveria falhar")
	}
}

func TestRunner_AddPhaseDuplicada_Substitui(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	var calls atomic.Int32
	r.AddPhase(Phase{Name: "dup", Start: func(context.Context) error { calls.Add(1); return nil }})
	r.AddPhase(Phase{Name: "dup", Start: func(context.Context) error { calls.Add(1); return nil }})
	if err := r.Boot(context.Background()); err != nil {
		t.Fatalf("boot: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("Start chamado %d vezes, want 1 (substituição)", calls.Load())
	}
}

func TestRunner_Phases(t *testing.T) {
	r := NewRunner(zerolog.Nop(), newRecordingMetrics())
	r.AddPhase(Phase{Name: "a", Start: func(context.Context) error { return nil }})
	r.AddPhase(Phase{Name: "b", Start: func(context.Context) error { return nil }})
	got := r.Phases()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Phases = %v, want [a b]", got)
	}
}
