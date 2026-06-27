# ADR 0001 — D1: Bus in-process tipado

* **Status:** Aceita (mantida desde o pai `mez-go`)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D1](../../README.md#5-decisões-arquiteturais)

## Contexto

O pai `mez-go` tinha dois candidatos para comunicação in-process: NATS JetStream
(broker externo, multi-binário) e um bus in-process. A Fase 1 do mono já havia
optado pelo bus in-process pela topologia single-process; restava a forma.

As alternativas:

1. **Bus baseado em `interface{}` / `any`** com tópicos string — flexível, mas
   todo handler precisa de type assertion, e erros de tipo só aparecem em
   runtime.
2. **Bus tipado por evento** — cada evento é um tipo Go concreto, e a API do
   bus expõe `PublishInbound(event.InboundEvent)`, `PublishOutbound(event.OutboundEvent)`,
   `SubscribeInbound(func(event.InboundEvent))`, etc. Sem type assertion, sem `any`.

## Decisão

Adotamos o **bus tipado por evento** (opção 2). A interface `broker.Bus` expõe
métodos concretos por tipo de evento; subscribers registram callbacks
`func(event.InboundEvent)` em vez de `func(any)`.

## Consequências

### Positivas

- **Type-safety em compile-time:** se um subscriber espera `event.InboundEvent`
  e alguém publica outro tipo, o código não compila. Não é possível haver
  drift silencioso entre publisher e subscriber.
- **Zero overhead de runtime:** sem boxing/unboxing, sem reflection, sem
  type assertion. O hot-path de webhook → ingestor é CPU-puro.
- **IDE-friendly:** "Find Usages" sobre `PublishInbound` lista todos os
  publishers; "Find Implementations" sobre `event.InboundEvent` lista os
  tipos que fluem pelo barramento.
- **Documentação por tipo:** cada evento carrega em sua struct os campos
  relevantes, e o godoc renderiza a interface completa do bus.

### Negativas

- **Adicionar evento novo é mais caro:** requer (a) criar o tipo,
  (b) adicionar método no Bus, (c) possivelmente adicionar consumer no
  relay. Com `any` seria só publicar uma struct nova. Aceitável dado o
  volume baixo de tipos de evento (4 hoje: Inbound/Outbound/Status/Lifecycle).
- **Acoplamento leve ao pacote `event`:** todos os producers e consumers
  importam `internal/core/event`. Concentrado em um único pacote, fácil
  de manter.
- **Refatoração de assinatura é breaking:** renomear `event.InboundEvent`
  toca todos os arquivos do bus. Mitigação: `gopls rename` faz isso
  automaticamente.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/broker/bus.go` — `Bus.PublishInbound/SubscribeInbound/...`
- `internal/core/event/event.go` — `InboundEvent`, `OutboundEvent`,
  `StatusEvent`, `LifecycleEvent`
- `internal/core/port/channel.go` — `InboundSink`, `OutboundPublisher`
  (interfaces consumidas pelos providers)
