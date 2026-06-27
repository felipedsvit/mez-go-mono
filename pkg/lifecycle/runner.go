// Package lifecycle — runner.go: Runner (Fase 8 #96).
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

// ErrShutdownInProgress é retornado por Boot quando Shutdown já está em curso.
var ErrShutdownInProgress = errors.New("lifecycle: shutdown in progress")

// ErrBootFailed é retornado quando uma phase falha no boot.
var ErrBootFailed = errors.New("lifecycle: boot failed")

// Phase representa uma unidade de boot/shutdown com timeout próprio.
//
// Start é síncrono e idempotente. Retornar erro aborta o boot. nil = sem
// start (a phase existe apenas para Stop no shutdown).
//
// Stop é síncrono e idempotente. nil = sem stop. Erros são logados mas
// não interrompem o shutdown — a próxima phase ainda é tentada.
type Phase struct {
	Name    string
	Start   func(ctx context.Context) error
	Stop    func(ctx context.Context) error
	Timeout time.Duration // default 5s
}

// Order é a lista ordenada de phases executada por Boot (na ordem) e por
// Shutdown (em LIFO).
type Order []Phase

// defaultPhaseTimeout é o timeout aplicado a uma phase que não define Timeout.
const defaultPhaseTimeout = 5 * time.Second

// MetricsSink é a interface que o Runner usa para publicar métricas.
// A implementação canônica é *metrics.Registry, mas aceita um NOP para
// testes que não querem Prometheus.
type MetricsSink interface {
	SetBootPhase(phase string, value float64)
	ObserveBootPhase(phase string, seconds float64)
	ObserveShutdownPhase(phase string, seconds float64)
	ObserveBootTotal(seconds float64)
	ObserveShutdownTotal(seconds float64)
}

// noopMetrics é o sink padrão quando nenhum é passado.
type noopMetrics struct{}

func (noopMetrics) SetBootPhase(string, float64)                 {}
func (noopMetrics) ObserveBootPhase(string, float64)              {}
func (noopMetrics) ObserveShutdownPhase(string, float64)          {}
func (noopMetrics) ObserveBootTotal(float64)                      {}
func (noopMetrics) ObserveShutdownTotal(float64)                  {}

// Runner coordena boot/shutdown de phases.
type Runner struct {
	log     zerolog.Logger
	metrics MetricsSink
	phases  []Phase

	// Long-running goroutines iniciadas via Run.
	wg sync.WaitGroup

	// Estado interno.
	mu              sync.Mutex
	started         bool
	shutdown        bool
	shutdownStarted bool
	bootStarted     bool
	startedSet      map[string]bool // phases com Start já executado (para Shutdown parcial)

	// currentPhase é o nome da phase em andamento (boot ou shutdown).
	current atomic.Value // string
}

// NewRunner cria um Runner. m pode ser nil (usa NOP) ou um *metrics.Registry
// adaptado para MetricsSink (ver metrics.NewRunnerSink).
func NewRunner(log zerolog.Logger, m MetricsSink) *Runner {
	if m == nil {
		m = noopMetrics{}
	}
	return &Runner{
		log:        log.With().Str("component", "lifecycle.Runner").Logger(),
		metrics:    m,
		startedSet: make(map[string]bool),
	}
}

// AddPhase adiciona uma phase. Idempotente: a última vence (warn log).
// Deve ser chamada antes de Boot.
func (r *Runner) AddPhase(p Phase) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bootStarted || r.shutdownStarted {
		r.log.Warn().Str("phase", p.Name).Msg("lifecycle: AddPhase após boot/shutdown — ignorado")
		return
	}
	if p.Timeout <= 0 {
		p.Timeout = defaultPhaseTimeout
	}
	// Idempotência: se já existe phase com mesmo nome, substitui.
	for i, existing := range r.phases {
		if existing.Name == p.Name {
			r.log.Warn().Str("phase", p.Name).Msg("lifecycle: phase duplicada — substituindo")
			r.phases[i] = p
			return
		}
	}
	r.phases = append(r.phases, p)
}

// Phases retorna a lista de phases registradas (snapshot). Útil para testes.
func (r *Runner) Phases() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.phases))
	for _, p := range r.phases {
		out = append(out, p.Name)
	}
	return out
}

// Current retorna a fase em andamento (boot ou shutdown). "" se idle.
func (r *Runner) Current() string {
	if v := r.current.Load(); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Boot executa as phases em ordem. Falha em qualquer phase aborta e
// chama Shutdown parcial. Cada phase roda no seu próprio ctx com timeout.
func (r *Runner) Boot(ctx context.Context) error {
	r.mu.Lock()
	if r.shutdownStarted || r.shutdown {
		r.mu.Unlock()
		return ErrShutdownInProgress
	}
	if r.bootStarted {
		r.mu.Unlock()
		return errors.New("lifecycle: boot already called")
	}
	r.bootStarted = true
	r.mu.Unlock()

	startTotal := time.Now()
	r.log.Info().Int("phases", len(r.phases)).Msg("lifecycle: boot started")

	// Itera; se qualquer Start falhar, faz shutdown parcial.
	for i := range r.phases {
		p := r.phases[i]
		if p.Start == nil {
			continue
		}
		r.setCurrent(p.Name)
		r.metrics.SetBootPhase(p.Name, 1)

		// Marca como started ANTES de chamar Start — se Start falhar
		// ou panicar, ainda queremos que Shutdown chame o Stop.
		r.mu.Lock()
		r.startedSet[p.Name] = true
		r.mu.Unlock()

		phaseCtx, cancel := context.WithTimeout(ctx, p.Timeout)
		t := time.Now()
		err := func() (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("lifecycle: phase %q panicked: %v", p.Name, rec)
				}
			}()
			return p.Start(phaseCtx)
		}()
		cancel()
		dur := time.Since(t)

		r.metrics.ObserveBootPhase(p.Name, dur.Seconds())
		r.metrics.SetBootPhase(p.Name, 0)

		if err != nil {
			r.log.Error().
				Str("phase", p.Name).
				Dur("duration", dur).
				Err(err).
				Msg("lifecycle: boot phase failed")
			r.setCurrent("")
			// Shutdown parcial (phases já iniciadas em LIFO).
			r.mu.Lock()
			r.started = true // marca como started para Shutdown executar
			r.mu.Unlock()
			// shutdownCtx com timeout generoso para o shutdown parcial.
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = r.Shutdown(shutCtx)
			shutCancel()
			r.metrics.ObserveBootTotal(time.Since(startTotal).Seconds())
			return fmt.Errorf("%w: phase %q: %v", ErrBootFailed, p.Name, err)
		}
		r.log.Info().Str("phase", p.Name).Dur("duration", dur).Msg("lifecycle: boot phase ok")
	}

	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
	r.setCurrent("")
	r.metrics.ObserveBootTotal(time.Since(startTotal).Seconds())
	r.log.Info().Dur("total", time.Since(startTotal)).Msg("lifecycle: boot complete")
	return nil
}

// Shutdown executa as phases em ordem INVERSA. Cada phase com Stop
// recebe um ctx com timeout. Erros são logados mas não interrompem.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	if r.shutdownStarted {
		r.mu.Unlock()
		return nil // idempotente
	}
	r.shutdownStarted = true
	r.mu.Unlock()

	startTotal := time.Now()
	r.log.Info().Msg("lifecycle: shutdown started")

	for i := len(r.phases) - 1; i >= 0; i-- {
		p := r.phases[i]
		if p.Stop == nil {
			continue
		}
		// Shutdown parcial: só chama Stop em phases que subiram (Boot
		// chegou a executar o Start delas).
		r.mu.Lock()
		started := r.startedSet[p.Name]
		r.mu.Unlock()
		if !started {
			continue
		}
		r.setCurrent(p.Name)

		phaseCtx, cancel := context.WithTimeout(ctx, p.Timeout)
		t := time.Now()
		err := func() (err error) {
			defer func() {
				if rec := recover(); rec != nil {
					err = fmt.Errorf("lifecycle: phase %q stop panicked: %v", p.Name, rec)
				}
			}()
			return p.Stop(phaseCtx)
		}()
		cancel()
		dur := time.Since(t)

		r.metrics.ObserveShutdownPhase(p.Name, dur.Seconds())

		if err != nil {
			r.log.Error().
				Str("phase", p.Name).
				Dur("duration", dur).
				Err(err).
				Msg("lifecycle: shutdown phase error (continuando)")
		} else {
			r.log.Info().Str("phase", p.Name).Dur("duration", dur).Msg("lifecycle: shutdown phase ok")
		}
	}

	r.mu.Lock()
	r.shutdown = true
	r.mu.Unlock()
	r.setCurrent("")
	r.metrics.ObserveShutdownTotal(time.Since(startTotal).Seconds())
	r.log.Info().Dur("total", time.Since(startTotal)).Msg("lifecycle: shutdown complete")
	return nil
}

// Run inicia uma goroutine long-running (HTTP, relay, reconciler).
// Encapsula wg.Add(1) + defer wg.Done() + recover() (C10). Se fn
// retornar erro, ele é logado mas a wg.Done é chamada normalmente.
//
// Não bloqueia: retorna imediatamente após lançar a goroutine.
//
// Cancelamento: a goroutine termina quando ctx for cancelado (passado para fn).
// Wait() aguarda todas as goroutines Run-style terminarem.
func (r *Runner) Run(ctx context.Context, name string, fn func(context.Context) error) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				r.log.Error().
					Str("goroutine", name).
					Interface("panic", rec).
					Msg("lifecycle: goroutine panicked; recovered (C10)")
			}
		}()
		if err := fn(ctx); err != nil {
			r.log.Error().
				Str("goroutine", name).
				Err(err).
				Msg("lifecycle: goroutine returned error")
		}
	}()
}

// Wait bloqueia até todas as goroutines background terminarem.
// Caller chama Wait() DEPOIS de Shutdown() para drenar tudo.
//
// Retorna erro se ctx expirar antes de todas as goroutines terminarem.
func (r *Runner) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) setCurrent(name string) {
	r.current.Store(name)
}
