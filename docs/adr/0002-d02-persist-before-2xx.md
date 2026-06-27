# ADR 0002 — D2: Inbound durável antes do ack

* **Status:** Aceita (mantida desde o pai `mez-go`)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D2](../../README.md#5-decisões-arquiteturais)

## Contexto

Webhooks de provedores (Meta, Telegram) operam em modo ack/resposta: o
servidor HTTP deve responder 2xx dentro de uma janela (5–30s) para que o
provedor não re-tente. Se o handler processa o evento (parse, dedup,
inserir mensagem, enfileirar no outbox) **antes** de responder 2xx, qualquer
crash no meio resulta em re-tentativa do provedor → mensagem duplicada.

A ordem inversa também é problemática: retornar 2xx **antes** de persistir
faz a Meta "achar" que recebeu, mas se o processo crashar entre 2xx e
INSERT, a mensagem é perdida em silêncio.

As alternativas:

1. **Persistir e retornar 2xx imediatamente** (ordem: INSERT → 2xx). Provider
   recebe sucesso quando o estado já está no DB. Re-tentativa do provedor
   vira no-op via dedup atômico.
2. **Retornar 2xx e processar em background** (ordem: 2xx → enqueue →
   process). Provider fica feliz, mas crash entre 2xx e enqueue perde a
   mensagem.
3. **Persistir + ack só após confirmação downstream** (ordem: INSERT → 2xx
   condicional). Acopla latência do provedor ao tempo total de processamento.

## Decisão

Adotamos a opção 1: **persistir (com dedup atômico) → retornar 2xx →
publicar no bus**. O 2xx é a confirmação de que a **linha** foi gravada,
não de que o processamento downstream completou.

## Consequências

### Positivas

- **Sem perda silenciosa:** o provedor só vê 2xx depois que a mensagem está
  no DB. Crash entre INSERT e 2xx → provedor re-tenta → `ON CONFLICT DO
  NOTHING` deduplica.
- **Re-tentativa segura:** provedores que re-tentam em caso de timeout/erro
  não causam duplicação visível (a coluna `provider_msg_id` é UNIQUE).
- **Latência previsível:** o handler HTTP responde em ~ms (1 INSERT + 1
  PUBLISH non-blocking). Não há espera por outbox, roteamento, ou ack do
  canal.

### Negativas

- **D2 não cobre o downstream:** persistir a linha não garante que o
  roteador ou o outbox processem. Mitigado pelo Reconciler (D2b, ADR 0003).
- **Custo de re-tentativa do provedor:** em cenário de crash em massa, o
  provedor re-envia o backlog. Aceitável — é o modelo de webhook padrão.
- **Latência do provedor ainda importa:** se o INSERT demora > janela
  (improvável para inserts simples), o provedor re-tenta. Mitigado por
  índices e por não fazer trabalho extra no handler.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/webhook/meta/handler.go` — verifica HMAC, parse, INSERT,
  2xx, PUBLISH
- `internal/adapter/webhook/telegram/handler.go` — mesma ordem
- `internal/usecase/messaging/ingestor.go` — `InsertMessage` com
  `ON CONFLICT (tenant_id, provider_msg_id) WHERE provider_msg_id IS NOT
  NULL DO NOTHING`
- `migrations/0001_init.up.sql:101` — constraint UNIQUE em
  `(tenant_id, provider_msg_id)`
