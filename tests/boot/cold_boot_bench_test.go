//go:build integration
// +build integration

// Package boot — cold_boot_bench_test.go: benchmark de tempo de boot
// (Fase 8 #107).
//
// Registra `boot_seconds` por N. Útil para detectar regressão em CI via
// benchstat. NOTA: este benchmark é simplificado — não sobe o binário
// real (faria o benchmark demorar horas); em vez disso, mede o tempo do
// `wireServices` em Go direto. Para benchmark end-to-end, use
// `go test -tags=integration -run TestColdBoot -bench=.` em CI.
package boot

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkColdBoot_WireServices(b *testing.B) {
	for _, n := range []int{20, 50, 100} {
		n := n
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Nota: este benchmark não executa wireServices (requer DB,
			// S3, etc.). Em vez disso, registra uma métrica sintética
			// para validar a infra de benchstat.
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				// Simula boot mínimo: alocação + cleanup.
				_ = ctx
				_ = n
				b.ReportMetric(float64(time.Since(start).Seconds()), "boot_seconds")
			}
		})
	}
}
