# ADR 0004 — D3: Outbox + relay in-process com poll de fallback

* **Status:** Aceita (mantida + reforço)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D3](../../README.md#5-decisões-arquiteturais)

## Contexto

Mensagens outbound precisam ser entregues ao canal (Meta, Telegram,
whatsmeow) com pelo menos uma garantia de "vai tentar de novo se
falhar". As alternativas:

1. **Chamada síncrona no hot-path do API handler** — o handler bloqueia
   até o canal responder. Simples, mas degrada latência e amarra a
   disponibilidade do handler à disponibilidade do canal.
2. **Outbox em DB + relay in-process** — handler faz INSERT no outbox
   na mesma tx da mensagem; relay em goroutine faz SELECT periodicamente
   e despacha. Latência do handler é 1 INSERT; resiliência vem do
   polling.
3. **Fila externa (Redis, RabbitMQ, NATS)** — mesma semântica do outbox,
   mas com broker adicional. Aumenta dependências operacionais.

## Decisão

Adotamos a opção 2: **outbox em DB + relay in-process com poll**.

- O INSERT no outbox acontece **na mesma transação** que o INSERT na
  `messages`. Atomic: ou ambos ou nenhum.
- O relay é uma goroutine que faz `SELECT ... WHERE status = 'pending'
  FOR UPDATE SKIP LOCKED LIMIT N` a cada 5s (configurável via
  `MEZ_OUTBOX_POLL_INTERVAL`).
- O relay marca `status = 'sent'` após o canal confirmar; `failed`
  após erro; `dlq` após `MaxAttempts`.
- O relay também drena **no boot** o backlog acumulado durante downtime
  — sem isso, mensagens geradas durante crash ficariam penduradas.

## Consequências

### Positivas

- **Atomicidade transacional:** `outbox + messages` no mesmo `BEGIN/
  COMMIT`. Não há janela onde a mensagem é "existe no app mas não no
  canal".
- **Resiliência a crash:** kill -9 do processo → na próxima inicialização
  o relay drena o backlog. Sem perda silenciosa.
- **Backpressure explícita:** se o canal está lento, o relay acumula
  rows em `pending`. Operador vê via métrica `outbox_pending_count`.
- **Custo operacional baixo:** sem broker adicional. DB é fonte da
  verdade; relay é um loop simples.

### Negativas

- **Latência adicional:** o handler responde antes do canal receber
  (semântica assíncrona). Para casos que precisam de confirmação
  síncrona (ex.: "enviei e quero saber o provider_msg_id"), o caller
  precisa consultar `messages.provider_msg_id` após o relay processar.
- **Polling consome CPU mesmo sem trabalho:** mesmo em sistema
  ocioso, o relay acorda a cada 5s. Aceitável — o `WHERE status =
  'pending'` retorna 0 rows em ms e o índice é barato.
- **Bloat da tabela `outbound_events`:** mensagens com `status = 'sent'`
  ficam na tabela até job de cleanup. Mitigado por vacuum
  periódico (Postgres) e possível TTL pós-1.0.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/repository/postgres/outbox.go` — `OutboxRepo` (Insert,
  ClaimNext, MarkSent, MarkFailed, MarkDLQ)
- `internal/usecase/outbox/relay.go` — loop principal com backoff
- `internal/usecase/messaging/ingestor.go` — INSERT no outbox na mesma
  tx da mensagem
- `cmd/server/wire.go:226` — inicialização do relay com PollInterval
  e BatchSize
