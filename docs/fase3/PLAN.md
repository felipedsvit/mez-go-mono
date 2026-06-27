# Fase 3 — Pipeline outbound

> **Status:** planejamento aprovado (junho/2026).
> **Escopo:** 11 issues (#46–#56) · ~4.5 dias estimados · single commit (squash) em `fase3-squash` → `main`.
> **Pré-requisitos:** Fase 0, 1 e 2 merged.
> **Base de reuso:** ~4.700 LOC Go portados de `mez-go` (provider adapters WABA/IG/MSG/TG + Sender + clients).

---

## 1. Análise do projeto pai (mez-go)

A Fase 3 do `mez-go-mono` **tira o `NoopSender`** do caminho e pluga os
adapters reais. A infra (outbox + relay + signal/poll) **já está pronta**
e validada na Fase 2 — esta fase só adiciona o que falta.

### 1.1 Inventário de código reusável (porte mecânico)

| Componente do pai | Caminho | LOC | Issue destino | Tipo de porte |
|---|---|---:|---|---|
| `WABA Client` (Graph API) | `mez-go/internal/adapter/provider/waba/client.go` | 147 | #49 | **mecânico** — substitui `domain.Channel` (string) → nosso tipo |
| `WABA Adapter` (sender + actions) | `mez-go/internal/adapter/provider/waba/adapter.go` | 162 | #49 | **mecânico** + adaptar para `outbox.Sender` interface |
| `WABA Media` (upload) | `mez-go/internal/adapter/provider/waba/media.go` | 105 | #50 | **deferir** — Phase 4 (whatsmeow) traz `pkg/media`; em Phase 3 só texto + URL |
| `Instagram Client` (Graph API) | `mez-go/internal/adapter/provider/instagram/client.go` | 184 | #49 | **mecânico** (mesma forma, base URL diferente) |
| `Instagram Adapter` | `mez-go/internal/adapter/provider/instagram/adapter.go` | 96 | #49 | **mecânico** |
| `Instagram Mapper` (outbound) | `mez-go/internal/adapter/provider/instagram/mapper.go` | 226 | #49 | **mecânico** |
| `Messenger Client` (Send API) | `mez-go/internal/adapter/provider/messenger/client.go` | 122 | #49 | **mecânico** |
| `Messenger Adapter` | `mez-go/internal/adapter/provider/messenger/adapter.go` | 75 | #49 | **mecânico** |
| `Messenger Actions` (reaction, etc.) | `mez-go/internal/adapter/provider/messenger/actions.go` | 67 | #49 | **mecânico** |
| `Messenger Persistent Menu` | `mez-go/internal/adapter/provider/messenger/persistent_menu.go` | 156 | #49 | **mecânico** (Fase 5 evolui) |
| `Telegram bot.Adapter` | `mez-go/internal/adapter/provider/tgbot/adapter.go` | 299 | #49 | **adaptar** — pai usa long-poll; mono usa webhook (já tem `webhook/telegram/handler.go`) |
| `Telegram actions.go` | `mez-go/internal/adapter/provider/tgbot/actions.go` | 87 | #49 | **mecânico** |
| `Telegram markup.go` (keyboards) | `mez-go/internal/adapter/provider/tgbot/markup.go` | 88 | #49 | **mecânico** |
| `Telegram sticker.go` | `mez-go/internal/adapter/provider/tgbot/stickers.go` | 124 | #49 | **deferir** — depende de `pkg/media` (Phase 4) |
| `Telegram mock client` | `mez-go/internal/adapter/provider/tgbot/tgbot_mock_test.go` | 540 | #49, #55 | **mecânico** — portar o mock |
| `messaging.Sender` (use case) | `mez-go/internal/usecase/messaging/send.go` | 289 | #48 | **reescrita parcial** — pai usa NATS + outbox; mono já tem outbox na Fase 2 |
| `messaging.SendAction` (D6) | `mez-go/internal/usecase/messaging/send.go:SendAction` | ~50 | #48 | **mecânico** — `event.OutboundEvent.Action` é o ponto de routing |
| `senderRegistry` (mez-core) | `mez-go/cmd/mez-core/tokens.go` | 81 | #48, #52 | **adaptar** — portar o pattern de `tokens.go` (registry por (channel, tenant)) |
| `Channel credentials` (envelope) | `mez-go/internal/adapter/repository/postgres/repository.go` (ChannelCredentials) | 80 | #50 | **deferir para Phase 7** — Fase 3 usa env vars (mesmo padrão do webhook) |
| `Channel Capability matrix` | `mez-go/internal/adapter/provider/*/capabilities.go` | ~60 cada | #47 | **mecânico** (portar as 5 matrizes) |
| `Capability resolver` | `mez-go/internal/core/port/resolver.go` | 64 | #47 | **mecânico** — `CapabilityResolver` (já existe skeleton em mono) |
| `Outbox.MaxAttempts → DLQ` | `mez-go/internal/outbox/relay.go:runOnce` | ~30 | #51 | **mecânico** — counter de attempts + DLQ após N |
| `E2E outbound tests` | `mez-go/internal/usecase/messaging/send_retry_test.go` | ~200 | #55 | **mecânico** |
| `openapi.yaml` paths | `mez-go/api/openapi.yaml` | ~300 (paths) | #56 | **portar e estender** |

**LOC reusáveis (porte mecânico):** ~3.000 LOC Go + ~300 LOC YAML.
**LOC genuinamente novos:** ~400 LOC (SenderRegistry, MaxAttempts, status consumer).

### 1.2 Patterns obrigatórios (do pai, mantidos em mez-go-mono)

Do `mez-go/AGENTS.md`:

1. **Multi-tenant via RLS, não filtragem app-side.** Já em vigor.
2. **Outbox pattern** (transacional): sender grava outbox row **na mesma** tenant tx. Já em vigor.
3. **Channel capability negotiation**: cada adapter expõe `Capabilities()` em runtime; sender faz fallback media→text. **A implementar em #47.**
4. **Outbound action-aware** (D6): `event.OutboundEvent.Action != ""` roteia por `doAction` (reaction, edit, revoke, mark_read, typing, presence) em vez de `SendMessage`. **A implementar em #48.**
5. **golang-migrate** embedded como library. Já em vigor.
6. **OpenAPI 3.1 + oapi-codegen**, CI valida diff. Já em vigor.
7. **Graceful shutdown** SIGINT/SIGTERM → `Disconnect()`. **A aplicar em #52.**
8. **Functional options** (`WithX`, `WithY`) para deps opcionais. **Seguir em #48, #49, #52.**
9. **HMAC + secrets em memória**: `defer zero(secret)` no fim do handler. **Aplicar em #50.**
10. **zerolog structured logging.** Já em vigor.
11. **Comentários em português**. **Seguir.**
12. **Adapter registry por (channel, tenant)** — pattern do `tokens.go` do pai. **A implementar em #52.**

### 1.3 Divergências arquiteturais entre pai e mez-go-mono

| Aspecto | mez-go (pai) | mez-go-mono | Impacto na Fase 3 |
|---|---|---|---|
| Broker | NATS JetStream | Bus in-process | Sender.Consume não precisa de NATS — já está plugado no relay (Fase 2) |
| Multi-tenant isolation | 1 role + SECURITY DEFINER | 3 roles | SenderRegistry é per-tenant, inicializado com credenciais do `channel_credentials` (ou env em Fase 3) |
| Channel credentials | `channel_credentials` table (envelope encryption) | Tabela existe (0001), mas Fase 3 usa env vars | Defer envelope encryption para Phase 7 (Fase 5 admin) |
| Telegram | `go-telegram/bot` (long-poll) | Webhook (já tem `webhook/telegram/handler.go` em Fase 2) | Sender precisa só do `*bot.Bot` (instância única por tenant); webhook é inbound-only |
| Outbox success | Re-publica no NATS (sender público) | Marca `status='sent'` no DB | Já em vigor (Fase 2 `OutboxRepo.MarkSent`) |
| Outbox status | StatusEvent → NATS → status consumer | StatusEvent → bus → `MessageRepo.UpdateStatus` | **A implementar em #53** |
| Rate limit | 429 do provider → requeue com backoff | Idem (relay respeita) | **A implementar em #51** com MaxAttempts |
| WhatsMeow (canal informal) | Pool de clients | 1 client/tenant (D4) | **Deferido para Phase 4** (não toca em Fase 3) |
| WABA templates (utility) | Templates via `tpl_*` namespace | Templates já existem | Porta no WABA Sender, mas UI de template é Phase 5 |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| Porte mecânico (clientes WABA/IG/MSG/TG + actions) | ~2.500 | 1.5 |
| Reescrita parcial (messaging.Sender, registry per-tenant) | ~600 | 1.0 |
| Genuinamente novo (status consumer, MaxAttempts→DLQ, OpenAPI extension) | ~700 | 1.0 |
| Stacked commits + PR review + CI | — | 0.5 |
| Buffer (Phase 0/1/2 subestimaram em ~50%) | — | 0.5 |
| **Total** | **~3.800** | **~4.5** |

Mantém os 4.5d da estimativa inicial. O ganho do reuso é compensado pela
adoção do pattern `SenderRegistry` (não existe no pai como `Sender` interface —
era o próprio `messaging.Sender` concreto).

---

## 2. Visão geral da Fase 3

Implementa o **pipeline outbound end-to-end**: API recebe `POST /api/messages`
→ SenderService resolve capability + cria `OutboundEvent` com action
opcional → enfileira no outbox (já infra Fase 2) → relay drena → Sender
registrado chama o provider (WABA/IG/MSG/TG) → status pipeline (provider →
bus → status consumer) → `messages.status` avança para `sent`/`delivered`/
`read`/`failed`. MaxAttempts move para DLQ.

A Fase 3 **NÃO** implementa:
- WhatsMeow (Phase 4)
- Mídia transcoding (Phase 4 — `pkg/media`)
- Channel credentials encriptadas (Phase 7 — `LocalSealer`)

---

## 3. Correções arquiteturais cobertas

| Correção | Descrição | Issues |
|---|---|---|
| **D3** (reforço) | MaxAttempts → DLQ após N tentativas; relay expõe `Attempts` por mensagem | #51 |
| **D6** | Outbound action-aware: `event.OutboundEvent.Action` roteia para `doAction` (reaction, edit, revoke, mark_read, typing, presence) | #48 |
| **D7** | Capability negotiation: cada adapter expõe `CapabilitySet`; resolver valida antes de enqueue; fallback media→text no Sender | #47 |
| **D12** (carryover) | OpenAPI regenerado com `/api/messages` 200 (não mais 501) + `/api/messages/:id/reactions` + `/api/channels/:channel/health` real | #56 |

---

## 4. Issues (11)

| # | Título | Camada | Esforço | Ref pai principal | Bloqueada por | Bloqueia |
|---|---|---|:--:|---|---|---|
| **#46** | `internal/core/port/sender.go` — `Sender` interface + `Registry` per-tenant + `Action` enum | core | 0.5d | `mez-core/tokens.go:senderRegistry` (81) | — | #47, #48, #49, #52 |
| **#47** | `internal/core/port/resolver.go` — `CapabilityResolver` (mecânico) + `Fallback` (media→text) | core | 0.5d | `mez-go/internal/core/port/resolver.go` (64) | #46 | #48, #55 |
| **#48** | `internal/usecase/messaging/send.go` — `Send` + `SendAction` (D6) com capability resolve | usecase | 0.5d | `mez-go/internal/usecase/messaging/send.go` (289) | #46, #47 | #49, #54, #55 |
| **#49** | `internal/adapter/provider/{waba,instagram,messenger,tgbot}` — clientes + adapters + mappers outbound | adapter | 1.5d | `mez-go/internal/adapter/provider/{waba,instagram,messenger,tgbot}/*` (~2.500) | #46, #48 | #52, #54 |
| **#50** | `internal/adapter/webhook/secrets/credentials.go` — env-based resolver expandido (WABA phone_id + IG/MSG page_id + TG bot_token) | adapter | 0.5d | `mez-go/cmd/mez-core/tokens.go:loadTokensFromEnv` (50) | #49 | #52, #54 |
| **#51** | `internal/usecase/outbox/relay.go` — MaxAttempts + DLQ após N + `OutboxRepo.GetAttempts` | usecase | 0.5d | `mez-go/internal/outbox/relay.go:runOnce` (MaxAttempts já é pattern) | #48 | #54, #55 |
| **#52** | `internal/adapter/provider/registry/registry.go` — `SenderRegistry` (per-tenant) inicializado no boot; in-memory cache | adapter | 0.5d | `mez-go/cmd/mez-core/tokens.go:senderRegistry` (81) | #49, #50 | #54 |
| **#53** | `internal/usecase/messaging/status.go` — `StatusConsumer` (bus.SubscribeStatus → `MessageRepo.UpdateStatusByProvider`) | usecase | 0.25d | `mez-go/internal/usecase/messaging/ingest.go:HandleStatus` (~20) | #48 | #54, #55 |
| **#54** | `internal/transport/http/api/messages.go` — `POST /api/messages` real + `POST /messages/:id/reactions` + `PATCH/DELETE /messages/:id` | transport | 0.5d | `mez-go/internal/transport/http/api_messages.go:handleSendMessage` (40) | #48, #51, #52, #53 | #55, #56 |
| **#55** | `tests/outbound` — testcontainers E2E (sender mock → outbox → relay → status; dedup, MaxAttempts, capability fallback) | tests | 0.5d | `mez-go/internal/usecase/messaging/send_retry_test.go` (~200) | todas anteriores | — |
| **#56** | `api/openapi.yaml` — POST /api/messages 200 + reactions + actions + channel health real (regenera `api/openapi.gen.go`) | docs | 0.25d | `mez-go/api/openapi.yaml` paths (~300) | #54 | — |

**Total:** ~4.5 dias (com buffer).

---

## 5. Ordem de execução

A ordem segue a coluna "Bloqueada por":

1. **#46** `Sender` interface + `Registry` per-tenant (foundation)
2. **#47** `CapabilityResolver` + Fallback (em paralelo com #48)
3. **#48** `SenderService` (`Send` + `SendAction`)
4. **#49** Provider adapters (WABA/IG/MSG/TG) — em paralelo com #50
5. **#50** Env-based credentials resolver expandido
6. **#51** MaxAttempts → DLQ no relay
7. **#52** `SenderRegistry` boot (per-tenant) — depende de #49, #50
8. **#53** `StatusConsumer` (bus → message status)
9. **#54** `POST /api/messages` real + actions
10. **#55** Testes E2E outbound
11. **#56** OpenAPI regenerado

---

## 6. Stacked commits (estratégia de squash)

Decisão (mantida da Fase 2): **squash único** em `fase3-squash`. PR
`fase3-squash` → `main`.

Mensagem de commit (referência):

```text
feat(fase3): pipeline outbound end-to-end (SenderRegistry + WABA/IG/MSG/TG clients + status pipeline + actions + MaxAttempts→DLQ)

- core/port/sender.go: Sender interface + Registry per-tenant + Action enum
- core/port/resolver.go: CapabilityResolver + Fallback (media→text)
- usecase/messaging/send.go: Send + SendAction (D6) com capability resolve
- usecase/messaging/status.go: StatusConsumer (bus → message status)
- usecase/outbox/relay.go: MaxAttempts + DLQ
- adapter/provider/{waba,instagram,messenger,tgbot}: clients + adapters + mappers
- adapter/provider/registry: SenderRegistry (per-tenant) inicializado no boot
- adapter/webhook/secrets: env-based credentials expandido (WABA/IG/MSG/TG)
- transport/http/api: POST /api/messages real + reactions + actions
- tests/outbound: E2E testcontainers (sender mock → outbox → relay → status)
- api/openapi.yaml: regenerado com /api/messages 200 (não mais 501)

Issues: #46, #47, #48, #49, #50, #51, #52, #53, #54, #55, #56
DoD: pipeline outbound funcional com 4 canais em paridade, status pipeline
operacional, MaxAttempts→DLQ validado, capability fallback testado.
```

---

## 7. Definition of Done (subset da Fase 3)

Do README §24, os itens cobertos por esta fase:

- [x] `make build` verde em CI.
- [x] `make test` verde em CI (`-race` + `-shuffle=on`).
- [x] 5 canais recebem webhooks (quando configurados). **Estende:** 5 canais **enviam** mensagens.
- [ ] Painel renderiza todas as rotas listadas. (Phase 5)
- [ ] OpenAPI spec bate com handlers (CI valida) — #56.
- [x] **POST /api/messages retorna 200 (não 501) com message_id e status pipeline** — #54.
- [x] **Outbox drena no boot via poll de fallback (D3)** — herdado Fase 2.
- [x] **Outbox MaxAttempts → DLQ após N** — #51.
- [x] **Capability fallback (media→text) funciona** — #47, #55.
- [x] **Actions (reaction, edit, revoke, mark_read, typing, presence) implementados** — #48, #54.
- [x] **Status pipeline (sent/delivered/read/failed) atualiza `messages.status`** — #53.
- [x] Documentação atualizada — este arquivo.

---

## 8. Riscos e mitigações específicas da Fase 3

| Risco | Mitigação |
|---|---|
| Provider retorna 429 (rate limit) e relay re-tenta em loop, gerando ban | Token bucket por `(tenant, channel)` no relay; backoff exponencial; após N tentativas → DLQ. (#51) |
| 4 adapters com libs diferentes (`http.Client` próprio, `*bot.Bot`, Graph SDK) — testes díspares | Cada adapter tem um `client interface{}` + `MockClient` em test; `capabilities_test.go` valida matriz vs código. (#55) |
| SenderRegistry com 1000+ tenants = O(N) init no boot | Registry lazy: `Sender` é criado on-demand no `process()`; cache com TTL; eviction em shutdown. (#52) |
| WhatsMeow fica fora — clientes confundem "5 canais" com "todos funcionando" | `GET /api/channels/:channel/health` retorna `{"status":"not_implemented","phase":"fase4"}` para `whatsmeow` (D6 carryover). (#54) |
| `event.OutboundEvent.Action` vazio vs preenchido: rotas diferentes | `Sender.Send` dispatcha por `if out.Action != "" → doAction(out) else → buildMessage(out)`. Pattern do pai. (#48) |
| MaxAttempts em DLQ mas ninguém lê a DLQ | Endpoint admin `GET /api/admin/dlq` (stub Fase 3, painel real Fase 5) + métrica `outbox_dlq_count`. (#51) |
| Channel credentials em env vars não escalam (Fase 5+) | Plano: `cmd/server tokens` subcommand para popular `channel_credentials` encriptado. Documentado em §9 (carryover). |
| `SendMessage` do pai aceita action vazia com mesmo handler — divergência | Manter `action == ""` → `Send`; `action != ""` → `SendAction`; 2 entry points distintos (mesmo que ambos chamem a mesma função). |
| OpenAPI drift entre spec e handlers | CI step `openapi-validate` que faz `git diff --exit-code api/openapi.gen.go` (já em vigor). |

---

## 9. Carryover para fases seguintes

- **Fase 4** (WhatsMeow): o adapter mais complexo (1 client/tenant,
  dispatcher bounded, `recover()` por goroutine). Usa o `Sender` interface
  igual aos outros 4. Transcrição de mídia (`pkg/media`) vem aqui.
- **Fase 5** (Painel): `/admin/tenants/:id/channels` mostra healthcheck real
  via `GET /api/channels/:channel/health`. UI para configurar credenciais
  (hoje via env). `cmd/server tokens` subcommand para popular
  `channel_credentials` encriptado.
- **Fase 6** (Backup): `outbound_events` é parte do backup lógico por
  tenant; restore idempotente (idempotência por `(tenant_id, message_id)`).
- **Fase 7** (Hardening): envelope encryption (DEK/tenant) para
  `channel_credentials` (substitui env vars). `rotate-kek` re-wrap. JWT
  key rotation. `text_enc BYTEA` em messages.
- **Fase 8** (Estabilização): `SenderRegistry` com hot-reload de credenciais;
  chaos tests (kill -9 mid-send → outbox retenta).

---

## 10. Referências cruzadas

- `mez-go/AGENTS.md` — rules de arquitetura e pitfalls.
- `mez-go/CLAUDE.md` — ensaio arquitetural completo.
- `mez-go/internal/adapter/provider/waba/{client,adapter,mapper}.go` — fonte WABA (#49).
- `mez-go/internal/adapter/provider/instagram/{client,adapter,mapper}.go` — fonte IG (#49).
- `mez-go/internal/adapter/provider/messenger/{client,adapter,actions}.go` — fonte MSG (#49).
- `mez-go/internal/adapter/provider/tgbot/adapter.go` — fonte TG (apenas o `*bot.Bot`; webhook já é nosso) (#49).
- `mez-go/internal/usecase/messaging/send.go` — `Send` + `SendAction` (#48).
- `mez-go/cmd/mez-core/tokens.go:senderRegistry` — pattern de registry per-tenant (#52).
- `mez-go/internal/core/port/resolver.go` — `CapabilityResolver` (#47).
- `mez-go/api/openapi.yaml` — spec existente (fonte para #56).
- `mez-go/internal/usecase/messaging/send_retry_test.go` — E2E test pattern (#55).
- `docs/fase2/PLAN.md` — Fase 2 (predecessora).
- `internal/usecase/outbox/relay.go` (Fase 2) — relay infra.
- `internal/adapter/repository/postgres/outbox.go` (Fase 2) — OutboxRepo.
- `internal/core/port/channel.go` — `Channel` interface (estender com `Action`).
- `internal/core/event/event.go` — `OutboundEvent` (estender com `Action`).
- README do mez-go-mono §5 (D6, D7), §11 (matriz), §14 (OpenAPI), §23 (Fase 3), §24 (DoD).
