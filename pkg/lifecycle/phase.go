// Package lifecycle implementa o cabeamento do processo único do
// mez-go-mono (Fase 8 #96, ADR 0021).
//
// O Runner coordena o boot determinístico e o graceful shutdown de todos
// os subsistemas do binário (C12 + D10). Cada subsistema vira uma Phase
// registrada com Start (síncrono, idempotente) e Stop (síncrono,
// idempotente). O Boot executa as phases em ordem; o Shutdown executa
// em LIFO. Goroutines long-running (relay, reconciler, HTTP listener) são
// iniciadas via Runner.Run, que encapsula wg.Add/wg.Done + recover() (C10).
//
// Princípios:
//
//   - Phases com Start/Stop explícitos. Falha em qualquer Start aborta o
//     boot e chama Shutdown parcial (LIFO).
//   - Cada phase roda no seu próprio ctx com timeout (default 5s),
//     isolado do ctx principal. Cancelamento cascata no Shutdown.
//   - Métricas: boot_phase_info{phase=...} (gauge 1/0), histogramas de
//     duração por phase, total de boot e shutdown.
//   - Wait() bloqueia até todas as goroutines Run-style terminarem.
//     Caller chama Wait() DEPOIS de Shutdown() para drenar tudo.
package lifecycle

// Nomes canônicos das phases do mez-go-mono. Usados em métricas e logs.
// Mantém stable: renomear quebra dashboards.
const (
	PhaseConfig         = "config"
	PhaseSealer         = "sealer"
	PhasePools          = "pools"
	PhaseTxRunner       = "txrunner"
	PhaseRepos          = "repos"
	PhaseBus            = "bus"
	PhaseIngestorRouter = "ingestor_router"
	PhaseRelay          = "relay"
	PhaseReconciler     = "reconciler"
	PhaseStatusConsumer = "status_consumer"
	PhaseWhatsmeow      = "whatsmeow"
	PhaseWebhook        = "webhook"
	PhaseAdminWeb       = "adminweb"
	PhaseHTTP           = "http"
)
