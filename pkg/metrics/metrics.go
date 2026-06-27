package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

type Registry struct {
	reg *prometheus.Registry

	BusPublishedTotal *prometheus.CounterVec
	BusDroppedTotal   *prometheus.CounterVec
	BusBufferDepth    *prometheus.GaugeVec
	OutboxPending     prometheus.Gauge
	ReconcilerLag     prometheus.Gauge

	// Lifecycle metrics (Fase 8 #101). nil-allowed: sink.go trata como
	// no-op quando não inicializados, o que permite testes que constroem
	// um Registry mínimo sem essas métricas.
	BootPhaseInfo         *prometheus.GaugeVec
	BootPhaseDuration     *prometheus.HistogramVec
	ShutdownPhaseDuration *prometheus.HistogramVec
	BootTotalDuration     prometheus.Histogram
	ShutdownTotalDuration prometheus.Histogram
}

func NewRegistry() *Registry {
	reg := prometheus.NewRegistry()

	r := &Registry{
		reg: reg,
		BusPublishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bus_published_total",
			Help: "Total number of bus events published",
		}, []string{"topic"}),
		BusDroppedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "bus_dropped_total",
			Help: "Total number of bus events dropped due to full buffer",
		}, []string{"topic"}),
		BusBufferDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "bus_buffer_depth",
			Help: "Current depth of bus buffers",
		}, []string{"topic"}),
		OutboxPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "outbox_pending",
			Help: "Number of pending outbox messages",
		}),
		ReconcilerLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "reconciler_lag",
			Help: "Reconciler processing lag in seconds",
		}),
	}

	reg.MustRegister(
		r.BusPublishedTotal,
		r.BusDroppedTotal,
		r.BusBufferDepth,
		r.OutboxPending,
		r.ReconcilerLag,
	)

	return r
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{})
}
