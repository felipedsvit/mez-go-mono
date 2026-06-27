package breaker

import (
	"errors"
	"testing"
)

func TestRegistry_GetOrCreate(t *testing.T) {
	r := NewRegistry(DefaultConfig())
	b1 := r.GetOrCreate("t1", "waba")
	b2 := r.GetOrCreate("t1", "waba")
	if b1 != b2 {
		t.Error("GetOrCreate deve retornar mesma instância para mesma key")
	}
	b3 := r.GetOrCreate("t1", "ig")
	if b1 == b3 {
		t.Error("GetOrCreate deve retornar instâncias diferentes para channels diferentes")
	}
	b4 := r.GetOrCreate("t2", "waba")
	if b1 == b4 {
		t.Error("GetOrCreate deve retornar instâncias diferentes para tenants diferentes")
	}
}

func TestRegistry_Execute_Closed(t *testing.T) {
	r := NewRegistry(DefaultConfig())
	err := r.Execute("t1", "waba", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
}

func TestRegistry_Execute_Opens_AfterFailures(t *testing.T) {
	r := NewRegistry(Config{
		MaxRequests:   3,
		Interval:      30 * 1e9, // 30s
		Timeout:       60 * 1e9, // 60s
		FailThreshold: 3,
	})
	myErr := errors.New("simulated failure")

	// 3 falhas devem abrir o breaker
	for i := 0; i < 3; i++ {
		_ = r.Execute("t1", "waba", func() error { return myErr })
	}

	// 4ª tentativa deve ser rejeitada (ErrOpenState ou ErrTooManyRequests)
	err := r.Execute("t1", "waba", func() error {
		t.Error("fn não deveria ser chamado com breaker aberto")
		return nil
	})
	if err == nil {
		t.Fatal("expected error com breaker aberto, got nil")
	}
}

func TestRegistry_Execute_SuccessResets(t *testing.T) {
	r := NewRegistry(Config{
		MaxRequests:   3,
		Interval:      30 * 1e9,
		Timeout:       60 * 1e9,
		FailThreshold: 10, // alto para não abrir durante este test
	})

	// 2 falhas
	myErr := errors.New("fail")
	_ = r.Execute("t1", "waba", func() error { return myErr })
	_ = r.Execute("t1", "waba", func() error { return myErr })

	// 1 sucesso deve resetar contagem
	err := r.Execute("t1", "waba", func() error { return nil })
	if err != nil {
		t.Errorf("expected success após recovery, got %v", err)
	}
}

func TestRegistry_Snapshot(t *testing.T) {
	r := NewRegistry(DefaultConfig())
	r.GetOrCreate("t1", "waba")
	r.GetOrCreate("t1", "ig")
	r.GetOrCreate("t2", "waba")

	snap := r.Snapshot()
	if len(snap) != 3 {
		t.Errorf("snapshot esperado 3 entries, got %d: %v", len(snap), snap)
	}
	if snap["t1/waba"] != "closed" {
		t.Errorf("estado inicial esperado 'closed', got %q", snap["t1/waba"])
	}
}

func TestRegistry_PerTenantIsolation(t *testing.T) {
	r := NewRegistry(Config{
		MaxRequests:   3,
		Interval:      30 * 1e9,
		Timeout:       60 * 1e9,
		FailThreshold: 3,
	})
	myErr := errors.New("fail")

	// tenant A abre seu breaker
	for i := 0; i < 3; i++ {
		_ = r.Execute("tenantA", "waba", func() error { return myErr })
	}
	errA := r.Execute("tenantA", "waba", func() error { return nil })

	// tenant B ainda funciona (breaker isolado)
	errB := r.Execute("tenantB", "waba", func() error { return nil })

	if errA == nil {
		t.Error("tenantA deveria estar bloqueado")
	}
	if errB != nil {
		t.Errorf("tenantB deveria passar, got %v", errB)
	}
}
