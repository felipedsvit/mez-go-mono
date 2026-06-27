# ADR 0021 — D12b / C12: Boot determinístico + shutdown coordenado (Fase 8)

> **Status:** Aceito (junho/2026) · owners: mez-go-mono core · tracking em
> `fase8-tracking` (issue #101, #102).
> **Contexto:** mono-binário precisa de coordenação cross-subsystem para
> que SIGTERM/SIGKILL não deixe recursos em estado inconsistente.

## Contexto

O mez-go (pai) era composto por 6 binários independentes. Cada um
tinha seu próprio `main()`, signal handler e supervisor externo.
Não havia coordenação entre os processos — um crash do `mez-core`
afetava o `mez-worker-whatsmeow` mas os supervisors
(SystemD, k8s) resolviam o problema reiniciando containers.

No mono (`mez-go-mono`), **um único processo** segura HTTP, bus,
reconciler, relay, outbox, status consumer, todos os adapters
de canal e o `Manager` whatsmeow. Sem coordenação explícita, um
SIGTERM durante o boot ou shutdown pode:

- Derrubar goroutines long-running em estado inconsistente
- Não drenar in-flight HTTP requests (cliente recebe conexão cortada)
- Não drenar o bus in-process (mensagens publicadas durante shutdown
  são perdidas)
- Não desconectar clients whatsmeow (próximo boot vê "session taken")

## Decisão

Adotar **`pkg/lifecycle.Runner`** (#96) como coordenador central de
boot/shutdown, com **phases explícitas** (#97), **`Hub.Shutdown`**
ordenada (#98) e **`MigrateOnBoot`** no binário (#101).

### Boot determinístico por fases

A boot é uma sequência nomeada de phases:

1. `config` (validação)
2. `sealer` (LocalSealer)
3. `pools` (app + platform)
4. `txrunner`
5. `repos`
6. `bus` (consumers iniciados)
7. `ingestor_router`
8. `relay` (Run via `runner.Run`)
9. `reconciler` (Run via `runner.Run`)
10. `status_consumer`
11. `whatsmeow` (Manager; lazy init)
12. `webhook` (Meta + Telegram)
13. `adminweb`
14. `http` (ListenAndServe via `runner.Run`)

Cada phase tem **Start** (síncrono, idempotente) e **Stop** (síncrono,
idempotente). Falha em qualquer phase aborta o boot e chama
**Shutdown parcial** (phases já iniciadas em LIFO).

### Shutdown coordenado em LIFO

SIGTERM/SIGINT → `runner.Shutdown(ctx)` itera phases em LIFO
com timeout por phase (default 5s). Cada phase com `Stop` recebe
um ctx isolado. Erros são logados mas não interrompem.

Ordem de shutdown (LIFO):
- `http` (para de aceitar requests; drena in-flight)
- `bus` (Bus.Drain)
- `whatsmeow` (Manager.DisconnectAll)
- `status_consumer` (Unsubscribe)
- `reconciler` (Stop)
- `relay` (Stop)
- `ingestor_router` (Unsubscribe)
- ... demais phases com Stop

Pools (`appPool`, `platformPool`) são fechados **depois** de
`runner.Shutdown()` retornar.

### `pkg/lifecycle.Runner`

API canônica:
```go
runner := lifecycle.NewRunner(log, metrics.NewRunnerSink(metricsReg))
runner.AddPhase(lifecycle.Phase{Name: "bus", Start: ..., Stop: ...})
runner.Run(ctx, "relay", relay.Run)        // goroutine long-running
if err := runner.Boot(ctx); err != nil { /* fail-closed */ }
if err := runner.Shutdown(ctx); err != nil { /* logged, continues */ }
if err := runner.Wait(ctx); err != nil { /* wait for goroutines */ }
```

Princípios:
- `Boot` e `Shutdown` retornam erro mas não interrompem (Boot aborta
  em falha; Shutdown continua tentando).
- `Run` encapsula `wg.Add(1) + defer wg.Done() + recover() (C10)`.
- `Wait` bloqueia até todas as goroutines `Run`-style terminarem.
- Métricas: `boot_phase_info{phase=...}`, histogramas de duração.

### `MigrateOnBoot` no binário

O subcomando `serve` agora roda `migrations up` antes de subir o
processo (config `MEZ_MIGRATE_ON_SERVE=true` por padrão). Fail-closed:
se migrate falhar, o container não inicia. `entrypoint.sh` reduzido a
`exec mez-go-mono serve`.

Vantagens:
- Sem dependência da CLI externa `migrate` no container runtime.
- Fail-closed nativo (vs. shell script que pode mascarar erro).
- Audit log de `boot_migration` no D17 (best-effort).

## Consequências

### Positivas

- **Coordenação explícita** entre subsistemas — ordem de boot/shutdown
  é determinística, visível em código e logada.
- **Recuperação de kill -9** validada por chaos tests (#106).
- **Drenagem de HTTP in-flight** sob SIGTERM (clientes recebem 2xx).
- **`recover()` por goroutine long-running** (relay, reconciler, HTTP)
  via `Runner.Run` — C10 honrado.
- **`goleak.VerifyTestMain`** em 7 pacotes críticos (#102) fecha a
  porta a goroutine leaks que esta fase expõe.
- **Métricas de boot/shutdown** visíveis em `/metrics` (Fase 8+).

### Negativas

- Acoplamento: `pkg/lifecycle` é usado por todos os subsistemas.
  Risco de "feature flag" no Runner. Mitigado por testes
  extensivos (#96) e API minimal.
- Overhead de abstração: ~50 LOC por phase (vs. inline). Aceitável
  para 14 phases.
- Shutdown parcial em caso de falha de boot: phases que não
  subiram não têm `Stop` chamado (correto), mas o caller precisa
  entender que `Boot` retornou erro.
- `migrate` no binário adiciona ~200ms ao boot (acceptable em
  SIGTERM/SIGINT cycles). Se necessário, `MEZ_MIGRATE_ON_SERVE=false`.

### Trade-offs aceitos

- **Zero-downtime deploy** NÃO implementado (decidido em §20 do
  README). Releases têm janela de indisponibilidade.
- **Multi-process / sharding** por tenant NÃO implementado
  (decidido em §25). Limitação assumida do 1.0.
- **Vault Transit sealer** NÃO implementado (decidido em §22). 1.0
  usa apenas LocalSealer.

## Alternativas consideradas

### 1. Cada subsistema tem signal handler próprio

- ❌ Race entre handlers; ordem de shutdown imprevisível.
- ❌ Duplicação de código (signal handling em 6 lugares).

### 2. Sem coordenação (status quo pré-Fase 8)

- ❌ kill -9 durante boot deixa pools abertos, sessions stuck.
- ❌ SIGTERM não drena HTTP in-flight.

### 3. Coordinator via channel (sem lifecycle.Runner)

- ❌ Mais complexo, sem recover() integrado.

## Implementação

- `pkg/lifecycle/{phase,runner,runner_test}.go` (NOVO, ~500 LOC)
- `pkg/metrics/sink.go` (NOVO, adapter)
- `pkg/metrics/metrics.go` (+5 métricas)
- `cmd/server/serve.go` (NOVO, refatora `main.go`)
- `cmd/server/wire.go` (REWRITE, mantém wireServices; adiciona phases)
- `internal/transport/websocket/hub.go` (+Hub.Shutdown, +closed atomic)
- `internal/adapter/broker/bus.go` (+UnsubscribeInbound, +UnsubscribeStatus)
- `internal/usecase/messaging/status.go` (+Unsubscribe)
- 7 packages: `+goleak.VerifyTestMain`
- `tests/chaos/` (NOVO, 4 cenários kill -9)
- `tests/boot/` (NOVO, cold-boot N=20,50,100 + warmup + bench)
- `migrations/0005_backup_gucs.up.sql` (REWRITE non-transactional)
- `deployments/entrypoint.sh` (reduzido a 4 linhas)
- 3 test helpers em `tests/{inbound,backup,secrets}` (workaround D3)

## Validação

- [ ] `make build` verde.
- [ ] `make test` verde (`-race -shuffle=on`).
- [ ] `make test-integration` verde (chaos 4/4 + cold-boot 3/3).
- [ ] `goleak.VerifyTestMain` ativo em 7 pacotes.
- [ ] Boot time (N=100): p95 < 30s.
- [ ] `/readyz` retorna 200 somente após PhaseHTTP concluída.
- [ ] SIGTERM com HTTP in-flight: cliente recebe 2xx.
- [ ] SIGKILL entre reconciler.Run e Tick: próxima instância drena.
- [ ] SIGKILL entre outbox.Notify e drain: próxima instância drena.
- [ ] Panic em `whatsmeow.Adapter.Send` NÃO derruba processo.

## Referências

- README §20 (operação em produção)
- README §21 (riscos C10)
- README §23 (Fase 8)
- README §24 (Definition of Done)
- `docs/fase8/PLAN.md` §4
