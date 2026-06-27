// Package metrics — testes do Registry e RunnerSink.
//
// Cobre:
//   - NewRegistry: constrói registry com 4 métricas básicas registradas;
//   - NewRegistry: Handler() retorna http.Handler funcional;
//   - RunnerSink: opera como no-op quando métricas lifecycle são nil
//     (modo "registry mínimo" usado em testes);
//   - RunnerSink: seta gauges/histograms quando inicializados.
package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRegistry_RegistersCoreMetrics(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	if r.BusPublishedTotal == nil {
		t.Error("BusPublishedTotal should be initialized")
	}
	if r.BusDroppedTotal == nil {
		t.Error("BusDroppedTotal should be initialized")
	}
	if r.BusBufferDepth == nil {
		t.Error("BusBufferDepth should be initialized")
	}
	if r.OutboxPending == nil {
		t.Error("OutboxPending should be initialized")
	}
	if r.ReconcilerLag == nil {
		t.Error("ReconcilerLag should be initialized")
	}

	// Lifecycle metrics: nil por default; populados separadamente.
	if r.BootPhaseInfo != nil {
		t.Error("BootPhaseInfo should be nil in minimal registry")
	}
	if r.BootTotalDuration != nil {
		t.Error("BootTotalDuration should be nil in minimal registry")
	}
}

func TestNewRegistry_HandlerExposesMetrics(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	// Gera uma observação para garantir que o output tem ao menos 1 linha.
	r.BusPublishedTotal.WithLabelValues("inbound").Inc()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "bus_published_total") {
		t.Errorf("metrics body missing bus_published_total:\n%s", body)
	}
	if !strings.Contains(body, `topic="inbound"`) {
		t.Errorf("metrics body missing inbound label:\n%s", body)
	}
}

func TestRunnerSink_NoOpWhenMetricsNil(t *testing.T) {
	t.Parallel()

	r := NewRegistry() // metrics lifecycle ficam nil
	sink := NewRunnerSink(r)
	if sink == nil {
		t.Fatal("NewRunnerSink returned nil")
	}

	// Nenhum panic em qualquer método, mesmo com metrics nil.
	sink.SetBootPhase("init", 1)
	sink.ObserveBootPhase("init", 0.5)
	sink.ObserveShutdownPhase("stop", 0.1)
	sink.ObserveBootTotal(1.5)
	sink.ObserveShutdownTotal(0.7)

	// nil sink também não panica.
	var nilSink *RunnerSink
	nilSink.SetBootPhase("x", 1)
	nilSink.ObserveBootPhase("x", 0.1)
	nilSink.ObserveShutdownPhase("x", 0.1)
	nilSink.ObserveBootTotal(0.1)
	nilSink.ObserveShutdownTotal(0.1)
}

func TestRunnerSink_NilInnerRegistry_NoPanic(t *testing.T) {
	t.Parallel()

	// Sink com r == nil (degenerate): todos os métodos devem ser no-op seguros.
	s := &RunnerSink{r: nil}
	s.SetBootPhase("init", 1)
	s.ObserveBootPhase("init", 0.1)
	s.ObserveShutdownPhase("init", 0.1)
	s.ObserveBootTotal(0.1)
	s.ObserveShutdownTotal(0.1)
}
