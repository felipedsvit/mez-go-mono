# ADR 0003 — D2b: Reconciler (C1) cobre o gap entre D2 e o downstream

* **Status:** Aceita (criada na revisão C1/C2)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D2b](../../README.md#5-decisões-arquiteturais)

## Contexto

D2 (ADR 0002) garante que a **linha** da mensagem é durável antes do 2xx.
Mas há um gap entre "INSERT feito" e "mensagem roteada/enviada":

1. Crash do processo entre INSERT e PUBLISH no bus → evento fica no DB,
   ninguém processa.
2. Bus satura (PublishInbound drop-safe, ADR sobre C2) e o handler
   silenciosamente descarta o evento após o 2xx.
3. Outbox relay não chegou a drenar (shutdown abrupto, OOM, kill -9).

Em qualquer desses casos, a linha existe mas o processamento downstream
não acontece — a mensagem é "zumbi": persistida, não roteada, não entregue.

Sem um mecanismo de recuperação, esses zumbis se acumulam e o operador
precisa rodar SQL ad-hoc.

## Decisão

Adotamos um **Reconciler** que varre a tabela `messages` em busca de linhas
com `status = 'received'` e as reprocessa. Roda em dois momentos:

- **No boot**, antes do `serve` aceitar tráfego HTTP. Garante recuperação
  após crash abrupto.
- **Periodicamente** a cada 30s (configurável via `MEZ_RECONCILE_INTERVAL`).

A query usa `FOR UPDATE SKIP LOCKED` para permitir múltiplas instâncias
do reconciler em cenários de scaling (Fase 8+), e batch size
configurável (`MEZ_RECONCILE_BATCH`, default 100).

## Consequências

### Positivas

- **Auto-cura:** o sistema se recupera de crash, drop de bus, e OOM sem
  intervenção manual. O operador pode confiar que "mensagem com status
  received há mais de X segundos" é sintoma de bug, não de falha esperada.
- **Backpressure natural:** se o reconciler está sempre cheio (lote
  inteiro a cada 30s), o operador é alertado via métrica e log — sinal
  claro de que algo upstream está atrasado.
- **Custo baixo:** query indexada (`status = 'received' ORDER BY created_at
  LIMIT N FOR UPDATE SKIP LOCKED`), executada a cada 30s. Em sistema
  saudável, retorna 0 linhas.

### Negativas

- **Latência de recovery:** se o processo crasha às 14:00:00 e o boot
  termina às 14:00:45, mensagens das 14:00:00–14:00:45 só são
  reprocessadas após o boot (não instantaneamente). Aceitável — o
  provedor já re-tenta após timeout de webhook.
- **Duplicação de processamento em crash no meio do reconcile:** se o
  reconciler crasha após o SELECT mas antes do commit do
  roteamento, a próxima passagem pega as mesmas linhas. Mitigado por
  idempotência no destino (UPSERT em `conversations`, `messages`
  re-INERT é no-op via dedup).
- **Conflito com relay em janelas longas:** se o relay está processando
  um lote grande e o reconciler entra ao mesmo tempo, o
  `SKIP LOCKED` evita contenção mas pode atrasar ambos. Aceitável
  dado o batch size pequeno.

## Notas de implementação

Arquivos relevantes:

- `internal/usecase/reconcile/reconciler.go` — loop principal
- `internal/adapter/repository/postgres/repository.go:318` —
  `MessageRepo.SelectUnroutedMessages` (a query `FOR UPDATE SKIP LOCKED`)
- `cmd/server/wire.go` — start/stop coordenado com graceful shutdown
- `pkg/config/config.go:60` — `reconcile_interval` e `reconcile_batch`
