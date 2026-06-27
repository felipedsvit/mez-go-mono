# ADR 0012 — D10: Graceful shutdown completo

* **Status:** Aceita (mantida + reforço)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D10](../../README.md#5-decisões-arquiteturais)

## Contexto

Single-process significa que o processo é o **toda a topologia**.
Crash durante o shutdown deixa estado inconsistente:

- HTTP server fecha com conexões half-open → cliente recebe
  connection reset.
- Bus drena em background mas goroutine pendurada segura ref →
  leak de memória.
- Outbox relay interrompe mid-batch → linhas em `status='pending'`
  ficam no DB sem quem processe (mitigado pelo Reconciler D2b).
- whatsmeow manager não fecha sockets → session store do
  whatsmeow pode corromper.
- DB pools fecham com transações abertas → ROLLBACK automático,
  mas as mutations em memória não commitam.

As alternativas:

1. **OS signal → os.Exit(0)** — instantâneo, mas vaza tudo.
2. **OS signal → orderly shutdown** com timeout — fecha cada
   subsistema em ordem.
3. **OS signal → dump-and-exit** — salva estado para debug
   depois. Útil em prod, mas adiciona complexidade.

## Decisão

Adotamos a opção 2 com timeout de 30s:

```
SIGTERM/SIGINT
  ↓
HTTP server.Shutdown(ctx)        ← para de aceitar; drena reqs in-flight
  ↓
Bus.Drain(ctx)                   ← drena event handlers com timeout
  ↓
Relay.Stop()                     ← termina loop do outbox poller
  ↓
Reconciler.Stop()                ← termina loop do reconciler
  ↓
Whatsmeow manager.Disconnect(×N) ← fecha 1 client por tenant
  ↓
appPool.Close() / platformPool.Close()
  ↓
os.Exit(0)
```

O `runWithGracefulShutdown` em `cmd/server/wire.go:350-409` é o
único entrypoint. Cada subsistema expõe `Stop()` / `Shutdown(ctx)`
/ `Drain(ctx)` idempotente (chamar 2x é no-op).

Logs estruturados em cada etapa com `level=info msg="shutdown: <step>"`.

## Consequências

### Positivas

- **Zero corrupção:** o whatsmeow fecha limpo, o session store
  em memória não vaza, o outbox não fica com linhas órfãs.
- **Cliente vê fim ordenado:** HTTP fecha primeiro → cliente
  recebe 503 rápido, não "connection reset".
- **Kubernetes-friendly:** `terminationGracePeriodSeconds: 35`
  cobre o timeout de 30s + 5s de margem.
- **Testável:** o teste de shutdown injeta signal fake e verifica
  que cada subsistema foi drenado.

### Negativas

- **30s de inatividade durante deploy:** se o operador faz
  rolling deploy, há uma janela de 30s em que o pod antigo ainda
  está finalizando. Aceitável — o rolling está configurado para
  esperar `terminationGracePeriodSeconds`.
- **Timeout estourado = abort:** se o relay não drena em 30s
  (ex.: canal Meta travado), o `os.Exit(0)` é chamado e o
  Reconciler (D2b) cobre na próxima inicialização.
- **Shutdown parcial em panic:** o `recover()` por goroutine
  (mitigação de C10) já trata panics, mas se o panic for no
  main thread, o OS mata o processo antes do shutdown coordenado.
  Mitigado por `defer` no topo do main.

## Notas de implementação

Arquivos relevantes:

- `cmd/server/wire.go:350-409` — `runWithGracefulShutdown`
- `internal/adapter/broker/bus.go` — `Bus.Drain(ctx)`
- `internal/usecase/outbox/relay.go` — `Relay.Stop()`
- `internal/usecase/reconcile/reconciler.go` — `Reconciler.Stop()`
- `internal/adapter/provider/whatsmeow/manager.go` —
  `Manager.Disconnect(ctx, tenantID)` por tenant
