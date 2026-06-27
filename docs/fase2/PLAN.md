# Fase 2 — Pipeline inbound

> **Status:** planejamento aprovado (junho/2026).
> **Escopo:** 12 issues (#34–#45) · ~8.5 dias estimados · single PR (squash) `fase2-squash` → `main`.
> **Pré-requisitos:** Fase 0 (merged em PR #18) e Fase 1 (merged em PR #19) completas.
> **Base de reuso:** `github.com/felipedsvit/mez-go` — 51.815 LOC Go, 1.905 LOC SQL, **maior parte do código da Fase 2 tem precedente direto no pai**.

---

## 1. Análise do projeto pai (mez-go)

A Fase 2 do `mez-go-mono` **não é greenfield** — ~90% do código tem precedente
em `mez-go`. O pai já tem o pipeline inbound funcional (NATS-based); o
`mez-go-mono` o **re-escreve** trocando o broker (NATS → bus in-process
tipado) e simplificando a multi-binário para um único processo.

### 1.1 Inventário de código reusável (porte mecânico)

| Componente do pai | Caminho (`/home/user/felipedsvit/mez-go/...`) | LOC | Issue destino | Tipo de porte |
|---|---:|---:|---|---|
| Ingestor (resolveContact, resolveConversation) | `internal/usecase/messaging/ingest.go` + `resolve.go` | 188 | #36 | **mecânico** — trocar `domain.TenantID string` → nosso tipo; trocar `Contact.Identities` → nosso `ProviderID` |
| Ingestor test (fakes em memória) | `internal/usecase/messaging/ingest_test.go` | 144 | #36 | **mecânico** |
| Sender (Send, SendAction, publishWithRetry) | `internal/usecase/messaging/send.go` | 289 | Phase 3 (Fase 3) | **reescrita** — Fase 2 só usa o port; o Sender concreto é Fase 3 |
| Outbox Relay (loop de 5s + ProcessOne) | `internal/outbox/relay.go` | 117 | #38 | **mecânico** — Publisher interface vira `Sender interface` |
| Outbox Relay test | `internal/outbox/relay_test.go` | 181 | #38 | **mecânico** |
| OutboxWriteRepo + OutboxRelayRepo (Postgres) | `internal/adapter/repository/postgres/outbox.go` | 171 | #35 | **adaptar** — mez-go usa SECURITY DEFINER; mez-go-mono usa `mez_platform` (BYPASSRLS) via `RunAsPlatform` |
| Outbox integration test | `internal/adapter/repository/postgres/outbox_integration_test.go` | 308 | #44 | **mecânico** |
| MessageRepo.Insert com dedup ON CONFLICT | `internal/adapter/repository/postgres/repository.go:317-355` | 39 | #35 (já existe) | **revisar** — mez-go-mono já tem, mas falta `text_enc` (Phase 7) |
| ContactRepo.Upsert + FindByIdentity | `internal/adapter/repository/postgres/repository.go:131-220` | 90 | #35 (já existe) | **mecânico** — mez-go-mono tem `ProviderID` único, não `[]ChannelIdentity` |
| ConversationRepo.Upsert + FindByPeer + GetByIDForUpdate | `internal/adapter/repository/postgres/repository.go:225-318` | 93 | #35 (já existe) | **mecânico** — `DeriveConversationKey` é o mesmo |
| handleMetaWebhook + validMetaSignature (HMAC-SHA256) | `internal/transport/http/webhook_meta.go` | 163 | #40 | **mecânico** — manter `maxWebhookBody=2MiB`, fail-closed sem app secret |
| handleMessengerWebhook | `internal/transport/http/webhook_messenger.go` | 82 | #40 (merge com Meta) | **mecânico** |
| webhook_meta_test (HMAC sign) | `internal/transport/http/webhook_meta_test.go` | 145 | #40, #44 | **mecânico** |
| waba.ParseWebhook (mapper WABA → canônico) | `internal/adapter/provider/waba/mapper.go:96-200` | 104 | #40 | **adaptar** — mesmo padrão, nosso domínio |
| instagram.ParseWebhook | `internal/adapter/provider/instagram/mapper.go` | — | #40 | **mecânico** (mesma forma) |
| messenger.ParseWebhook + ParseResult (LifecycleEvents, OTN) | `internal/adapter/provider/messenger/mapper.go` | — | #40 | **mecânico** (estende para incluir OTN) |
| tgbot.WebhookSecret (X-Telegram-Bot-Api-Secret-Token) | `internal/adapter/provider/tgbot/webhook_secret.go` | 30 | #41 | **mecânico** — `subtle.ConstantTimeCompare` |
| tgbot.toInbound (Telegram → canônico) | `internal/adapter/provider/tgbot/mapper.go:21-50` | 30 | #41 | **mecânico** |
| Auth middleware (HS256 + claims) | `internal/transport/http/middleware/auth.go` | 127 | #42 | **reusar** da Fase 1 |
| Auth middleware test (incl. MustGenerateToken) | `internal/transport/http/middleware/auth_test.go` | 77 | #42, #44 | **reusar** da Fase 1 |
| OIDC middleware (TokenVerifier, claim tenant) | `internal/transport/http/middleware/oidc.go` | 51 | #42 | **reusar** da Fase 1 |
| RateLimit token-bucket per IP | `internal/transport/http/middleware/ratelimit.go` | 132 | #42 | **reusar** da Fase 1 (ratelimit.New já existe) |
| RequireScope (RBAC) | `internal/transport/http/middleware/require_scope.go` | 57 | #42 | **mecânico** (novo — mez-go-mono não tem ApiKey) |
| api_messages.go (List/Thread/Send/Assign/Resolve) | `internal/transport/http/api_messages.go` | 306 | #42 | **adaptar** — Send fica 501 em Phase 2 (real é Phase 3) |
| api_messages_test (integration, mock) | `internal/transport/http/api_messages_test.go` | 844 | #42, #44 | **adaptar** (sem NATS) |
| api_gdpr.go, api_search.go, api_spec.go | `internal/transport/http/api_*.go` | 183 | Phase 5+ | **fora do escopo** da Fase 2 |
| server.go (Handler, chi routes, middleware composition) | `internal/transport/http/server.go` | 315 | #42 | **reescrita** — sem NATS, sem sinks, sem WS, sem ApiKey |
| inbox.Service (ListConversations, Thread, Assign, Resolve) | `internal/usecase/inbox/inbox.go` | 87 | #42 | **mecânico** — mesma interface |
| router.Router (Assign/Unassign/Resolve/GetByIDForUpdate) | `internal/usecase/routing/routing.go` | 242 | #37 | **simplificar** — sem ACD/sticky em Phase 2; só `defaultAgentID` |
| outbox.go migration (outbox table + SECURITY DEFINER fns) | `migrations/0003_outbox_fks_indexes.up.sql` | 133 | #34 (parcial) | **adaptar** — outbox já existe em 0001; falta DEFERRABLE FKs |
| outbox_skip_locked migration (FOR UPDATE SKIP LOCKED) | `migrations/0023_outbox_skip_locked.up.sql` | 28 | #38 (resolver) | **adaptar** — mez-go-mono usa `mez_platform` em vez de SECURITY DEFINER |
| pgtest.Start (testcontainers helper) | `internal/testutil/pgtest/pgtest.go` | 250 | #44 | **portar** — mez-go-mono tem `tests/platform/run_as_platform_test.go` que já usa testcontainers |
| SignMetaHMAC + MustGenerateToken | `internal/testutil/auth.go` | 50 | #44 | **mecânico** |
| openapi.yaml (paths /webhooks/*, /api/v1/*) | `api/openapi.yaml` | 930 | #45 | **portar e estender** — base sólida |
| tokens.go (senderRegistry) | `cmd/mez-core/tokens.go` | 81 | Phase 3 | **fora** — Phase 2 não tem tokens ainda |
| mez-core main.go (wiring) | `cmd/mez-core/main.go:182-280` | 100 | #43 | **reescrita** — wireServices determinístico |

**LOC reusáveis (porte mecânico):** ~2.500 LOC Go + ~1.900 LOC SQL + ~930 LOC YAML.
**LOC genuinamente novos:** ~600 LOC (reconciler #39, sendCtx decoupling, MTLS app secret lookup).

### 1.2 Patterns obrigatórios (do pai, mantidos em mez-go-mono)

Do `mez-go/AGENTS.md`:

1. **Multi-tenant via RLS, não filtragem app-side.** TenantID via `context.Context`
   populado por `RunInTenantTx` (que faz `set_config('mez.tenant_id', _, is_local=true)`).
   Repos nunca recebem `TenantID` como parâmetro. **Já em vigor** no mez-go-mono.

2. **Dedup atômico** via `ON CONFLICT (tenant_id, channel, provider_message_id)
   WHERE provider_message_id <> ''`. **Já em vigor** no mez-go-mono.

3. **Outbox pattern** (transacional): entrada de outbox escrita **na mesma tx** que
   persiste a mensagem; relay drena. Adaptar para mez-go-mono: o relay itera
   por `RunAsPlatform` (tenant em `mez_platform` pool) em vez de SECURITY DEFINER.

4. **Channel capability negotiation**: `port.Channel` adapters expõem
   `Capabilities()` em runtime; sender cai media→text se canal não carrega.
   **Já em vigor** no mez-go-mono (CapabilitySet + Resolver).

5. **Functional options** (`WithX`, `WithY`) para deps opcionais. **Seguir em #36, #37, #38.**

6. **golang-migrate** embedded como library em `cmd/server migrate`. **Já em vigor.**

7. **OpenAPI 3.1 + oapi-codegen**, CI valida diff. **Já em vigor** (parcialmente — #45 estende).

8. **whatsmeow client is not goroutine-safe** — não tocar (Phase 4).

9. **Graceful shutdown** SIGINT/SIGTERM → `Disconnect()`. **Aplicar em #43.**

10. **Bounded buffers** entre whatsmeow socket e dispatch. **Não toca** (Phase 4).

11. **`pgx` stdlib driver** para whatsmeow session store. **Não toca** (Phase 4).

12. **zerolog structured logging.** **Já em vigor.**

13. **Comentários em português** (novos arquivos podem misturar; preferir PT
    para consistência com o pai e a equipe brasileira).

### 1.3 Divergências arquiteturais entre pai e mez-go-mono

| Aspecto | mez-go (pai) | mez-go-mono | Impacto na Fase 2 |
|---|---|---|---|
| Broker | NATS JetStream (cross-process) | Bus in-process tipado (channels Go) | `ConsumeInbound` → `bus.SubscribeInbound(handler)` (já em #36) |
| Multi-tenant isolation | 1 role (`mez_app`) + SECURITY DEFINER functions | 3 roles (`mez_migrate`/`mez_app`/`mez_platform`) | Outbox relay #38 usa `RunAsPlatform` (mez_platform, BYPASSRLS) em vez de SECURITY DEFINER |
| Multi-binário | 6 binários (`mez-core`, `mez-worker-whatsmeow`, ...) | 1 binário (`cmd/server`) | Wire em `cmd/server/main.go` é o único wiring point |
| Outbox security | SECURITY DEFINER functions em DB | `RunAsPlatform` no Go (tenant_id no contexto, mez_platform pool) | `OutboxRelayRepo` precisa de método `ForEachTenant(mez_platform_pool)` |
| Conversation ID | `DeriveConversationKey(tenant, channel, peer)` (sha256) | mesmo | herdar |
| Status | `StatusQueued/Sent/Delivered/Read/Failed` | `MessageStatusReceived/Routed/Notified` (Fase 2) | **incompatibilidade intencional** — mez-go-mono é inbound-focused, estados refletem o pipeline |
| Message encryption at rest | `text_enc BYTEA` (mig 0012) | `body TEXT` plain (Phase 7) | deferir para Phase 7 |
| ACD (routing) | Full ACD: queues, agents, skills, sticky, overflow, transbordo | Phase 2: só `defaultAgentID` por tenant | `routing.Router` super-simplificado |
| Sinks (opensearch, redis stream, webhook) | 3 sinks | Phase 5+ | não toca |
| OpenSearch search | habilitado | Phase 5+ | não toca |
| GDPR | endpoints `/api/v1/gdpr/*` | Phase 5+ | não toca |
| Channel Telegram | `go-telegram/bot` (long-poll) | webhook (Phase 2) | diferente — não herda long-poll; só o `mapper.go` e `webhook_secret.go` |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| Porte mecânico (~80% dos 12 issues) | ~2.500 | 4.0 |
| Reescrita parcial (reconciler, sendCtx, FKs deferíveis) | ~600 | 1.5 |
| Genuinamente novo (testes E2E, OpenAPI extension) | ~600 | 1.5 |
| Stacked commits + PR review + CI | — | 0.5 |
| Buffer (Phase 0/1 subestimaram em ~50%) | — | 1.0 |
| **Total** | **~3.700** | **~8.5** |

Mantém os 8.5d da estimativa inicial. O ganho do reuso é compensado pela
divergência de broker (NATS→bus in-process) e pelo groundwork de C6 (FKs
deferíveis) que não existia no pai.

---

## 2. Visão geral da Fase 2

Implementa o **pipeline inbound end-to-end**: webhook do provider (Meta/Telegram)
→ verificação de assinatura fail-closed → normalização para envelope canônico →
ingest (Contact + Conversation + Message em `RunInTenantTx` com dedup) → bus
in-process → routing (assign + `routed`) → reconciler que recupera mensagens
órfãs. Em paralelo, implementa a infraestrutura outbound (outbox + relay com
poll de fallback) e a API REST autenticada (Bearer JWT + cookie de sessão).

A Fase 2 **NÃO** implementa os adapters de envio dos canais (WABA/IG/MSG/TG
Send) — isso é Fase 3. O relay (#38) roda com `Sender` interface vazia em
Fase 2; o default é "no sender" → requeue, e os adapters reais plugam na
Fase 3 sem mudar a infraestrutura.

---

## 3. Correções arquiteturais cobertas

| Correção | Descrição | Issues |
|---|---|---|
| **C1** | Reconciler inbound (boot + 30s) que recupera mensagens em `received` órfãs | #39, #44 |
| **C2** | Bus drop-safe + reconciler (cobertura) | já coberto na Fase 0; #39 fecha o ciclo |
| **C3** | FORCE RLS (já na Fase 0) | herdado |
| **C4** | RLS fail-closed (já na Fase 0) | herdado; #36 valida em ingest |
| **C6** | FKs deferíveis (DEFERRABLE INITIALLY DEFERRED) | #34 (groundwork em Phase 2; restore completo é Fase 6) |
| **C12** | Boot determinístico e graceful shutdown coordenado | #43 |
| **D3** | Outbox + relay com sinal in-process + poll de fallback 5s | #35, #38, #44 |
| **D8** | Webhook signature verification fail-closed (HMAC-SHA256 Meta + Secret-Token Telegram) | #40, #41, #44 |
| **D12** | OpenAPI 3.1 gerado por `oapi-codegen`, CI valida diff | #45 (resolve #32 carryover) |

---

## 4. Issues (12)

| # | Título | Camada | Esforço | Ref pai principal | Bloqueada por | Bloqueia |
|---|---|---|:--:|---|---|---|
| **#34** | `migrations/0003` — DEFERRABLE FKs (C6) + índices outbox/reconciler + `routed_at`/`notified_at` | migrations | 0.5d | `migrations/0003_outbox_fks_indexes.up.sql` (sem DEFERRABLE) | — | #35, #36 |
| **#35** | `internal/adapter/repository/postgres` — `OutboxRepo` + `InboundEventsRepo` + tenant list (`RunAsPlatform`) | adapter | 1d | `internal/adapter/repository/postgres/outbox.go` | #34 | #36, #37, #38, #39 |
| **#36** | `internal/usecase/messaging/ingest` — pipeline canônico (Contact→Conversation→Message) com dedup em `RunInTenantTx` | usecase | 0.5d | `internal/usecase/messaging/ingest.go` + `resolve.go` | #34, #35 | #40, #41 |
| **#37** | `internal/usecase/messaging/routing` — assign + `bus.SubscribeInbound` + marca `routed` (sem ACD) | usecase | 0.5d | `internal/usecase/routing/routing.go` (simplificado) | #35, #36 | #39 |
| **#38** | `internal/usecase/outbox/relay` — drain com sinal in-process + poll 5s (D3); `Sender` interface (Fase 3 implementa adapters) | usecase | 1d | `internal/outbox/relay.go` (adaptar) | #35 | #43 |
| **#39** | `internal/usecase/reconcile` — Reconciler no boot + 30s; `received → routed` para órfãs (C1) | usecase | 1d | **novo** (sem precedente; pattern do READme §6) | #35, #37 | #43, #44 |
| **#40** | `internal/adapter/webhook/meta` — webhook unificado WABA+IG+Messenger; HMAC-SHA256 (`X-Hub-Signature-256`) fail-closed (D8) | adapter | 1d | `internal/transport/http/webhook_meta.go` + `waba/mapper.go` + `instagram/mapper.go` + `messenger/mapper.go` | #36 | #42 |
| **#41** | `internal/adapter/webhook/telegram` — webhook com `X-Telegram-Bot-Api-Secret-Token` fail-closed (D8) | adapter | 0.5d | `internal/adapter/provider/tgbot/webhook_secret.go` + `mapper.go` (apenas toInbound) | #36 | #42 |
| **#42** | `internal/transport/http` — `/webhooks/*` + `/api/{conversations,messages}` + middleware Bearer JWT (claim tenant) + cookie session | transport | 1d | `internal/transport/http/api_messages.go` + `middleware/auth.go` + `server.go` | #40, #41, #38 | #43, #45 |
| **#43** | `cmd/server/main.go` (runServe) — wire ingestSvc+routingSvc+relay+reconciler; boot determinístico (C12); graceful shutdown ordenado | transport | 0.5d | `cmd/mez-core/main.go:180-280` (adaptar para mono) | #38, #39, #42 | #44 |
| **#44** | `tests/inbound` — testcontainers end-to-end (webhook→persist, dedup, reconciler recovery após `kill -9`, outbox poll fallback) | tests | 0.5d | `internal/testutil/pgtest/pgtest.go` + `internal/transport/http/api_messages_test.go` | todas anteriores | — |
| **#45** | `api/openapi.yaml` — `/webhooks/*` + `/conversations` + `/messages` + `bearerAuth` (resolve issue #32 carryover) | docs | 0.5d | `api/openapi.yaml` (porte) | #42 | — |

**Total:** ~8.5 dias (com buffer).

---

## 5. Ordem de execução

A ordem de implementação segue a coluna "Bloqueada por" do grafo acima:

1. **#34** migrations/0003 (foundation)
2. **#35** repos postgres (depende de #34)
3. **#36** ingest service (depende de #34, #35) — em paralelo com:
4. **#37** routing service (depende de #35, #36) — em paralelo com:
5. **#38** outbox relay (depende de #35)
6. **#39** reconciler (depende de #35, #37) — **novo código**
7. **#40** webhook Meta (depende de #36) — em paralelo com:
8. **#41** webhook Telegram (depende de #36)
9. **#42** transport/http (depende de #40, #41, #38)
10. **#43** wire main.go (depende de #38, #39, #42)
11. **#44** tests E2E (depende de todas)
12. **#45** OpenAPI (depende de #42)

---

## 6. Stacked commits (estratégia de squash)

A branch `fase2` recebe 4 stacked commits intermediários + 1 squash:

1. `feat(fase2): migrations/0003 + adapter repos (outbox/inbound_events)` — issues #34, #35.
2. `feat(fase2): ingest + routing + reconciler + relay (pipeline inbound)` — issues #36, #37, #39, #38.
3. `feat(fase2): webhooks (Meta + Telegram) + transport/http` — issues #40, #41, #42.
4. `feat(fase2): wire main.go + tests E2E + OpenAPI` — issues #43, #44, #45.

Depois, squash em um único commit na branch `fase2-squash` com mensagem
detalhada. PR de `fase2-squash` → `main`.

---

## 7. Definition of Done (subset da Fase 2)

Do README §24, os itens cobertos por esta fase:

- [x] `make build` verde em CI.
- [x] `make test` verde em CI (`-race` + `-shuffle=on`).
- [ ] `docker compose up` sobe postgres + minio + app (já herdado da Fase 0).
- [ ] `curl /health` retorna 200; `curl /readyz` retorna 200 (herdado).
- [ ] **Reconciler recupera mensagens órfãs após `kill -9` (C1)** — #39 + #44.
- [ ] **Outbox drena no boot via poll de fallback (D3)** — #38 + #44.
- [ ] Webhook Meta + Telegram recebem payload e persistem mensagem (fail-closed) — #40, #41, #44.
- [ ] API REST: GET /conversations, GET /messages, POST /messages — #42, #45.
- [ ] OpenAPI spec bate com handlers (CI valida) — #45.
- [ ] Documentação atualizada — este arquivo.

---

## 8. Riscos e mitigações específicas da Fase 2

| Risco | Mitigação |
|---|---|
| FKs deferíveis (C6) são breaking change para testes RLS da Fase 0 | Testes passam porque deferrable é STRICT no commit; validar em #34 antes de mergear |
| Reload do sender registry é complexo se Fase 3 quiser hot-reload | Sender interface é imutável após boot; reload fica pós-1.0 |
| Reconciler pode race com o routing consumer se ambos pegarem a mesma msg | Reconciler usa `FOR UPDATE SKIP LOCKED` no `SelectUnroutedMessages`; routing consumer também |
| HMAC-SHA256 com segredo em memória (decifrado) é vetor de dump | Segredo só fica em memória durante o request (defer zero); mitigação completa é Vault Transit (pós-1.0) |
| OpenAPI gerado com `oapi-codegen` pode divergir da spec | CI step `openapi-validate` que faz `git diff --exit-code api/openapi.gen.go` |
| **Outbox relay sem SECURITY DEFINER**: precisa iterar por tenant em mez_platform (cross-tenant) | `RunAsPlatform` por tenant — vai serializar 1 query por tenant em vez de 1 cross-tenant; aceitável para 100s de tenants (1ms × 100 = 100ms/tick) |
| **Ingestor chama `i.autoAssign` FORA da tx** (padrão do pai) — race com routing service? | `autoAssign` abre sua própria tx + FOR UPDATE (D11 do pai); idempotente. Validar em #36. |
| **Comentários em inglês** vs **português** (padrão do pai) | Novo código: preferir português para consistência com a base herdada. |

---

## 9. Carryover para fases seguintes

- **Fase 3** (outbound): implementa os `Sender` adapters (WABA/IG/MSG/TG/WhatsMeow). O `SenderRegistry` de #38 é o ponto de plug. Pattern do pai: `tokens.go:senderRegistry` é o port.
- **Fase 4** (WhatsMeow): o `whatsmeow` adapter é o Sender mais complexo (dispatcher, recover, session store).
- **Fase 5** (painel): usa o cookie session middleware de #42 e o `GET /api/conversations` para popular a inbox. Adicionar o full ACD (queues, skills, sticky) ao `routing.Router` de #37.
- **Fase 6** (backup): usa as FKs deferíveis de #34 (C6 groundwork) e o replay de migrations (C7).
- **Fase 7** (hardening): adiciona rotação de JWT keys, Vault Transit Sealer, e `MessageRepo` encryption (mig 0012 do pai).
- **Fase 8** (estabilização): boot determinístico de #43 é a base; Fase 8 adiciona chaos tests e warm-up paralelo de whatsmeow.

---

## 10. Referências cruzadas

- `mez-go/AGENTS.md` — rules de arquitetura e pitfalls.
- `mez-go/CLAUDE.md` — ensaio arquitetural completo.
- `mez-go/docs/canais/README.md` — capability matrix.
- `mez-go/docs/adr/0001-event-topology.md` — por que os subjects são shaped assim.
- `mez-go/internal/core/event/event.go` — envelope canônico (template para `mez-go-mono`).
- `mez-go/api/openapi.yaml` — spec existente (fonte para #45).
- `mez-go/internal/testutil/pgtest/pgtest.go` — testcontainers helper (fonte para #44).
- `mez-go/internal/transport/http/middleware/auth.go` — JWT pattern (fonte para #42).
- `mez-go/internal/transport/http/webhook_meta.go` — HMAC verification (fonte para #40).
- `mez-go/internal/usecase/messaging/ingest.go` — Ingestor (fonte para #36).
- `mez-go/internal/outbox/relay.go` — Relay (fonte para #38).
- README do mez-go-mono §3 (C1-C12), §6 (pipeline), §23 (Fase 2), §24 (DoD).
