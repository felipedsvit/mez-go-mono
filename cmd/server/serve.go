// Package main — serve.go: entrypoint do subcomando `serve` (Fase 8 #97).
//
// Refatorado para usar pkg/lifecycle.Runner. Boot determinístico por fases
// + graceful shutdown coordenado em LIFO. MigrateOnBoot opcional (#101).
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/pkg/config"
	"github.com/felipedsvit/mez-go-mono/pkg/lifecycle"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

// runServe inicia o pipeline inbound completo (Fase 2+).
// Boot determinístico (via lifecycle.Runner) e graceful shutdown coordenado.
func runServe(cfg config.Config, log zerolog.Logger) {
	if err := cfg.ValidateServe(); err != nil {
		log.Fatal().Err(err).Msg("config validation")
	}

	// MigrateOnBoot (Fase 8 #101): roda migrations antes de subir o
	// processo. Fail-closed nativo. Idempotente.
	if cfg.MigrateOnServe {
		log.Info().Msg("serve: migrate on boot (MigrateOnServe=true)")
		if err := runMigrateInline(cfg, log); err != nil {
			log.Fatal().Err(err).Msg("migrate on boot failed; container will not start (fail-closed)")
		}
		log.Info().Msg("serve: migrate on boot complete")
	}

	// Audit log de boot_migration (D17). Best-effort.
	if cfg.MigrateOnServe {
		log.Info().Str("audit", "boot_migration").Str("details", "migrations applied at boot").Msg("audit: boot event")
	}

	metricsReg := metrics.NewRegistry()
	runner := lifecycle.NewRunner(log, metrics.NewRunnerSink(metricsReg))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app, err := wireApp(ctx, cfg, log, runner)
	if err != nil {
		log.Fatal().Err(err).Msg("wire app")
	}

	log.Info().
		Str("addr", app.Cfg.HTTPAddr).
		Bool("s3", app.Store != nil).
		Msg("serve: starting")

	if err := runner.Boot(ctx); err != nil {
		log.Fatal().Err(err).Msg("boot")
	}

	runWithGracefulShutdown(ctx, app, runner, log)
}

// wireApp monta o AppContext e registra as phases no runner.
//
// Refatoração Fase 8 #97: o wireServices original monta toda a árvore
// de dependências. Aqui adicionamos as phases com Start/Stop explícitos
// para os subsistemas que têm ciclo de vida real.
func wireApp(ctx context.Context, cfg config.Config, log zerolog.Logger, runner *lifecycle.Runner) (*AppContext, error) {
	// Monta a árvore de dependências (igual ao wireServices original).
	app, err := wireServices(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	// --- Phase: relay ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseRelay,
		Start: func(ctx context.Context) error {
			runner.Run(ctx, "relay", app.Relay.Run)
			return nil
		},
		Stop: func(ctx context.Context) error {
			app.Relay.Stop()
			return nil
		},
	})

	// --- Phase: reconciler ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseReconciler,
		Start: func(ctx context.Context) error {
			runner.Run(ctx, "reconciler", app.Reconciler.Run)
			return nil
		},
		Stop: func(ctx context.Context) error {
			app.Reconciler.Stop()
			return nil
		},
	})

	// --- Phase: status_consumer ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseStatusConsumer,
		Start: func(ctx context.Context) error {
			app.StatusConsumer.Subscribe()
			return nil
		},
		Stop: func(ctx context.Context) error {
			app.StatusConsumer.Unsubscribe()
			return nil
		},
	})

	// --- Phase: whatsmeow (D10: Manager.DisconnectAll no shutdown) ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseWhatsmeow,
		Start: func(ctx context.Context) error {
			// Manager já foi criado em wireServices. Nada para fazer
			// no Start — clients são lazy via GetOrCreate.
			return nil
		},
		Stop: func(ctx context.Context) error {
			// Fecha todos os clients whatsmeow (D10).
			app.WAManager().DisconnectAll()
			return nil
		},
		Timeout: 30 * time.Second, // Disconnect por tenant pode demorar.
	})

	// --- Phase: bus (Drain no shutdown) ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseBus,
		Start: func(ctx context.Context) error {
			// Bus.NewBus já inicia os consumers internamente (goroutines
			// de drain). Nada a fazer aqui.
			return nil
		},
		Stop: func(ctx context.Context) error {
			// Drena os buffers antes de fechar. Bloqueia até ctx expirar
			// ou todas as goroutines terminarem.
			return app.Bus.Drain(ctx)
		},
		Timeout: 10 * time.Second,
	})

	// --- Phase: http (long-running; ListenAndServe via runner.Run) ---
	runner.AddPhase(lifecycle.Phase{
		Name: lifecycle.PhaseHTTP,
		Start: func(ctx context.Context) error {
			runner.Run(ctx, "http", func(ctx context.Context) error {
				app.Log.Info().Str("addr", app.Cfg.HTTPAddr).Msg("http: listening")
				if err := app.HTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					return err
				}
				return nil
			})
			return nil
		},
		Stop: func(ctx context.Context) error {
			return app.HTTPServer.Shutdown(ctx)
		},
		Timeout: 30 * time.Second,
	})

	return app, nil
}

// runWithGracefulShutdown coordena shutdown: signal → runner.Shutdown →
// runner.Wait → pool close (Fase 8 #97).
func runWithGracefulShutdown(ctx context.Context, app *AppContext, runner *lifecycle.Runner, log zerolog.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("shutdown: signal received")
	case <-ctx.Done():
		log.Warn().Msg("shutdown: ctx cancelled")
	}

	// 30s totais para shutdown completo.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Ordem inversa: HTTP → bus → whatsmeow → status → reconciler → relay
	// → ... Implementado pelo runner.
	if err := runner.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("shutdown: runner error")
	}

	// Espera goroutines long-running (HTTP, relay, reconciler) terminarem.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if err := runner.Wait(waitCtx); err != nil {
		log.Warn().Err(err).Msg("shutdown: wait timeout (5s)")
	}

	// Pools por último (depois que nada mais precisa deles).
	if app.appPool != nil {
		app.appPool.Close()
	}
	if app.platformPool != nil {
		app.platformPool.Close()
	}
	log.Info().Msg("shutdown: complete")
}
