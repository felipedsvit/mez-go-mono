# Plano de Implementação: `mez-go-mono`

> **Status:** plano de implementação (junho/2026).
> **Fonte da verdade:** [`README.md`](../README.md) — quando este plano divergir do README, vale o README (e o código que o valida em CI).

---

## 0. Visão geral

**Objetivo:** Substituir `mez-go` (51.815 LOC, 388 arquivos Go, 6 binários) por **um único binário / uma única imagem Docker** que entrega 5 canais em paridade, RLS multi-tenant fail-closed, bus in-process tipado com reconciler, painel `templ+htmx`, API REST validada por OIDC, e backup/restore/reset lógico.

**Decisões confirmadas:**

| # | Decisão |
|---|---------|
| 1 | Repo novo, do zero. Nada de `cp -r mez-go`. |
| 2 | Reescrita a partir do zero, lendo o pai como **referência semântica** (não porte literal). |
| 3 | Três roles Postgres desde a migration 0001 (`mez_migrate` / `mez_app` / `mez_platform`). |
| 4 | Seguir o roadmap de 8 fases do README §23 integralmente. |

**Princípio invariante:** o README é a *fonte da verdade*. Onde houver divergência entre código e documento, vale o código (mas o código só pode divergir em matriz de capacidades, OpenAPI e RLS — exatamente os três validados em CI pelo README §25).

---

## 1. Topologia final (alvo)

```
┌────────────────────── mez-go-mono (1 binário) ──────────────────────┐
│                                                                       │
│  HTTP/WS (chi) ─► Auth middleware (OIDC + session + CSRF)             │
│       │                                                               │
│       ├─ /webhooks/{meta,telegram,messenger}  (verif. assinatura)     │
│       │                                                               │
│       ▼                                                               │
│  Bus tipado (channels Go) — inbound.* outbound.* status dlq          │
│       │                                                               │
│  ┌────┴────┬────────┬──────────┬──────────┐  WhatsMeow (1 client/     │
│  │ WABA    │ IG     │ Messenger│ TGBot    │  tenant, dispatcher +     │
│  │ statel. │ statel.│ statel.  │ statel.  │  recover() por goroutine) │
│  └─────────┴────────┴──────────┴──────────┘                           │
│       │                                                               │
│  Usecase: messaging · routing · outbox+relay · reconciler · auth     │
│       │                                                               │
│  PG (FORCE RLS) + S3/MinIO + LocalSealer (envelope)                  │
│                                                                       │
│  Painel templ+htmx: /setup · /login · /admin/* · /app/*               │
└───────────────────────────────────────────────────────────────────────┘
```

---

## 2. Estrutura de diretórios alvo (do README §16)

```
mez-go-mono/
├── cmd/server/                  # serve + migrate + setup + rotate-kek + wire
├── internal/
│   ├── core/                    # domain + event + port + Sealer (sem deps ext.)
│   ├── usecase/
│   │   ├── messaging/  routing/  outbox/
│   │   ├── reconcile/           # Reconciler inbound (C1)
│   │   ├── auth/  admin/  channels/  backup/
│   ├── adapter/
│   │   ├── broker/              # bus tipado + política de saturação (C2)
│   │   ├── repository/postgres/ # RunInTenantTx + RunAsPlatform (C5) + FORCE RLS (C3)
│   │   ├── storage/s3/  idp/oidc/  meta/
│   │   ├── crypto/              # LocalSealer envelope (C9)
│   │   └── provider/{waba,whatsmeow,instagram,messenger,tgbot}/
│   └── transport/{http,websocket,adminweb}/
├── pkg/{config,logger,media,crypto,health,metrics,qrcode}/
├── migrations/                  # 0001: FORCE RLS + 3 roles + FKs deferíveis
├── api/{openapi.yaml,openapi.gen.go}
├── deployments/{Dockerfile,docker-compose.yml,entrypoint.sh}
├── configs/  go.mod  Makefile  AGENTS.md  CLAUDE.md  README.md
└── tests/
```

---

## 3. Correções C1–C12 — onde cada uma vira código

| # | Onde mora no código | Localização |
|---|---------------------|-------------|
| **C1** Reconciler | `internal/usecase/reconcile/reconciler.go` + estado `received/routed/notified` em `messages.status` | migrations/0001 + reconcile/*.go |
| **C2** Política de saturação | `internal/adapter/broker/bus.go` — `PublishInbound` non-blocking drop-safe | broker/bus.go |
| **C3** FORCE RLS | migration 0001: `ALTER TABLE x FORCE ROW LEVEL SECURITY` em 8 tabelas | migrations/0001_init.up.sql |
| **C4** Fail-closed | policy usa `current_setting('mez.tenant_id', missing_ok := false)` | migrations/0001 |
| **C5** RunAsPlatform auditado | `RunAsPlatform(ctx, actor, fn)` em `repository/postgres/db.go` + `audit_log` | adapter/repository/postgres |
| **C6** FKs deferíveis | `ALTER TABLE messages ADD CONSTRAINT ... DEFERRABLE INITIALLY DEFERRED` | migrations/0001 |
| **C7** Replay de migrations no restore | `internal/usecase/backup/restore.go` lê `schema_version` do manifesto e aplica migrations sobre os dados antes do upsert | usecase/backup/ |
| **C8** Tx REPEATABLE READ + caveat bloat | `internal/usecase/backup/export.go` — `BeginTxFunc` com `IsoLevel=RepeatableRead` e comentário sobre VACUUM/bloat | usecase/backup/ |
| **C9** Envelope encryption local | `pkg/crypto/envelope.go` — KEK da env, DEK por tenant em `channel_credentials` | pkg/crypto/ |
| **C10** Riscos single-box | `cmd/server/wire.go` define ordem de boot determinística + graceful shutdown | cmd/server/ |
| **C11** Estimativas | phasing 8 fases (35-50 dias úteis) | este plano |
| **C12** Fase 8 estabilização | boot determinístico + shutdown coordenado + chaos test | Fase 8 |

---

## 4. Roadmap — 8 fases, com entregáveis verificáveis

### **Fase 0 — Esqueleto + bootstrap** (2-3 dias)

**Arquivos novos:**
- `go.mod` (`module github.com/felipedsvit/mez-go-mono`, go 1.22+)
- `cmd/server/{main,wire,setup,migrate,rotate_kek}.go`
- `internal/core/{domain,event,port}/` — base types (TenantID, Channel, MessageType, MessageStatus, Conversation, Message, Contact, etc.)
- `internal/core/event/event.go` — InboundEvent, OutboundEvent, StatusEvent, LifecycleEvent, DLQEvent (sem subjects NATS)
- `internal/core/port/` — TxRunner, repositórios, Channel, CapabilitySet, CapabilityResolver, OutboundPublisher, InboundSink, Keyring, Sealer
- `internal/adapter/broker/bus.go` — bus tipado com `PublishInbound` (non-blocking drop-safe) + métricas
- `internal/adapter/repository/postgres/{db,repository}.go` — `RunInTenantTx` + `RunAsPlatform` + pool
- `pkg/{config,logger,health,metrics,crypto}/` — bootstrap helpers
- `migrations/0001_init.up.sql` + `.down.sql` — 3 roles, 8 tabelas, FORCE RLS, FKs deferíveis, policies fail-closed, `messages.status` enum `received/routed/notified`
- `deployments/{Dockerfile,docker-compose.yml,entrypoint.sh}` — multi-stage com ffmpeg + libwebp + templ
- `Makefile` — tools/build/test/openapi-gen/lint/migrate-up
- `api/openapi.yaml` — esqueleto

**DoD da Fase 0:**
- `make build` verde.
- `make test` verde (`-race -shuffle=on`).
- `docker compose up` sobe postgres + minio + app.
- `curl /health` 200, `curl /readyz` 200.
- Wizard `/setup` cria admin global (Argon2id).
- Migração 0001 cria os 3 roles; SELECT cross-tenant sem `RunInTenantTx` falha (RLS fail-closed).
- Bus tipado compila com `PublishInbound` (drop-safe) e métrica `bus_dropped_total`.

**Fonte de referência no mez-go:** `pkg/{config,logger,health}`, `internal/core/{domain,event,port}`, `internal/adapter/broker/nats/broker.go` (apenas a *interface*; a implementação será in-process).

---

### **Fase 1 — Auth + admin global** (3-4 dias)

**Arquivos novos:**
- `internal/core/admin/` — `User`, `Role`, `Session`, `Password` (Argon2id), `Audit`
- `internal/adapter/auth/argon2/` — implementação Argon2id OWASP 2024
- `internal/adapter/idp/oidc/` — verifier JWKS (porta do `mez-go/internal/adapter/idp/oidc/verifier.go`)
- `internal/usecase/auth/` — login, logout, session
- `internal/usecase/admin/` — tenants, users, roles, audit, secrets
- `internal/adapter/repository/postgres/admin/{db,user_repo,role_repo,audit_repo}.go` — pool `mez_platform` para audit cross-tenant
- `internal/transport/adminweb/{server,handlers_auth,handlers_tenants,handlers_users,handlers_roles,handlers_audit}.go` — primeiro subconjunto
- `internal/transport/adminweb/templates/{base,login,dashboard,tenants,users,roles,audit}.html` — primeira leva de `html/template` (será reescrito em templ na Fase 5)
- `internal/transport/adminweb/{render,embed,page_data}.go` — renderizador + `//go:embed`
- `internal/transport/http/middleware/{auth,csrf,require_scope}.go`
- `api/openapi.yaml` — endpoints `/auth/oidc/login`, `/admin/*`
- `tests/auth/argon2_test.go`, `tests/rls/fail_closed_test.go` ← teste de regressão **C3/C4**

**DoD da Fase 1:**
- Login local admin (Argon2id) + cookie HttpOnly + CSRF middleware.
- OIDC JWKS verifier funcionando.
- `RunAsPlatform` auditado gera linha em `audit_log` em todo acesso cross-tenant.
- **Teste de regressão RLS fail-closed passa** (C3/C4).
- CRUD de tenants e users/roles funcional via painel.

**Fonte de referência no mez-go:** `cmd/mez-ui/main.go`, `internal/core/admin/`, `internal/adapter/auth/argon2/`, `internal/adapter/idp/oidc/verifier.go`, `internal/transport/adminweb/{server,handlers_*,embed,page_data}.go`, `internal/usecase/admin/{auth_login,auth_logout,session,tenants,users,roles,audit}.go`.

---

### **Fase 2 — Pipeline inbound + Reconciler** (4-5 dias, +1 do reconciler)

**Arquivos novos:**
- `internal/core/domain/{message,conversation,contact,tenant}.go` — tipos canônicos
- `internal/adapter/repository/postgres/{message,conversation,contact,tenant}_repo.go` — `RunInTenantTx` everywhere
- `internal/adapter/repository/postgres/outbox.go` — `OutboxWriteRepo` + `OutboxRelayRepo` (mantém interface do `mez-go`, mas sem NATS)
- `internal/usecase/messaging/ingest.go` — `Ingestor` (persist + dedup + 2xx + bus.PublishInbound)
- `internal/usecase/messaging/send.go` — `Sender` (capability resolve + outbox insert)
- `internal/usecase/routing/routing.go` — `Router` com ACD (sem Redis obrigatório)
- `internal/usecase/outbox/relay.go` — relay in-process com poll de fallback (D3)
- **`internal/usecase/reconcile/reconciler.go`** — Reconciler (C1) com `SelectUnroutedMessages` + `MarkRouted`
- `internal/transport/http/webhook_meta.go` + `webhook_telegram.go` + `webhook_messenger.go` — verificação `X-Hub-Signature-256`, fail-closed
- `internal/adapter/provider/{waba,instagram,messenger,tgbot}/{adapter,mapper,client,capabilities}.go` — ingestor + capability declaration
- `internal/transport/http/{api_messages,api_conversations}.go` — `GET /messages`, `GET /conversations`
- `migrations/0002_inbox.up.sql` — adiciona `messages.status` (`received/routed/notified`) + índice parcial
- `tests/inbox/integration_test.go` — testcontainers Postgres

**DoD da Fase 2:**
- Webhook Meta unificado (WABA + IG) recebe, valida assinatura, persiste, retorna 2xx, publica bus.
- WABA/IG/MSG/TG: mapper + ingestor funcional.
- Reconciler roda no boot + a cada 30s; `kill -9` durante handler → reconciler recupera.
- Outbox + relay com poll de 5s drena pendentes.
- API `GET /messages` e `GET /conversations` retornam dados escopados ao tenant.
- **Teste de regressão C1**: kill -9 entre COMMIT e bus.Publish → reconciler reprocessa.

**Fonte de referência no mez-go:** `internal/usecase/messaging/{ingest,send}.go`, `internal/usecase/routing/routing.go`, `internal/adapter/repository/postgres/{message,conversation,contact,outbox}_*.go`, `internal/transport/http/{webhook_meta,webhook_messenger,api_messages}.go`, `internal/adapter/provider/{waba,instagram,messenger,tgbot}/{adapter,mapper,client}.go`, `internal/outbox/outbox.go`.

---

### **Fase 3 — Pipeline outbound** (3-4 dias)

**Arquivos novos:**
- `internal/usecase/messaging/send.go` (completo) — `Sender.Send` com capability negotiation + fallback media→text
- `internal/adapter/provider/{waba,instagram,messenger,tgbot}/send.go` — clientes HTTP de cada provider
- `internal/adapter/provider/{waba,instagram,messenger,tgbot}/actions.go` — reaction/edit/revoke/mark_read/typing/presence
- `internal/transport/http/api_messages.go` — `POST /messages`, `PATCH /messages/:id`, `DELETE /messages/:id`, `POST /messages/:id/reactions`
- `internal/transport/websocket/hub.go` — fan-out por tenant (sem NATS)
- `internal/core/port/{resolver,capability}.go` — capability resolver com fallback
- Testes `capabilities_test.go` por adapter (matriz = código, validado em CI)

**DoD da Fase 3:**
- `POST /messages` insere no outbox + publica bus; consumer chama `provider.Send`.
- Capability negotiation: se canal não suporta, fallback media→text.
- Ações de canal (reaction/edit/revoke/mark_read/typing/presence) implementadas.
- WS Hub faz fan-out por tenant; cliente lento → drop do cliente (best-effort).
- Tabela de capacidades + teste em CI.

**Fonte de referência no mez-go:** `internal/usecase/messaging/send.go` (capability resolve), `internal/adapter/provider/{waba,instagram,messenger,tgbot}/send.go` (ações), `internal/core/port/capability.go` (CapabilityResolver), `internal/transport/websocket/hub.go`.

---

### **Fase 4 — WhatsMeow** (6-8 dias, realocado por complexidade)

**Arquivos novos:**
- `pkg/media/transcode.go` — ffmpeg/cwebp com semáforo (4 global)
- `internal/adapter/storage/s3/s3.go` — mídia
- `internal/adapter/provider/whatsmeow/{adapter,dispatcher,registry,capabilities,mapper,send,actions,events,reconnect,warmup,humanize,identity,error_filter,history,media,blocklist,disappearing,privacy,calls,newsletter,groups,profile,status,anti_ban}.go` — **1 client/tenant** + dispatcher bounded + `recover()` por goroutine (C10) + reconexão automática
- `internal/adapter/repository/postgres/{whatsapp_state,whatsapp_session_keys,history}.go` — session store em Postgres
- `internal/adapter/provider/whatsmeow/manager.go` — `Manager` (1 client por tenant) com circuit breaker
- `internal/transport/http/api_qrcode.go` — `GET /channels/whatsmeow/qrcode`
- `internal/transport/http/webhook_whatsmeow.go` — events do whatsmeow
- `internal/core/port/{capability,keyring}.go` — adaptação para envelope DEK/tenant
- `pkg/qrcode/qrcode.go` — geração PNG do QR

**DoD da Fase 4:**
- 1 client whatsmeow por tenant (D4).
- Dispatcher com buffers bounded + `recover()` por goroutine (C10): panic num tenant **não** derruba o processo.
- Send: text/image/audio/sticker/video; actions: reaction/edit/revoke/mark_read/typing/presence.
- AutoReconnect + graceful `Disconnect()` no shutdown.
- Session store em Postgres (migrations 0011+0013 do pai).
- QR-code PNG servido em `/channels/whatsmeow/qrcode`.
- **Teste C10**: panic injetado num dispatcher → recover() → outros tenants seguem normais.

**Fonte de referência no mez-go:** `internal/adapter/provider/whatsmeow/*` (2.815 LOC — maior bloco), `cmd/mez-worker-whatsmeow/main.go` (entrypoint, será colapsado), `pkg/media/transcode.go`.

**Atenção:** é a fase com mais **reescrita**, não porte (conforme §22 do README). Pool multi-tenant → 1 client/tenant; dispatcher com `recover()` é **novo** (C10).

---

### **Fase 5 — Painel completo + templ** (5-6 dias, realocado)

**Arquivos novos:**
- `internal/transport/adminweb/templates/*.templ` — **reescrita de `html/template` → `templ`**
- `internal/transport/adminweb/static/{app.css,htmx.min.js,sse.js,logo.*,favicon.ico}` — reuso do pai
- `internal/transport/adminweb/handlers_*.go` (refactor) — handlers tipados com `templ` em vez de string templates
- `internal/transport/adminweb/render/renderer.go` — gera Go a partir de `*.templ`
- `internal/transport/adminweb/middleware/csrf.go` — double-submit para todos POST/PUT/DELETE
- `internal/transport/http/api_*.go` (restantes) — `GET /conversations`, `GET /admin/services`, etc.
- Makefile alvo `templ generate` antes do `build`

**DoD da Fase 5:**
- Todas as rotas do README §15 renderizam: `/setup`, `/login`, `/admin/*` (10 rotas), `/app/*` (5 rotas).
- QR-code whatsmeow com refresh `hx-trigger="every 5s"`.
- WS real-time na inbox via htmx SSE extension.
- CSRF middleware em todos POST/PUT/DELETE.
- Painel funciona end-to-end: login → admin → tenant → channel → inbox.

**Fonte de referência no mez-go:** `internal/transport/adminweb/*` (3.729 LOC, **segundo maior bloco**). Lógica porta, mas `html/template` → `templ` é **reescrita de apresentação** (1.500-2.000 LOC) + re-cabeamento htmx/WS.

---

### **Fase 6 — Backup/Restore/Reset** (5-7 dias, realocado por complexidade)

**Arquivos novos:**
- `internal/usecase/backup/export.go` — `BeginTxFunc` com `IsoLevel=RepeatableRead`, `COPY (SELECT * FROM x WHERE tenant_id=$1) TO STDOUT` por tabela, `io.Pipe` para S3 multipart
- `internal/usecase/backup/restore.go` — replay de migrations (C7) + upsert topológico (C6) com FKs deferidas
- `internal/usecase/backup/reset.go` — `client.Disconnect()` + DELETE por tenant + delete S3 prefix
- `internal/usecase/backup/manifest.go` — JSON com `schema_version` + checksums
- `internal/transport/http/api_backup.go` — `POST /admin/tenants/:id/backup`, `/restore`, `/reset`
- `internal/transport/adminweb/handlers_backup.go` — UI com htmx polling de progresso
- `internal/adapter/storage/s3/multipart.go` — chunked upload via `io.Pipe`
- `migrations/0003_backup_metadata.up.sql` — tabela `backup_jobs` (id, status, size, ...)

**DoD da Fase 6:**
- Export lógico gera arquivo NDJSON + tar de mídia no bucket `mezgo-backups`.
- **Restore round-trip**: export → reset → restore → diff = vazio.
- Replay de migration: backup de schema v1 restaurado em DB v2 aplica migrations faltantes antes do upsert.
- Confirmação dupla "RESET" + senha admin funciona; whatsmeow desconectado antes do DELETE.
- Progresso visível no painel (htmx polling).

**Fonte de referência no mez-go:** **Não há precedente.** É o bloco mais novo. Apenas padrões em `internal/transport/http/api_*` e `internal/adapter/storage/s3/` para reuso.

---

### **Fase 7 — Hardening** (2-3 dias)

**Arquivos novos:**
- `internal/adapter/crypto/envelope.go` — KEK + DEK/tenant (LocalSealer) com interface `Sealer`
- `cmd/server/rotate_kek.go` — subcomando `rotate-kek` (C9)
- `.github/workflows/ci.yml` — build + test + openapi-validate + govulncheck
- `Makefile` — alvo `govulncheck`
- `docs/` — README + ADRs das decisões D1-D18

**DoD da Fase 7:**
- Envelope encryption ativo: tokens Meta, bot tokens Telegram, etc., cifrados com DEK/tenant.
- `rotate-kek` re-wrap de todos os DEKs sem perda (C9).
- CI verde: build, test (`-race -shuffle=on`), openapi-validate, govulncheck.
- `Interface Sealer` abstrai backend (VaultTransitSealer pós-1.0).

**Fonte de referência no mez-go:** `internal/adapter/secret/sealer/{local,vault}.go` (a interface `Sealer`); o backend Vault vira pós-1.0 (C9).

---

### **Fase 8 — Estabilização do processo único** (3-4 dias, NOVO C12)

**Arquivos novos:**
- `cmd/server/wire.go` — ordem determinística: migrate → sealer init → pools → bus → reconciler → providers → HTTP
- `cmd/server/main.go` — graceful shutdown coordenado: signal → parar aceitar HTTP → drain WS → bus drain → relay flush → whatsmeow `Disconnect()` por tenant
- `internal/adapter/broker/bus.go` — método `Drain(ctx)` com timeout
- `tests/chaos/kill9_test.go` — `kill -9` em pontos críticos, valida recovery
- `tests/boot/cold_boot_test.go` — N tenants em paralelo, warm-up paralelo
- `deployments/entrypoint.sh` — fail-fast se migration falhar

**DoD da Fase 8:**
- Ordem de boot determinística (migrate → sealer → pools → bus → reconciler → providers → HTTP).
- Shutdown coordenado: drain em 10s, whatsmeow `Disconnect()` por tenant antes do exit.
- **Teste chaos C1**: `kill -9` entre COMMIT e bus.Publish → reconciler recupera a mensagem.
- **Teste boot frio**: 10 tenants, todos conectam whatsmeow em < 30s.
- `migrate` falha-fechada: container não sobe se migration falha.

**Fonte de referência no mez-go:** padrões em `cmd/mez-core/main.go` (signal + Shutdown), `cmd/mez-worker-whatsmeow/main.go` (graceful Disconnect).

---

## 5. Mapeamento mez-go → mez-go-mono (tabela de origem por fase)

| Fase | % LOC reescrita do mez-go | Origem principal |
|------|---------------------------|-----------------|
| 0 | 30% (esqueleto + bus + core/port) | `pkg/{config,logger,health,metrics}`, `internal/core/{domain,event,port}`, `internal/adapter/broker/nats/broker.go` (só a interface) |
| 1 | 70% (auth + admin + audit + RLS fail-closed) | `cmd/mez-ui/main.go`, `internal/core/admin/*`, `internal/adapter/auth/argon2/*`, `internal/adapter/idp/oidc/*`, `internal/transport/adminweb/*` (parcial), `internal/usecase/admin/*` |
| 2 | 80% (repos + ingest + send + reconcile) | `internal/usecase/messaging/*`, `internal/usecase/routing/*`, `internal/adapter/repository/postgres/*`, `internal/transport/http/{webhook_*,api_*}`, `internal/adapter/provider/{waba,ig,msgr,tgbot}/*` (parcial) |
| 3 | 70% (sender + actions + capability) | `internal/usecase/messaging/send.go`, `internal/core/port/capability.go`, `internal/transport/websocket/hub.go` |
| 4 | **40%** (whatsmeow: maior reescrita) | `internal/adapter/provider/whatsmeow/*` (2.815 LOC), `cmd/mez-worker-whatsmeow/main.go`, `pkg/media/*` |
| 5 | 50% (painel: html→templ) | `internal/transport/adminweb/*` (3.729 LOC), `internal/transport/websocket/*` |
| 6 | **10%** (backup: genuinamente novo) | Apenas padrões de `internal/transport/http/api_*` e `internal/adapter/storage/s3/*` |
| 7 | 60% (envelope + CI) | `internal/adapter/secret/sealer/local.go` |
| 8 | 50% (wire + shutdown + chaos) | `cmd/mez-core/main.go` (signal/shutdown), `cmd/mez-worker-whatsmeow/main.go` (Disconnect) |

**LOC final estimado no mez-go-mono:** ~24.000-26.000 LOC de Go + ~3.500-5.500 LOC genuinamente novos (do README §22). Duração total estimada: **35-50 dias úteis solo**.

---

## 6. Definition of Done global (README §24 — "quando paramos?")

- [ ] `make build` verde em CI.
- [ ] `make test` verde em CI (`-race -shuffle=on`).
- [ ] `docker compose up` sobe postgres + minio + app.
- [ ] `curl /health` 200; `curl /readyz` 200.
- [ ] Wizard `/setup` cria admin.
- [ ] OIDC login funciona com IdP configurado.
- [ ] 5 canais recebem webhooks (quando configurados).
- [ ] Painel renderiza todas as rotas listadas em §15.
- [ ] QR-code whatsmeow é gerado e atualiza via htmx.
- [ ] **Backup gera arquivo no bucket; restore round-trip valida igualdade (C6/C7).**
- [ ] Reset wipe com confirmação dupla funciona.
- [ ] OpenAPI spec bate com handlers (CI valida).
- [ ] `govulncheck` passa.
- [ ] **Teste de regressão RLS fail-closed passa (C3/C4).**
- [ ] **`RunAsPlatform` gera audit log em todo acesso cross-tenant (C5).**
- [ ] **Reconciler recupera mensagens órfãs após `kill -9` (C1).**
- [ ] **Outbox drena no boot via poll de fallback (D3).**
- [ ] **`recover()` por dispatcher comprovado: panic de um tenant não derruba o processo (C10).**
- [ ] **`rotate-kek` re-wrap de todos os DEKs sem perda (C9).**
- [ ] Documentação (AGENTS.md, CLAUDE.md, README) atualizada.

---

## 7. Riscos assumidos (do README §21)

| Risco | Onde é mitigado |
|-------|-----------------|
| Blast radius = 100% | Fase 4: `recover()` por dispatcher + `-race` em CI + circuit breaker por tenant |
| Deploy = downtime total | README §20 (janela de manutenção); aceito como limitação do 1.0 |
| `migrate` no boot vira outage | Fase 8: migrations forward-only + backup prévio |
| WhatsMeow + IP único → risco de ban | Fase 7: rate-limit de envio + monitorar `whatsmeow_disconnected` |
| Contenção de pool Postgres (backup/burst) | Fase 0: 3 pools separados (`mez_migrate`/`mez_app`/`mez_platform`) |
| ffmpeg global satura entre tenants | Fase 4: semáforo (4) global + worker pool |
| Memória escala linear com tenants ativos | Fase 0: `MEZ_MAX_ACTIVE_TENANTS` como teto; multi-process é pós-1.0 |
| RLS fail-open via owner | Fase 0: `FORCE RLS` em 8 tabelas + role `mez_app` sem `BYPASSRLS` |
| Vazamento cross-tenant pelo admin | Fase 1: `RunAsPlatform` dedicado e auditado |
| Restore viola FK / colide sequence | Fase 6: FKs `DEFERRABLE INITIALLY DEFERRED` + UUID PKs |
| Backup inútil após migration | Fase 6: replay de migrations no restore (C7) |

---

## 8. O que **NÃO** entra no 1.0 (do README §2 — descarte consciente)

`analytics`/TimescaleDB, `crm`, `automation`/River, `campaigns`, `marketplace`, feature-flags-como-produto, tooling GDPR, **Vault Transit backend** (apenas `LocalSealer`). Essas features (~1.900 LOC no pai) brigam com "um container". Voltam pós-1.0 só se justificadas.

---

## 9. Próximos passos imediatos (Fase 0)

1. Criar `mez-go-mono/{go.mod,Makefile,README.md,AGENTS.md,CLAUDE.md}`.
2. Criar `migrations/0001_init.up.sql` com 3 roles + FORCE RLS + FKs deferíveis.
3. Criar `cmd/server/main.go` mínimo (apenas `serve` e `migrate` subcommands) + `pkg/logger` + `pkg/config`.
4. Criar `internal/adapter/broker/bus.go` com `PublishInbound` drop-safe.
5. Criar `internal/core/{domain,event,port}/` com os 5 canais.
6. Subir docker-compose: postgres + minio + app + healthchecks.

Após a Fase 0, **revisar o DoD antes de avançar para Fase 1**.

---

*Este plano é vivo: revisitar ao fim de cada fase e ajustar prazos/complexidade com base no que o código ensina.*
