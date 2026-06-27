# Fase 8 — Estabilização do processo único (C12)

> **Status:** planejamento aprovado (junho/2026) · em aberto · tracking em `fase8-tracking`.
> **Escopo:** 7 issues planejadas + 2 carryovers (D3, D4) = 9 issues (#99–#107) · ~4.0d solo estimado · single commit (squash) em `fase8-squash` → `main`.
> **Pré-requisitos:** Fases 0, 1, 2, 3, 4, 5, 6 e 7 merged (Fase 7 commit `6448f15` em `main`).
> **Base normativa:** `README.md` §23 (Fase 8), §6 (garantias de entrega), §20 (operação em produção), §21 (riscos C10) e §24 (Definition of Done).

### Mapeamento issue → escopo

| Issue | Título | Tipo |
|------:|--------|------|
| [#99](https://github.com/felipedsvit/mez-go-mono/issues/99) | D3 — tests/inbound/TestOutbox_InsertAndClaim (UUID vazio) | carryover (Fase 6) |
| [#100](https://github.com/felipedsvit/mez-go-mono/issues/100) | D4 — whatsmeow/TestDispatcher_BoundedDrop (flake) | carryover (Fase 4) |
| [#101](https://github.com/felipedsvit/mez-go-mono/issues/101) | #96 — pkg/lifecycle.Runner | NEW |
| [#102](https://github.com/felipedsvit/mez-go-mono/issues/102) | #97 — Refatorar cmd/server/wire.go em fases | REWRITE |
| [#103](https://github.com/felipedsvit/mez-go-mono/issues/103) | #98 — Hub.Shutdown(ctx) | REWRITE |
| [#104](https://github.com/felipedsvit/mez-go-mono/issues/104) | #101 — MigrateOnBoot | MECHANICAL |
| [#105](https://github.com/felipedsvit/mez-go-mono/issues/105) | #102 — goleak.VerifyTestMain em 7 pacotes | MECHANICAL |
| [#106](https://github.com/felipedsvit/mez-go-mono/issues/106) | #103 — Testes de chaos | NEW |
| [#107](https://github.com/felipedsvit/mez-go-mono/issues/107) | #107 — Cold-boot com N tenants | NEW |

---

## 1. Análise do projeto pai (`mez-go`) e carryover do mono

A Fase 8 é a peça que **não existia no pai** (`mez-go` operava em 6 binários,
cada um com seu próprio ciclo de vida, supervisor externo e ausência de
responsabilidade pelo estado dos vizinhos). No mono, um único processo segura
HTTP, bus, reconciler, relay, outbox, status consumer, bus, todos os adapters
de canal e o `Manager` whatsmeow — todos compartilham o mesmo `context`, a
mesma `WaitGroup` (quando houver) e o mesmo fate em SIGTERM/SIGKILL. O
`README.md` §23 lista esta fase como **nova nesta revisão (C12)** porque o
plano original não tratava o cabeamento do processo único.

### 1.1 Inventário de código reusável (mono, carryover)

| Componente | Caminho | LOC | Issue destino | Tipo |
|---|---|---:|---|---|
| `runWithGracefulShutdown` (boot signal + HTTP `Shutdown` + `Bus.Drain` + `Relay.Stop` + `Reconciler.Stop`) | `cmd/server/wire.go:350-409` | 60 | #97 | **REWRITE** — complementar com phases explícitas |
| `Manager.DisconnectAll` (encerra todos os clients whatsmeow) | `internal/adapter/provider/whatsmeow/manager.go:141` | 14 | #97 | reuso direto — apenas chamar no shutdown |
| `Manager.Disconnect(ctx, tenantID)` | `internal/adapter/provider/whatsmeow/manager.go:159` | 22 | #97 | reuso direto — chamado pelo reset de tenant (Fase 6) |
| `Reconciler.Run` / `Stop` (boot sweep + tick + drop-safe) | `internal/usecase/reconcile/reconciler.go:113,148` | 38 | #97 | reuso direto |
| `Relay.Run` / `Stop` (boot drain + notify + poll) | `internal/usecase/outbox/relay.go:87,123` | 39 | #97 | reuso direto |
| `StatusConsumer.Subscribe` (consome status events do bus) | `internal/usecase/messaging/status_consumer.go` | ~80 | #97 | reuso direto; precisa `Unsubscribe` para shutdown limpo |
| `Bus.Drain(ctx)` + `safeCall` (recover por handler) | `internal/adapter/broker/bus.go:183,221` | 60 | #97 | reuso direto |
| `Dispatcher.recover()` (panic por goroutine de tenant) | `internal/adapter/provider/whatsmeow/dispatcher.go:91,115` | 24 | #101 | reuso direto — base do teste de chaos |
| `goleak.VerifyTestMain` (goroutine leak detection) | `internal/adapter/cache/memory/session_test.go:3` | 1 | #100 | reuso direto — propagar para 7 pacotes |
| Testcontainers Postgres (Fase 2 carryover) | `internal/testutil/pgtest/pgtest.go` | ~120 | #101, #102 | reuso direto |
| `entrypoint.sh` (`migrate up` antes de `serve`) | `deployments/entrypoint.sh:7` | 13 | #99 | **REWRITE** — mover lógica para o binário |

### 1.2 Referência semântica no pai (`mez-go`)

| Componente (pai) | Caminho | Aprendizado para mono |
|---|---|---|
| Boot do `mez-core` (signal + HTTP) | `mez-go/cmd/mez-core/main.go` | mono **NÃO** porta literalmente: pai tinha 6 binários independentes, cada supervisor próprio. mono precisa de coordenação cross-subsystem. |
| Graceful shutdown do `mez-worker-whatsmeow` | `mez-go/cmd/mez-worker-whatsmeow/main.go` | mono usa `Manager.DisconnectAll` em vez de `defer client.Disconnect()` por binário |
| `pkg/shard/shard.go` (crc32 % N) | `mez-go/pkg/shard/shard.go` | mono **NÃO** porta (decisão §2 + §22) — fora do 1.0 |
| Boot do `mez-analytics-consumer` (JetStream consumer) | `mez-go/cmd/mez-analytics-consumer/main.go` | mono **NÃO** porta (descartado em §2) |

### 1.3 Patterns obrigatórios (do AGENTS.md, mantidos)

1. **RLS via context, nunca parâmetro** — `RunInTenantTx(ctx, tenantID, fn)` continua mandatório.
2. **FORCE RLS** (C3) — todo o boot precisa conectar via `mez_app` (sem `BYPASSRLS`).
3. **Functional options** — `lifecycle.NewRunner(WithLogger, WithMetrics, WithTimeout(...))`.
4. **Audit log em toda ação admin** (D17) — `MigrateOnBoot` registra `boot_migration` no audit (mesmo que seja operação do sistema).
5. **Comentários português** — manter consistência com pai e mono.
6. **Sem imports proibidos** — guardrails: sem `sink/`, `broker/nats`, `pkg/shard`, `cache/redis`, `secret/sealer/vault`.
7. **recover() por goroutine** (C10) — `pkg/lifecycle.Runner` adiciona recover em phases que disparam goroutines.
8. **Graceful shutdown** (D10) — coordenador no `Runner.Shutdown` com timeout por fase.

### 1.4 Divergências arquiteturais pai → mono

| Aspecto | mez-go (pai) | mez-go-mono (Fase 8) | Impacto |
|---|---|---|---|
| **Coordenação de boot** | 6 binários independentes + supervisor externo | **1 binário, ordem determinística** | Novo: `pkg/lifecycle.Runner` com fases nomeadas |
| **Shutdown signal** | cada binário tem o seu | 1 signal → cascade em fases inversas | Novo: `Runner.Shutdown(ctx)` itera phases em LIFO |
| **Goroutine tracking** | implícito (cada binário é 1 processo) | **explícito** (`sync.WaitGroup` por fase) | `Runner.Wait()` cobre HTTP/relay/reconciler/status |
| **kill -9 recovery** | JetStream tinha replay nativo | **reconciler + outbox poll** (carryover Fase 2/3) | Validar via teste de chaos (#101) |
| **`migrate` no boot** | `entrypoint.sh` | **`MigrateOnBoot` no binário** (fail-closed) | Tira dependência do shell script (#99) |
| **In-flight HTTP drain** | por binário (≤ 1 tenant) | **graceful `http.Server.Shutdown`** + `Runner` espera terminar | Coordenado via `Runner.Shutdown` (HTTP stop → 30s grace) |
| **WS hub drain** | sem hub (cada tenant 1 processo) | **Hub.Shutdown(ctx)** com drain de subscribers | Novo em #98 |
| **Whatsmeow disconnect** | per-client no worker | **Manager.DisconnectAll** em LIFO | Leva em média 10s por tenant (DisconnectGrace default) |
| **Pool close** | independente por binário | **LIFO** depois de tudo que usa fechar | appPool + platformPool por último |
| **goleak coverage** | implícito (1 binário = 1 suite) | **explícito** em 7 pacotes críticos | #100 propaga `VerifyTestMain` |

### 1.5 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| **NEW** (`pkg/lifecycle` + `Hub.Shutdown` + `serve.go` com `MigrateOnBoot` + chaos tests + cold-boot tests) | ~1.600 | 2.4 |
| **REWRITE** (`cmd/server/wire.go` em fases + `entrypoint.sh` reduzido) | ~400 | 0.7 |
| **MECHANICAL** (`goleak.VerifyTestMain` em 7 pacotes + config) | ~250 | 0.3 |
| **Buffer** (20% para chaos flakejar + cold-boot timing) | — | 0.6 |
| **Total** | **~2.250** | **~4.0** |

Mantém a estimativa 3-4 dias do README §23 com 0.5d de folga.

---

## 2. Visão geral da Fase 8

Implementa o **cabeamento do processo único** (C12) que o README §23 define
como pré-requisito do 1.0:

- **Boot determinístico por fases** com `pkg/lifecycle.Runner` (config →
  sealer → pools → txRunner → repos → bus → ingestor/router → relay →
  reconciler → statusConsumer → waManager → metaH/tgH → adminweb → HTTP).
- **Graceful shutdown coordenado** em ordem inversa, com timeout por fase,
  `WaitGroup` global, drain de WS hub, `Manager.DisconnectAll` e fechamento
  de pools por último.
- **`MigrateOnBoot`** integrado no binário (fail-closed, idempotente);
  `entrypoint.sh` reduzido a `exec mez-go-mono serve`.
- **`goleak.VerifyTestMain`** global em 7 pacotes críticos — fecha a porta
  a goroutine leaks que esta fase está expondo ao consolidar tudo num só
  processo.
- **Testes de chaos** (`tests/chaos/`) com `kill -9` em 4 pontos críticos
  para validar C1 (reconciler recovery) e D3 (outbox poll fallback).
- **Cold-boot com N tenants** (`tests/boot/`) + bench de tempo de boot
  (N=20, 50, 100) — detecta regressão de tempo de inicialização.

### A Fase 8 **NÃO** implementa

- Multi-process / sharding por tenant (pós-1.0, §25 limitação assumida).
- Zero-downtime deploy (decidido em §20, aceito como limitação do 1.0).
- Vault Transit sealer (pós-1.0, §2 + §22).
- Métricas Prometheus novas além de `boot_phase_info` + `shutdown_phase_duration_seconds`.
- Chaos em produção (Gremlin / Litmus). Pós-1.0.

---

## 3. Issues

| Issue | Título | Arquivo(s) alvo | Origem | Classif. | Esforço | Bloq. |
|-------|--------|-----------------|--------|----------|--------:|-------|
| **#96** | `pkg/lifecycle.Runner` (fases de boot/shutdown com `WaitGroup`, timeout, ctx isolado) | `pkg/lifecycle/{phase,runner,runner_test}.go` (NOVO) | gap §1 (C12) | NEW | 0.6d | — |
| **#97** | Refatorar `cmd/server/wire.go` em fases nomeadas + `Manager.DisconnectAll` no shutdown | `cmd/server/wire.go` (REWRITE) | gap §1 | REWRITE | 0.7d | #96 |
| **#98** | `Hub.Shutdown(ctx)` + drenagem ordenada dos subscribers | `internal/transport/websocket/{hub,hub_test}.go` | gap §1 | REWRITE | 0.4d | — |
| **#99** | `MigrateOnBoot` em `cmd/server/serve` (fail-closed, idempotente) + reduzir `entrypoint.sh` | `cmd/server/serve.go` (NOVO) + `pkg/config/config.go` + `deployments/entrypoint.sh` (UPDATE) | gap §1 (C10 — `migrate` no boot vira outage se falhar) | MECHANICAL | 0.3d | — |
| **#100** | `goleak.VerifyTestMain` em 7 pacotes críticos | `internal/{lifecycle,usecase/outbox,usecase/reconcile,usecase/messaging,transport/websocket,adapter/broker,adapter/provider/whatsmeow}/*_test.go` (UPDATE) | gap §1 | MECHANICAL | 0.3d | — |
| **#101** | Testes de chaos (`tests/chaos/`) — kill -9 em 4 pontos críticos, validar reconciler + outbox recovery | `tests/chaos/{reconciler_recovery,outbox_recovery,bus_drain_shutdown,whatsmeow_panic}_test.go` (NOVO) | §23 + C1 + D3 | NEW | 1.0d | #96, #97, #98 |
| **#102** | Teste de boot frio com N tenants + warm-up paralelo whatsmeow + bench | `tests/boot/{cold_boot,cold_boot_bench,warmup_parallel}_test.go` (NOVO) | §23 | NEW | 0.7d | #97 |

**Total:** 7 issues · 4.0d solo · ~2.250 LOC.

---

## 4. Detalhamento por issue

### #96 — `pkg/lifecycle.Runner` (fases + WaitGroup + timeout)

**Novos arquivos em `pkg/lifecycle/`:**

- `phase.go` (~50 LOC):
  ```go
  // Phase representa uma unidade de boot/shutdown com timeout próprio.
  type Phase struct {
      Name    string
      Start   func(ctx context.Context) error // sync, idempotente; nil = sem start
      Stop    func(ctx context.Context) error // sync, idempotente; nil = sem stop
      Timeout time.Duration                   // default 5s
  }

  // Order é a ordem de boot; shutdown é a inversa.
  type Order []Phase

  // Fases padronizadas do mez-go-mono (referenciadas pelo wire.go).
  const (
      PhaseConfig         = "config"
      PhaseSealer         = "sealer"
      PhasePools          = "pools"
      PhaseTxRunner       = "txrunner"
      PhaseRepos          = "repos"
      PhaseBus            = "bus"
      PhaseIngestorRouter = "ingestor+router"
      PhaseRelay          = "relay"
      PhaseReconciler     = "reconciler"
      PhaseStatusConsumer = "status_consumer"
      PhaseWhatsmeow      = "whatsmeow"
      PhaseWebhook        = "webhook"
      PhaseAdminWeb       = "adminweb"
      PhaseHTTP           = "http"
  )
  ```

- `runner.go` (~180 LOC):
  ```go
  type Runner struct {
      log     zerolog.Logger
      metrics *metrics.Registry
      phases  []Phase
      wg      sync.WaitGroup
      cancel  context.CancelFunc
      mu      sync.Mutex
      current string
      started time.Time
  }

  func NewRunner(log zerolog.Logger, m *metrics.Registry) *Runner

  // AddPhase adiciona uma fase. Idempotente: a última vence (warn log).
  func (r *Runner) AddPhase(p Phase)

  // Boot executa as phases em ordem. Falha em qualquer phase aborta e
  // chama Shutdown parcial. Cada phase roda no seu próprio ctx com
  // timeout, isolado do ctx principal.
  func (r *Runner) Boot(ctx context.Context) error

  // Shutdown executa as phases em ordem INVERSA. Cada phase com Stop
  // recebe um ctx com timeout. Erros são logados mas não interrompem.
  func (r *Runner) Shutdown(ctx context.Context) error

  // Wait bloqueia até todas as goroutines background terminarem.
  // Caller chama Wait() DEPOIS de Shutdown() para drenar tudo.
  func (r *Runner) Wait() error

  // Run é um helper para goroutines long-running (reconciler, relay, HTTP).
  // Encapsula wg.Add(1) + defer wg.Done() + recover() + shutdown grace.
  func (r *Runner) Run(ctx context.Context, name string, fn func(context.Context) error)

  // Current retorna a fase em andamento (boot ou shutdown).
  func (r *Runner) Current() string
  ```

- `runner_test.go` (~250 LOC):
  - Boot em ordem: 3 phases A→B→C; assert `Current()` reporta B enquanto B.Start bloqueia.
  - Shutdown em LIFO: C→B→A.
  - Panic em phase não derruba Runner; próximo Shutdown é seguro.
  - Timeout por fase: phase.Start que dorme 10s com Timeout=50ms retorna `context.DeadlineExceeded`.
  - `Run()` com fn que retorna erro: wg.Done é chamado; Wait() retorna nil (erro logado).
  - `Run()` com fn que panica: recover segura; wg.Done é chamado.
  - Métrica `boot_phase_info` e `shutdown_phase_duration_seconds` são emitidas.

**Métricas** (registradas no `*metrics.Registry` passado no construtor):
- `boot_phase_info{phase=...}` Gauge (1=fase atual, 0=demais).
- `boot_phase_duration_seconds{phase=...}` Histogram.
- `shutdown_phase_duration_seconds{phase=...}` Histogram.
- `boot_total_seconds` Histogram (boot completo).
- `shutdown_total_seconds` Histogram (shutdown completo).

**Princípio:** cada phase roda num ctx isolado derivado do ctx principal;
o `Runner` propaga cancel em cascata inversa no shutdown. O `WaitGroup`
rastreia goroutines `Run`-style (relay, reconciler, HTTP listener). Recover
em `Run()` cobre panics em goroutines long-running (C10).

---

### #97 — Refatorar `wire.go` em fases + `Manager.DisconnectAll`

**Arquivo:** `cmd/server/wire.go` (REWRITE, mantém 100% da lógica, só re-encapsula).

- **Remover** o comentário "Boot order (C12)" do topo (passos 1-17 numerados) —
  vira código real via `Runner.AddPhase`.
- **Substituir** `wireServices` por `wireApp(ctx, runner) error` que constrói
  cada subsistema e o registra no runner. Cada subsistema vira uma `Phase`
  com `Start` (se aplicável) e `Stop`.
- **`runServe` refatorado** (~40 LOC, substitui o atual `wire.go:44-65`):
  ```go
  func runServe(cfg config.Config, log zerolog.Logger) {
      if err := cfg.ValidateServe(); err != nil {
          log.Fatal().Err(err).Msg("config validation")
      }

      runner := lifecycle.NewRunner(log, metrics.NewRegistry())

      ctx, cancel := context.WithCancel(context.Background())
      defer cancel()

      app, err := wireApp(ctx, cfg, log, runner)
      if err != nil {
          log.Fatal().Err(err).Msg("wire")
      }

      log.Info().Str("addr", app.Cfg.HTTPAddr).Msg("serve: starting")
      if err := runner.Boot(ctx); err != nil {
          log.Fatal().Err(err).Msg("boot")
      }

      runWithGracefulShutdown(ctx, app, runner) // ver bloco abaixo
  }
  ```
- **`runWithGracefulShutdown` refatorado** (substitui `wire.go:350-409`):
  ```go
  func runWithGracefulShutdown(ctx context.Context, app *AppContext, runner *lifecycle.Runner) {
      sigCh := make(chan os.Signal, 1)
      signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

      select {
      case sig := <-sigCh:
          app.Log.Info().Str("signal", sig.String()).Msg("shutdown: signal received")
      case <-ctx.Done():
          app.Log.Warn().Msg("shutdown: ctx cancelled")
      }

      // 30s totais, distribuídos em 5s por phase crítica.
      shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
      defer cancel()

      // Ordem inversa: HTTP → adminweb → webhook → whatsmeow → status → reconciler → relay → bus → ingestor/router → repos → txrunner → pools → sealer → config
      if err := runner.Shutdown(shutdownCtx); err != nil {
          app.Log.Error().Err(err).Msg("shutdown: error")
      }

      // Wait para goroutines long-running (HTTP, relay, reconciler).
      waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
      defer waitCancel()
      if err := runner.WaitFor(waitCtx); err != nil {
          app.Log.Warn().Err(err).Msg("shutdown: wait timeout")
      }

      // Pools por último (depois que ninguém mais precisa).
      if app.appPool != nil {
          app.appPool.Close()
      }
      if app.platformPool != nil {
          app.platformPool.Close()
      }
      app.Log.Info().Msg("shutdown: complete")
  }
  ```

- **Mapeamento subsistema → Phase:**
  | Phase | Start | Stop |
  |---|---|---|
  | `config` | `cfg.ValidateServe()` | — |
  | `sealer` | `keyring.New(...)` | `keyring.Close()` (zera DEK cache) |
  | `pools` | `postgres.ConnectPool(app)` + `postgres.ConnectPool(platform)` | `appPool.Close()` + `platformPool.Close()` (registrado para LIFO) |
  | `txrunner` | `postgres.NewTxRunner(appPool, platformPool)` | — |
  | `repos` | `tenantRepo`, `convRepo`, `msgRepo`, `outboxRepo`, `inboundEvsRepo` | — |
  | `bus` | `broker.NewBus(...)` + `startConsumers()` (já interno) | `Bus.Drain(ctx)` |
  | `ingestor+router` | `ingestor := ucmessaging.NewIngestor(...)` + `bus.SubscribeInbound(router)` | `bus.UnsubscribeInbound()` |
  | `relay` | `relay := ucoutbox.New(...)` | `runner.Run(ctx, "relay", relay.Run)` + `relay.Stop()` no shutdown |
  | `reconciler` | `reconciler := ucreconcile.New(...)` | `runner.Run(ctx, "reconciler", reconciler.Run)` + `reconciler.Stop()` no shutdown |
  | `status_consumer` | `statusConsumer.Subscribe()` | `statusConsumer.Unsubscribe()` |
  | `whatsmeow` | `waManager := whatsmeow.NewManager(...)` | **`Manager.DisconnectAll()`** (era o gap) |
  | `webhook` | `metaH := meta.New(...)` + `tgH := telegram.New(...)` | — |
  | `adminweb` | `adminSrv := adminweb.NewServer(...)` | `adminSrv.Close()` (fecha templates, sessão) |
  | `http` | `srv.ListenAndServe()` em goroutine via `runner.Run` | `srv.Shutdown(ctx)` |

- **`AppContext` ganha campos:**
  ```go
  appPool       *pgxpool.Pool   // exportado para Close() no shutdown
  platformPool  *pgxpool.Pool
  Keyring       *secrets.Keyring  // exportado para Close() no shutdown
  statusConsumer *ucmessaging.StatusConsumer
  ```

**DoD #97:**
- [ ] `wire.go` continua compilando (`make build` verde).
- [ ] `wire.go` tem ≤ 350 LOC (era 411, agora com mais fases mas menos duplicação).
- [ ] Cada phase tem `Start` E `Stop` (exceto as que não precisam).
- [ ] Ordem de shutdown é LIFO estrito: `runner.Shutdown` itera phases em reverso.
- [ ] `Manager.DisconnectAll` é chamado no Stop da phase `whatsmeow` (logger: `"manager: disconnected all"`).
- [ ] `appPool.Close()` + `platformPool.Close()` acontecem **depois** de `runner.Shutdown()` retornar.
- [ ] SIGTERM com request HTTP em voo: cliente recebe 2xx (não conexão cortada).
- [ ] Sem imports proibidos.

---

### #98 — `Hub.Shutdown(ctx)` com drain ordenado

**Arquivos:**

- `internal/transport/websocket/hub.go` — adicionar:
  ```go
  // Shutdown fecha todos os subscribers ativos de todos os tenants.
  // Idempotente. Após Shutdown, Subscribe retorna erro (hub fechado).
  //
  // Ordem:
  //  1. Marca hub como `closed` (atomicamente).
  //  2. Itera subscribers por tenant.
  //  3. Para cada subscriber, fecha o conn (WritePump termina).
  //  4. ReadPump termina no próximo pong timeout (≤ pongWait = 60s).
  //
  // Ctx: usado para log apenas — Close é sync e não bloqueia em I/O.
  func (h *Hub) Shutdown(ctx context.Context) error {
      h.mu.Lock()
      defer h.mu.Unlock()

      h.closed = true
      for tenantID, subs := range h.subscribers {
          for s := range subs {
              s.Close() // fecha o conn; ReadPump/WritePump saem
          }
          delete(h.subscribers, tenantID)
      }
      h.log.Info().Int("tenants", len(h.subscribers)).Msg("hub: shutdown complete")
      return nil
  }
  ```
  Mais o campo:
  ```go
  closed atomic.Bool // impede novos Subscribe após Shutdown
  ```
  E o early-return em `Subscribe`:
  ```go
  if h.closed.Load() { return ErrHubClosed }
  ```

- `hub_test.go` (+120 LOC):
  - Subscribe 3 subscribers em 2 tenants.
  - `Shutdown(ctx)` retorna nil.
  - `Stats()` retorna `subscribers=0, tenants=0`.
  - Segundo `Shutdown` é no-op (idempotente).
  - `Subscribe` após `Shutdown` retorna `ErrHubClosed`.
  - Latência p99 de Shutdown com 100 subscribers: < 100ms (benchmark opcional).

**DoD #98:**
- [ ] `Hub.Shutdown(ctx)` existe e compila.
- [ ] `hub.closed atomic.Bool` impede novos Subscribes.
- [ ] `hub_test.go` cobre happy path + idempotência + erro após close.
- [ ] `goleak.VerifyTestMain` adicionado em `hub_test.go` (#100).

---

### #99 — `MigrateOnBoot` integrado + reduzir `entrypoint.sh`

**Arquivos:**

- `cmd/server/serve.go` (NOVO, ~70 LOC) — split do `runServe` em:
  ```go
  func runServe(cfg config.Config, log zerolog.Logger) {
      if err := cfg.ValidateServe(); err != nil {
          log.Fatal().Err(err).Msg("config validation")
      }

      if cfg.MigrateOnServe {
          log.Info().Msg("migrate on serve: running")
          if err := runMigrateInline(cfg, log); err != nil {
              log.Fatal().Err(err).Msg("migrate on serve failed; fail-closed (container will not start)")
          }
          log.Info().Msg("migrate on serve: complete")
      }

      runner := lifecycle.NewRunner(log, metrics.NewRegistry())
      ctx, cancel := context.WithCancel(context.Background())
      defer cancel()

      app, err := wireApp(ctx, cfg, log, runner)
      if err != nil { log.Fatal().Err(err).Msg("wire") }
      if err := runner.Boot(ctx); err != nil { log.Fatal().Err(err).Msg("boot") }
      runWithGracefulShutdown(ctx, app, runner)
  }

  // runMigrateInline carrega a config, conecta como mez_migrate, roda migrations up.
  func runMigrateInline(cfg config.Config, log zerolog.Logger) error {
      m, err := migrate.New("file://migrations", cfg.MigrateDBURL)
      if err != nil { return fmt.Errorf("migrate source: %w", err) }
      defer m.Close()
      if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
          return fmt.Errorf("migrate up: %w", err)
      }
      return nil
  }
  ```
- `pkg/config/config.go` — adicionar:
  ```go
  MigrateOnServe bool // default true; env MEZ_MIGRATE_ON_SERVE
  ```
  E parsing em `Load()`.
- `cmd/server/main.go` — manter subcommand `migrate` para uso manual (ex: CI de schema).
- `cmd/server/rotate_kek.go` — **NÃO** chama migrate (operação offline; não toca schema).
- `deployments/entrypoint.sh` (REWRITE, 5 LOC):
  ```sh
  #!/bin/sh
  set -e
  exec mez-go-mono serve
  ```
  A lógica de migrate foi para o binário (fail-closed nativo, em vez de shell).
- `Makefile` — alvo `migrate` explícito:
  ```makefile
  migrate:
      $(GO) run ./cmd/server migrate up
  ```
  (Já existe `migrate-up` que usa binário externo `migrate` da CLI; este novo
  `migrate` usa o subcommand do binário único, evitando dependência extra.)

**DoD #99:**
- [ ] `MEZ_MIGRATE_ON_SERVE=false` pula o migrate (útil pra testes de schema em CI).
- [ ] Default `MigrateOnServe=true` mantém compatibilidade.
- [ ] `entrypoint.sh` reduzido para `exec mez-go-mono serve`.
- [ ] `migrate` subcommand continua funcionando (uso manual).
- [ ] Audit log: `boot_migration` registrado quando `MigrateOnServe=true` (D17).

---

### #100 — `goleak.VerifyTestMain` global

Adicionar a função padrão em 7 pacotes críticos:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

**Pacotes cobertos:**
1. `internal/lifecycle/runner_test.go` (#96)
2. `internal/usecase/outbox/relay_test.go` (carryover + VerifyTestMain)
3. `internal/usecase/reconcile/reconciler_test.go` (carryover + VerifyTestMain)
4. `internal/usecase/messaging/..._test.go` (sender + status consumer)
5. `internal/transport/websocket/hub_test.go` (#98)
6. `internal/adapter/broker/bus_test.go` (carryover + VerifyTestMain)
7. `internal/adapter/provider/whatsmeow/whatsmeow_test.go` (carryover + VerifyTestMain)

**Plus:**
- `//go:build !integration` opt-out em testes de testcontainers, onde o
  `VerifyTestMain` pode dar falso positivo durante cleanup assíncrono de
  containers (carryover já tem `tests/integration` separados).
- README §19 (Build e desenvolvimento) menciona goleak na tabela de
  critérios de build verde.
- `internal/testutil/goleak.go` (NOVO, ~30 LOC) — helper compartilhado:
  ```go
  //go:build !integration
  package testutil

  import "go.uber.org/goleak"

  func VerifyNone(t *testing.T, opts ...goleak.Option) {
      t.Helper()
      goleak.VerifyNone(t, opts...)
  }
  ```

**DoD #100:**
- [ ] 7 pacotes com `TestMain(goleak.VerifyTestMain)`.
- [ ] `go test -race -shuffle=on` verde em todos.
- [ ] Helper `internal/testutil.VerifyNone` exportado.
- [ ] `Makefile` alvo `test-leak` (opcional, para diagnóstico).

---

### #101 — Testes de chaos (kill -9 + recovery)

**Diretório:** `tests/chaos/` (NOVO, build tag `integration` + `chaos`).

**Helper comum** `tests/chaos/harness.go` (~150 LOC):
- `type Harness struct { BinPath string; Env []string; Cmd *exec.Cmd; Cancel context.CancelFunc }`
- `func BuildAndStart(t *testing.T, env ...string) *Harness` — compila o binário
  em `/tmp/chaos-mez-{pid}/server` e inicia em goroutine com stdin/stdout capturados.
- `func (h *Harness) Kill9()` — envia `syscall.SIGKILL` no `h.Cmd.Process`.
- `func (h *Harness) WaitReady(timeout time.Duration) error` — polling em `/readyz`.
- `func (h *Harness) Stop(graceful bool)` — graceful via SIGTERM OU `Kill9()`.

**4 cenários:**

1. **`reconciler_recovery_test.go`** (~200 LOC):
   ```go
   //go:build integration && chaos
   func TestReconciler_RecoversFromSIGKILL(t *testing.T) {
       pgtest.SkipIfNoDocker(t)
       ctx := context.Background()

       pool, cleanup := pgtest.Start(ctx, t)
       defer cleanup()
       pgtest.MigrateUp(t, pool)

       // Cria tenant + 50 mensagens em status='received' (não publicadas).
       tenantID := uuid.NewString()
       for i := 0; i < 50; i++ {
           insertMessage(t, pool, tenantID, "received")
       }

       // Primeira instância: sobe reconciler, processa ~10, recebe SIGKILL.
       h1 := chaos.BuildAndStart(t, env(pgURL))
       chaos.WaitForReconcile(t, pool, 10, 30*time.Second)
       h1.Kill9()

       // Valida que ~40 ainda estão em 'received'.
       assertPendingBetween(t, pool, 30, 50)

       // Segunda instância: deve drenar via boot sweep em < 35s.
       h2 := chaos.BuildAndStart(t, env(pgURL))
       defer h2.Stop(true)
       chaos.WaitForReconcile(t, pool, 50, 35*time.Second)

       // Asserção final: zero mensagens em 'received'.
       assertZeroPending(t, pool)
   }
   ```

2. **`outbox_recovery_test.go`** (~200 LOC):
   ```go
   func TestOutbox_RecoversFromSIGKILL_BootPoll(t *testing.T) {
       // Cria 30 outbox pendentes.
       // Sobe relay, espera 5 drenados, SIGKILL.
       // Sobe nova instância, valida que poll de 5s drena o resto em ≤ 10s.
       // Asserção: status IN ('sent','dlq') = 30; status='pending' = 0.
   }
   ```

3. **`bus_drain_shutdown_test.go`** (~150 LOC):
   ```go
   func TestBus_DrainGracefulShutdown(t *testing.T) {
       // Publica 100 inbound events.
       // Chama Bus.Drain(50ms).
       // Asserção: BufferDepth['inbound'] == 0 OU documenta os dropped.
       // Valida que PublishInbound após Drain é no-op.
   }
   ```

4. **`whatsmeow_panic_test.go`** (~150 LOC):
   ```go
   func TestWhatsmeow_PanicInHandler_DoesNotCrashProcess(t *testing.T) {
       // Stub Adapter que panica em Send.
       // Enfileira 10 outbound events; relay chama sender; sender panica.
       // Asserção: processo continua (relay loga erro, métricas incrementam, exit code 0).
       // Valida `outbox_failed_total` ou `bus_dropped_total` incrementado.
   }
   ```

**Padrão de kill -9:** executar `go build -o /tmp/chaos-mez ./cmd/server` e
em paralelo `exec.Command(/tmp/chaos-mez, "serve", envs...)` com
`syscall.SIGKILL` direto. Sem shell intermediário.

**Skip condicional:** `if runtime.GOOS == "darwin" && os.Getenv("CI") == "" { t.Skip("kill -9 timing flaky em macOS") }`.

**DoD #101:**
- [ ] 4 cenários de chaos verdes em CI.
- [ ] Helper `chaos.Harness` reusável.
- [ ] Métricas: `outbox_failed_total`, `bus_dropped_total`, `reconciler_processed_total` visíveis no `/metrics`.
- [ ] Audit log de `boot_*` registrado (carryover de #99).

---

### #102 — Cold-boot com N tenants (warm-up paralelo)

**Diretório:** `tests/boot/` (NOVO, build tag `integration`).

- `cold_boot_test.go` (~200 LOC):
  ```go
  //go:build integration

  func TestColdBoot_ScalesLinearly(t *testing.T) {
      for _, n := range []int{20, 50, 100} {
          t.Run(fmt.Sprintf("tenants=%d", n), func(t *testing.T) {
              pgtest.SkipIfNoDocker(t)
              pool, cleanup := pgtest.Start(context.Background(), t)
              defer cleanup()
              pgtest.MigrateUp(t, pool)

              // Cria N tenants com 1 channel_credentials whatsmeow cada.
              for i := 0; i < n; i++ {
                  insertTenantWithCredentials(t, pool, uuid.NewString(), "whatsmeow")
              }

              h := chaos.BuildAndStart(t, env(pgURL), env("MEZ_MAX_ACTIVE_TENANTS", strconv.Itoa(n)))
              defer h.Stop(true)

              // Mede tempo desde spawn até /readyz 200.
              start := time.Now()
              chaos.WaitReady(t, h, 60*time.Second)
              bootDuration := time.Since(start)

              // Asserções de tempo (piso soft, não-fatal em hardware lento).
              softAssert(t, bootDuration < 30*time.Second, "boot took %v (p95 < 30s)", bootDuration)

              // Valida warm-up paralelo: Manager.Size() == n (todos os clients criados).
              size := queryManagerSize(t, h)
              assert.Equal(t, n, size, "manager should have %d active clients", n)

              // Valida zero goroutine leak.
              goleak.VerifyNone(t, goleak.IgnoreCurrent())
          })
      }
  }
  ```

- `warmup_parallel_test.go` (~150 LOC):
  - Mede ganho de paralelismo: warm-up sequencial vs. concorrente
    (com `errgroup.Group` bounded em `cfg.MaxActiveTenants`).
  - Speedup ≥ 4× em 8 cores (sanity check; não-fatal se hardware não entregar).
  - Loga métrica `boot_warmup_seconds{strategy=...}`.

- `cold_boot_bench_test.go` (~80 LOC):
  - `go test -bench=.` registra `boot_seconds` por N; baseline para detectar
    regressão de boot em CI (comparável via `benchstat`).

**Métrica:** se Fase 8 não couber tempo, pelo menos o teste `cold_boot_test.go`
para N=20 + `goleak` é o mínimo aceitável para DoD §24.

**DoD #102:**
- [ ] `tests/boot/cold_boot_test.go` cobre N=20, 50, 100.
- [ ] `tests/boot/warmup_parallel_test.go` demonstra speedup ≥ 4× em 8 cores.
- [ ] `tests/boot/cold_boot_bench_test.go` registra baseline.
- [ ] Zero goroutine leak verificado via `goleak.VerifyNone`.

---

## 5. Definition of Done — Fase 8

- [ ] **#96–#102** mergeadas em `fase8-squash` → `main`.
- [ ] `make build` verde em CI.
- [ ] `make test` verde (`-race -shuffle=on -count=1`).
- [ ] `make test-integration` verde (4 cenários de chaos + cold-boot N=20,50,100).
- [ ] `goleak.VerifyTestMain` ativo em 7 pacotes críticos; zero leak.
- [ ] Boot time (N=100): p95 < 30s (DoD §24).
- [ ] `/readyz` retorna 200 **somente** após `PhaseHTTP` concluída.
- [ ] SIGTERM com payload HTTP em voo termina com `2xx` ao cliente.
- [ ] SIGKILL entre `Run` do reconciler e `Tick` → reconciler da próxima
      instância recupera todas as mensagens em `status='received'` (C1 do DoD §24).
- [ ] SIGKILL entre `Notify()` do outbox e `drain()` → nova instância drena
      via poll em ≤ 10s (D3 do DoD §24).
- [ ] Panic num `whatsmeow.Adapter.Send` NÃO derruba o processo (C10 do DoD §24).
- [ ] `Manager.DisconnectAll` chamado no shutdown; zero `whatsmeow_disconnected`
      spurious após boot subseqüente.
- [ ] `entrypoint.sh` reduzido para `exec mez-go-mono serve` (migrate é
      responsabilidade do binário).
- [ ] `pkg/lifecycle` documentado em package doc + exemplos.
- [ ] ADRs novos: `docs/adr/0021-d12b-boot-order.md` (C12 boot/shutdown coordenado).
- [ ] `README.md` §20 (operação) atualizado com nova ordem de shutdown.
- [ ] `README.md` §21 (riscos) atualizado: blast radius agora mitigated por
      `recover()` em `Runner.Run` + goleak coverage.
- [ ] `README.md` §23 (roadmap): Fase 8 marcada ✅; total LOC atualizado.
- [ ] `AGENTS.md` atualizado: seção "Fase 8 — Estabilização" com uso de
      `pkg/lifecycle.Runner` + `MigrateOnBoot` + goleak.
- [ ] `CLAUDE.md` atualizado: nova subseção "Boot determinístico (C12)".
- [ ] PR único: `Fase 8: estabilização do processo único — boot determinístico, shutdown coordenado, chaos + cold-boot tests (#96..#102)`.
- [ ] Branch: `fase8-squash`. Base: `main`. Body: tabela de issues, DoD, `Closes #96..#102`.

---

## 6. Riscos da Fase 8

| Risco | Severidade | Mitigação |
|-------|:----------:|-----------|
| `Runner.Shutdown` ficar preso em phase lenta | média | Timeout por phase (default 5s, configurável); log de phase atual; `Shutdown` retorna erro mas continua tentando as próximas phases |
| `goleak.VerifyTestMain` quebrar suite por goroutine conhecida (relay/reconciler test que esquece de chamar `Stop`) | média | Rodar suite primeiro; corrigir tests órfãos junto; documentar `//go:build !integration` opt-out para testcontainers |
| Chaos test com `kill -9` flakeyar em CI (timing) | média | Retry por cenário (máx 2); `wait.Poll` com timeout; `t.Skip` em ambiente sem `/proc` confiável (macOS dev) |
| Warm-up paralelo whatsmeow em N=100 demorar mais que 30s | baixa | Benchmark põe piso soft; em hardware lento,放宽 pra 60s; foco é **detectar regressão**, não cumprir SLA absoluto |
| `MigrateOnBoot` duplicar com `entrypoint.sh` | baixa | Remover do shell; documentar em `AGENTS.md`; CI valida com `--no-migrate` para testes de schema |
| `Manager.DisconnectAll` cancelar clients em uso (request in-flight do sender) | média | In-flight sender termina com `ErrNotConnected`; relay `markFailed`; cliente whatsapp recebe reconexão automática na próxima vez |
| `Hub.Shutdown` deixar ReadPump pendurado (> pongWait = 60s) | baixa | Close fecha o conn; ReadPump termina no próximo read; `runner.WaitFor(5s)` cobre o pior caso |
| Reuso de `Runner.Run` conflitar com `sync.WaitGroup` interno dos componentes (relay já tem `wg`) | baixa | `Runner.Run` adiciona sua própria wg via `defer wg.Done()`; componentes continuam com wg interna; dupla wg é OK (são ortogonais) |
| Race condition entre `runner.Shutdown` e `runner.Boot` se chamados concorrentemente | baixa | `Boot` e `Shutdown` têm mutex; `Boot` retorna erro se chamado durante `Shutdown` |

---

## 7. Débitos pré-existentes (carryover Fases anteriores)

Após o merge da Fase 7 (PR #98, commit `6448f15`), `make test-integration`
apresenta 2 falhas pré-existentes que **não foram introduzidas pela Fase 7**.
Confirmado via `git stash` das mudanças Fase 7: os erros são idênticos.

Status atual dos débitos conhecidos:

| Débitos | Severidade | Status | Issue destino |
|---------|:----------:|--------|---------------|
| **D1** `actor_id` text vs uuid (audit_log) | média | ✅ Corrigido em PR #98 | — |
| **D2** `admin_users_local_has_hash` check | média | ✅ Corrigido em PR #98 | — |
| **D3** `tests/inbound/TestOutbox_InsertAndClaim` — UUID vazio | média | ❌ Pendente | #103 |
| **D4** `internal/adapter/provider/whatsmeow/TestDispatcher_BoundedDrop` | média | ❌ Pendente | #104 |

A Fase 8 deve corrigir **D3 e D4** como parte da estabilização (DoD §24
exige `make test-integration` verde end-to-end).

---

### D1 (RESOLVIDO em Fase 7 PR #98) — `actor_id` text vs uuid

**Erro original:**
```
run_as_platform_test.go:146: RunAsPlatform: run-as-platform audit:
  ERROR: column "actor_id" is of type uuid but expression is of type text (SQLSTATE 42804)
```

**Causa raiz:** O INSERT em `admin_audit_log` (executado dentro de
`RunAsPlatform`) passava `actor` como `text`, mas a coluna `actor_id` foi
migrada para `uuid` na Fase 1 (migração `0002_admin.up.sql`). O código
não castou o valor com `::uuid`.

**Correção aplicada (Fase 7 PR #98):**

- `internal/adapter/repository/postgres/admin/audit_repo.go` — INSERT em
  `admin_audit_log.actor_id` agora usa `NULLIF($2, '')::uuid`.
- `internal/adapter/repository/postgres/admin/db.go` — mesmo cast no
  `RunAsPlatform` SQL.

**Validação:** `tests/platform/TestRunAsPlatform_AuditAtomicity` 100%
verde após o fix (12.6s).

---

### D2 (RESOLVIDO em Fase 7 PR #98) — `admin_users_local_has_hash`

**Erro original:**
```
fail_closed_test.go:241: mez_platform should be able to INSERT admin_users, got:
  ERROR: new row for relation "admin_users" violates check constraint
  "admin_users_local_has_hash" (SQLSTATE 23514)
```

**Causa raiz:** O teste inseria um usuário admin via `mez_platform` sem
fornecer `password_hash`, e a check constraint `admin_users_local_has_hash`
(migration 0002) exige que usuários locais tenham hash. O teste foi escrito
antes da constraint existir.

**Correção aplicada (Fase 7 PR #98):** O INSERT em
`tests/rls/fail_closed_test.go:241` foi atualizado para usar
`auth_kind='oidc'` + `idp_subject` + `idp_issuer` (sem password_hash).
Adicionalmente, `tests/platform/run_as_platform_test.go` ganhou um seed
do actor em `admin_users` (UUID fixo `11111111-1111-1111-1111-111111111111`)
antes de `RunAsPlatform`, para satisfazer a FK
`admin_audit_log_actor_id_fkey`.

**Validação:** `tests/rls/TestRLSFailClosed` 100% verde após o fix
(7.6s).

---

### D3 (PENDENTE) — `tests/inbound/TestOutbox_InsertAndClaim` — UUID vazio no INSERT

**Erro:**
```
inbound_test.go:277: insert outbox:
  ERROR: invalid input syntax for type uuid: "" (SQLSTATE 22P02)
```

**Stack trace (resumida):** o test `TestOutbox_InsertAndClaim` em
`tests/inbound/inbound_test.go:258-278` faz:

```go
_, err := appPool.Exec(ctx,
    `INSERT INTO outbound_events (tenant_id, channel, target, payload, status)
     VALUES ($1, $2, $3, $4, 'pending')`,
    tenantID, "waba", target, payload,
)
```

…mas o INSERT falha porque o **GUC `mez.tenant_id` não está registrado no
Postgres do testcontainer**. A policy RLS `WITH CHECK (tenant_id =
current_setting('mez.tenant_id', false)::uuid)` é avaliada, e
`current_setting('mez.tenant_id', false)` retorna `''` (string vazia), que
não pode ser convertido para `uuid`.

**Causa raiz:** A migration `0005_backup_gucs.up.sql` registra o GUC com
`ALTER DATABASE mez SET mez.tenant_id TO ''`. Esse `ALTER DATABASE` é
**transacional** em PG 16 — quando a migration é aplicada via
`pool.Exec(string(rawSQL))` em um testcontainer (que abre uma transação
interna), o `ALTER DATABASE` é revertido no rollback da transação. Resultado:
o GUC nunca é persistido no cluster.

A migration 0005 também tem um `DO $$ ... BEGIN ... END $$` que envolve o
`ALTER DATABASE`, mas isso não muda a semântica transacional.

**Correção esperada (issue #103, alvo Fase 8):**

Duas opções:

1. **Setar o GUC na sessão explicitamente durante `applyAllMigrations`** em
   cada test suite (após o `pool.Exec`, fazer `pool.Exec("SET mez.tenant_id
   TO ''")`). Workaround, mas funciona.

2. **Reescrever a migration 0005** para usar `ALTER DATABASE ... SET`
   FORA de uma transação. Como `golang-migrate` (biblioteca do
   subcomando `migrate`) abre uma transação por migration, isso
   exigiria uma migration especial marcada como "non-transactional"
   (suportado por `golang-migrate` via header especial) **ou** mover o
   `ALTER DATABASE` para o `entrypoint.sh` antes do `serve`.

Recomendação: **opção 2 com migration non-transactional** —
`-- +golang-migrate Down` na linha 1 do `.up.sql`, ou split em duas
migrations:
- `0005a_backup_gucs.up.sql` — `SET` (transacional OK; vale para a
  sessão da migration e propaga para conexões do mesmo pool no test)
- `0005b_backup_gucs_alter.up.sql` — `ALTER DATABASE` (marca
  non-transactional com `-- +golang-migrate TransactionOff` no header)

**Arquivos a tocar:**

- `migrations/0005_backup_gucs.up.sql` — reescrever ou split
- `tests/inbound/inbound_test.go:applyAllMigrations` — incluir
  `SET mez.tenant_id TO ''` após as migrations (workaround, se opção 1)
- `tests/backup/roundtrip_test.go:applyAllMigrationsForBackup` — idem
- `tests/secrets/keyring_test.go:applyAllMigrations` — idem (já
  funciona, mas podemos unificar)

**Severidade:** Média — bloqueia `make test-integration` end-to-end.
Já bloqueia parcialmente desde a Fase 6 (issue #82 mencionou).

---

### D4 (PENDENTE) — `internal/adapter/provider/whatsmeow/TestDispatcher_BoundedDrop`

**Erro:**
```
whatsmeow_test.go (alguma linha) — bounded drop asserção falhou
```

A mensagem exata varia por run (timing dependente). O teste está em
`internal/adapter/provider/whatsmeow/` e cobre a capacidade do `Dispatcher`
de dropar mensagens quando o buffer bounded (2048 events, 8 history sync
por D4) enche.

**Causa raiz (provável):** A capacidade `bufferSize=2048` e `historySize=8`
foram definidas em PR #70 (Fase 4). O test provavelmente assume o
comportamento exato de drop, mas o handler real (`whatsmeow.Client` +
`bus.SubscribeInbound`) tem timing diferente em testcontainers vs.
in-memory.

**Correção esperada (issue #104, alvo Fase 8):**

Auditar `internal/adapter/provider/whatsmeow/dispatcher.go` +
`dispatcher_test.go`:
- Confirmar se o drop policy é "drop-oldest" ou "drop-newest" (afeta
  o test).
- Se flakejar por timing: usar `time.Sleep` mais generoso
  **ou** trocar para `clock.Clock` injetável (Fase 8 #97).
- Se for bug real: corrigir o dispatcher para honrar o buffer
  bounded sob carga.

**Arquivos a tocar:**

- `internal/adapter/provider/whatsmeow/dispatcher_test.go` — pode ser
  flake, ajustar timings
- `internal/adapter/provider/whatsmeow/dispatcher.go` — se for bug,
  corrigir

**Severidade:** Média — bloqueia `make test` (não só test-integration).

---

### Impacto no DoD da Fase 8

D3 e D4 **bloqueiam** os itens:
> `make test-integration` verde (4 cenários de chaos + cold-boot N=20,50,100)
> `make test` verde (`-race -shuffle=on -count=1`)

Devem ser corrigidos em issues dedicadas (#103, #104) **antes** dos novos
testes de chaos (#101) e cold-boot (#102) serem mergeados, pois esses
testes rodam em cima de `make test-integration` que precisa estar 100%
verde.

---

## 8. Pós-Fase 8 (release 1.0)

Fase 8 fecha o escopo 1.0 do `mez-go-mono`. Pós-1.0 (não nesta fase):

- **Multi-process / sharding por tenant** (substituir `ws.Hub` in-memory por
  Redis fan-out, `Manager` por `Manager[shardID]`). **Reescrita**, não
  incremento.
- **Zero-downtime deploy** com session store em S3 + handover de client
  whatsmeow (pode exigir mudança no formato do session store).
- **Métricas avançadas de boot** (SLO de p95 boot time, alerta de boot > 60s).
- **Chaos em produção** (Gremlin / Litmus) com blast radius por tenant.
- **KMS externo** (`VaultTransitSealer` plugado via `port.Sealer`).
- **Readiness probe em k8s** com `tcpSocket` + `httpGet /readyz` para
  orquestração avançada.

---

## 9. Estimativa final

| Categoria | LOC | Dias |
|---|---:|---:|
| **NEW** (Runner + Hub.Shutdown + serve.go + chaos + cold-boot) | ~1.600 | 2.4 |
| **REWRITE** (wire.go + entrypoint.sh) | ~400 | 0.7 |
| **MECHANICAL** (goleak + config) | ~250 | 0.3 |
| **Buffer** (20% para chaos flakejar + cold-boot timing) | — | 0.6 |
| **Total** | **~2.250** | **~4.0** |

Cabe no envelope 3-4 dias do README §23 com 0.5d de folga. Pior caso
(N=100 cold-boot flakeyar + chaos timer issues): 4.5d, ainda dentro do 5d
realista. Fase 8 é o **último bloco de feature do 1.0**; após merge, o
projeto atinge o DoD §24 e pode fazer o release 1.0 archive do `mez-go` pai.

---

## 10. Próximos passos

1. **Aprovação do plano.** Se aprovado, criar issues #96–#102 com labels
   `fase8` + `priority/critical` (boot/shutdown é blocker para 1.0).
2. **Sequência sugerida:**
   `#96` (Runner) → `#97` (wire) → `#98` (Hub) → `#100` (goleak) →
   `#99` (migrate) → `#101` (chaos) → `#102` (cold-boot).
3. **Gating:** nenhum PR de Fase 9+ deve ser mergeado sem Fase 8 fechada,
   porque os novos packages vão depender de `pkg/lifecycle.Runner` para
   shutdown limpo.
4. **PR único:** `Fase 8: …` em `fase8-squash` → `main`.
5. **Comunicação:** anunciar no `mez-go` que a Fase 8 é a última migração;
   após merge + smoke test, o `mez-go` entra em modo **archive**.

---

*Este plano é vivo: revisitar ao fim da fase e ajustar prazos/complexidade
com base no que o código ensina. Referências cruzadas: `README.md` §3 (C1–C12),
§5 (D1–D18), §6 (garantias), §7 (bus), §20 (operação), §21 (riscos),
§23 (roadmap), §24 (DoD).*
