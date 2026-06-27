package metrics

// RunnerSink adapta um *Registry para a interface lifecycle.MetricsSink.
// Evita dependência circular (lifecycle não importa metrics diretamente
// para não criar ciclos) e mantém o contrato testável.
type RunnerSink struct {
	r *Registry
}

// NewRunnerSink cria o adapter.
func NewRunnerSink(r *Registry) *RunnerSink {
	return &RunnerSink{r: r}
}

// SetBootPhase define o valor do gauge de fase atual.
func (s *RunnerSink) SetBootPhase(phase string, value float64) {
	if s == nil || s.r == nil || s.r.BootPhaseInfo == nil {
		return
	}
	s.r.BootPhaseInfo.WithLabelValues(phase).Set(value)
}

// ObserveBootPhase observa a duração de uma phase de boot.
func (s *RunnerSink) ObserveBootPhase(phase string, seconds float64) {
	if s == nil || s.r == nil || s.r.BootPhaseDuration == nil {
		return
	}
	s.r.BootPhaseDuration.WithLabelValues(phase).Observe(seconds)
}

// ObserveShutdownPhase observa a duração de uma phase de shutdown.
func (s *RunnerSink) ObserveShutdownPhase(phase string, seconds float64) {
	if s == nil || s.r == nil || s.r.ShutdownPhaseDuration == nil {
		return
	}
	s.r.ShutdownPhaseDuration.WithLabelValues(phase).Observe(seconds)
}

// ObserveBootTotal observa a duração total do boot.
func (s *RunnerSink) ObserveBootTotal(seconds float64) {
	if s == nil || s.r == nil || s.r.BootTotalDuration == nil {
		return
	}
	s.r.BootTotalDuration.Observe(seconds)
}

// ObserveShutdownTotal observa a duração total do shutdown.
func (s *RunnerSink) ObserveShutdownTotal(seconds float64) {
	if s == nil || s.r == nil || s.r.ShutdownTotalDuration == nil {
		return
	}
	s.r.ShutdownTotalDuration.Observe(seconds)
}
