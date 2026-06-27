# ADR 0010 — D8: Webhook Meta unificado com HMAC fail-closed

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D8](../../README.md#5-decisões-arquiteturais)

## Contexto

Meta (Facebook) expõe webhooks para WhatsApp Business, Instagram e
Messenger. A verificação de autenticidade é via header
`X-Hub-Signature-256` (HMAC-SHA256 do body, chave = app secret).

As alternativas:

1. **Verificação opcional** — handler aceita requests sem
   assinatura. Erro: expõe endpoint público a abuse (inundar de
   mensagens fake).
2. **Verificação obrigatória (fail-closed)** — handler rejeita
   (401) qualquer request com assinatura inválida ou ausente.
   Erro: se a Meta mudar o esquema (rolling deploy), todas as
   mensagens caem.
3. **Verificação com bypass em dev** — flag `MEZ_META_WEBHOOK_INSECURE`
   para ambiente de teste. Default: fail-closed.

## Decisão

Adotamos a opção 2 com a flag da opção 3 (apenas para dev/CI):

- **Produção (default):** verificação HMAC obrigatória.
  - `X-Hub-Signature-256` ausente → 401.
  - `X-Hub-Signature-256` presente mas não bate com
    `HMAC-SHA256(secret, body)` → 401.
  - HMAC válido mas o app_id/phone_number_id não corresponde a
    nenhum tenant cadastrado → 404 (não vaza "este app existe").
- **Dev/CI:** `MEZ_META_WEBHOOK_INSECURE=true` permite requests
  sem assinatura, mas loga warning. **Nunca** setar em prod.

O handler é **unificado** entre WABA, Instagram e Messenger: o
mesmo `meta.Handler` decide qual evento é baseado no
`object` field do payload (`whatsapp_business_account` vs
`instagram` vs `page`).

## Consequências

### Positivas

- **Sem abuse:** requests não-Meta não conseguem injetar mensagens.
  Aceita-se a perda zero vs risco de impersonation.
- **Auditoria por hash:** o handler loga o `X-Hub-Signature-256`
  truncado, permitindo correlacionar eventos com logs do Meta
  Business Manager.
- **Single code path:** 1 handler, 1 HMAC check, 3 canais.
  Manutenção simples.
- **Failure mode óbvio:** se a Meta rotacionar o app secret, o
  operador vê `401` no log imediatamente. Não há "evento
  processado mas ignorado".

### Negativas

- **Replay attack possível:** o HMAC não inclui timestamp; um
  atacante que capture um request válido pode re-enviá-lo.
  Mitigado pelo dedup atômico (`provider_msg_id UNIQUE`): a
  segunda inserção vira no-op.
- **Skew de secret entre Meta e mono:** se o operador rotaciona
  o secret no Meta Business Manager mas não atualiza o env var,
  todos os webhooks falham. Mitigado por `MEZ_META_APP_SECRET_FILE`
  (reload) e alerta via métrica `meta_webhook_auth_failures_total`.
- **Testes E2E precisam de secret conhecido:** o test
  `webhook_test.go` gera um secret aleatório e assina o body
  no test. Padrão mantido.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/webhook/meta/handler.go` — `verifyHMAC`,
  routing por `object`
- `internal/adapter/webhook/secrets/credentials.go` (carryover
  Fase 3, removido na Fase 7 em favor de Keyring DB-backed) —
  carrega `MEZ_META_APP_SECRET`
- `cmd/server/wire.go:254-260` — wiring do `meta.New(...)` com
  secrets
- `internal/adapter/webhook/meta/handler_test.go` — testes de
  HMAC válido/inválido/ausente
