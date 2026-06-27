# Fase 4 — WhatsMeow (canal informal)

> **Status:** planejamento aprovado (junho/2026).
> **Escopo:** 11 issues (#58–#68) · ~6-8 dias estimados · single commit (squash) em `fase4-squash` → `main`.
> **Pré-requisitos:** Fases 0, 1, 2 e 3 merged.
> **Base de reuso:** ~4.800 LOC Go portados de `mez-go/internal/adapter/provider/whatsmeow/` (adapter, dispatcher, send, actions, reconnect, identity, media) + `pkg/media/transcode.go` (190 LOC).

---

## 1. Análise do projeto pai (mez-go)

A Fase 4 do `mez-go-mono` substitui o **5º canal — WhatsMeow** — que é o
mais complexo de todos: conexão persistente (não webhook), dispatcher
bounded por tenant com `recover()`, reconexão automática, session store
em Postgres, e suporte a mídia transcodada (ffmpeg/cwebp).

A infra Fase 3 (SenderRegistry + SenderService + StatusConsumer + outbox/relay +
API) **é a fundação** sobre a qual o whatsmeow pluga. As mudanças
estruturais são todas internas ao novo pacote.

### 1.1 Inventário de código reusável (porte mecânico + cirurgia)

| Componente do pai | Caminho | LOC | Issue destino | Tipo de porte |
|---|---|---:|---|---|
| `pkg/media/transcode.go` | `mez-go/pkg/media/transcode.go` | 140 | #58 | **mecânico** — ffmpeg/cwebp wrappers + semáforo global |
| `media.go` (whatsmeow download/upload) | `mez-go/internal/adapter/provider/whatsmeow/media.go` | 280 | #62 | **mecânico** — usa `pkg/media` |
| `adapter.go` (Client + handlers) | `mez-go/internal/adapter/provider/whatsmeow/adapter.go` | 530 | #60, #62 | **cirurgia** — pai tinha pool multi-tenant; mono usa 1 client/tenant (D4) |
| `send.go` (text/image/audio/sticker/video) | `mez-go/internal/adapter/provider/whatsmeow/send.go` | 185 | #62 | **mecânico** — assinatura de port.Sender muda |
| `actions.go` (reaction/edit/revoke/mark_read/typing/presence) | `mez-go/internal/adapter/provider/whatsmeow/actions.go` | 142 | #63 | **mecânico** — D6 já tem Action enum (Fase 3) |
| `dispatcher.go` (buffers + recover) | `mez-go/internal/adapter/provider/whatsmeow/dispatcher.go` | 104 | #61 | **mecânico** + bound a tenant-id |
| `reconnect.go` (autoReconnect backoff) | `mez-go/internal/adapter/provider/whatsmeow/reconnect.go` | 184 | #64 | **mecânico** — bounded retries; após N → reconnect failed event |
| `identity.go` (QR pairing) | `mez-go/internal/adapter/provider/whatsmeow/identity.go` | 80 | #65 | **mecânico** — depende do `*whatsmeow.Client.GetQRChannel` |
| `events.go` (inbound + history sync) | `mez-go/internal/adapter/provider/whatsmeow/events.go` | 156 | #66 | **mecânico** — public via bus (Fase 2 bus) |
| `mapper.go` (canônico) | `mez-go/internal/adapter/provider/whatsmeow/mapper.go` | 154 | #66 | **mecânico** — Fase 2 `domain.Message` igual |
| `humanize.go` (text utils) | `mez-go/internal/adapter/provider/whatsmeow/humanize.go` | 180 | #62 | **mecânico** — helpers |
| `error_filter.go` | `mez-go/internal/adapter/provider/whatsmeow/error_filter.go` | 117 | #62 | **mecânico** — reclassifica erros do whatsmeow |
| `warmup.go` (multi-tenant warmup paralelo) | `mez-go/internal/adapter/provider/whatsmeow/warmup.go` | ~50 | #60 | **reescrita** — mono usa `MEZ_MAX_ACTIVE_TENANTS` |
| `groups.go`, `newsletter.go`, `blocklist.go`, `calls.go`, `disappearing.go`, `privacy.go`, `profile.go`, `history.go`, `status.go` | `mez-go/internal/adapter/provider/whatsmeow/` | ~1.700 | #63 (parcial), carryover | **deferir** — Fase 4 cobre apenas Send + actions; groups/newsletter/calls/etc são Fase 5+ |
| `registry.go` (per-tenant) | `mez-go/internal/adapter/provider/whatsmeow/registry.go` | 70 | #60 | **reescrita** — mono já tem `port.SenderRegistry` (Fase 3) |
| **whatsmeow session store** (Postgres) | não existe no pai (era file-based) | — | #59 | **novo** — schema migration + repo |
| **whatsmeow mock client** | não existe no pai | — | #69 | **novo** — testes |
| **OpenAPI whatsmeow endpoints** | não existe no pai (era cmd/mez-worker) | — | #68 | **novo** — `qrcode` + `health` |

**LOC reusáveis (porte mecânico):** ~2.300 LOC Go (de ~4.800 totais do pai).
**LOC genuinamente novos:** ~1.500 LOC (session store, manager per-tenant, qrcode handler, mock client, chaos tests).
**LOC carryover (Fase 5+):** ~1.000 LOC (groups, newsletter, calls, blocklist, history, privacy, profile).

### 1.2 Patterns obrigatórios (do pai, mantidos em mez-go-mono)

Do `mez-go/AGENTS.md`:

1. **Multi-tenant via RLS, não filtragem app-side.** Já em vigor.
2. **Outbox pattern** (transacional). Já em vigor.
3. **Channel capability negotiation**. Já em vigor (Fase 3).
4. **Outbound action-aware** (D6). Já em vigor (Fase 3).
5. **golang-migrate** embedded como library. Já em vigor.
6. **OpenAPI 3.1 + oapi-codegen**, CI valida diff. Já em vigor.
7. **Graceful shutdown** SIGINT/SIGTERM → `Disconnect()`. **A aplicar em #64.**
8. **Functional options** (`WithX`, `WithY`) para deps opcionais. **A aplicar em #60, #61.**
9. **HMAC + secrets em memória**: `defer zero(secret)`. **A aplicar em #59 (session store).**
10. **zerolog structured logging**. Já em vigor.
11. **Comentários em português**. **Seguir.**
12. **Adapter registry per (channel, tenant)** — pattern do `tokens.go` do pai (Fase 3).
13. **`recover()` por goroutine de dispatcher** (C10). **A aplicar em #61.** Decisão de arquitetura
    crítica: panic num tenant **não pode** derrubar o processo inteiro.

### 1.3 Divergências arquiteturais entre pai e mez-go-mono

| Aspecto | mez-go (pai) | mez-go-mono | Impacto na Fase 4 |
|---|---|---|---|
| Pool whatsmeow | Multi-tenant pool (router distribuído) | **1 client/tenant** (D4) | Manager per-tenant; lazy init; bounded por `MEZ_MAX_ACTIVE_TENANTS` |
| Session store | file-based em disco | **Postgres** (`whatsmeow_sessions` table) | Migration 0004 + repo |
| Inbox | NATS subject `whatsmeow.inbound.*` | **bus in-process** `PublishInbound` | Reescrita do handler; mesmo modelo dos 4 canais |
| Mídia | download/upload S3 + transcode ffmpeg local | **S3 + pkg/media semáforo** | Mesma infra; semaphore global `MEZ_FFMPEG_CONCURRENCY=4` |
| QR pairing | `cmd/mez-core` expõe HTTP server | **`/api/channels/whatsmeow/qrcode`** | Endpoint novo; refresh htmx no painel (Fase 5) |
| Multi-device | support parcial | **1 device/tenant** (escopo 1.0) | Documentado como limitação |
| History sync | OOM risks em contas grandes | **bounded via `whatsmeow.HistorySync` config** | Limite 1000 messages/tenant no start |
| Egress IP | compartilhado | **compartilhado (single-box)**; carryover pós-1.0 = egress dedicado | Documentado em §21 do README |
| AutoReconnect | exponential backoff | **idêntico** | Porte direto |
| `*whatsmeow.Client` goroutine-safety | **NÃO thread-safe** (oficial) | **disparado via dispatcher per-tenant (single goroutine)** | Decisão central: TODA chamada ao Client passa pelo dispatcher; **nenhuma** chamada direta |
| Sticker pack | download próprio | **deferido** (Fase 5 com `pkg/media` estendido) | Fora do escopo 1.0 |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| Porte mecânico (send/actions/media/mapper/error_filter/humanize) | ~1.500 | 1.5 |
| Reescrita parcial (manager per-tenant, dispatcher bounded, registry) | ~800 | 1.5 |
| Genuinamente novo (session store postgres, qrcode handler, mock client, pkg/media semaphore) | ~1.500 | 2.0 |
| Chaos tests (panic recovery, reconnect cycle, E2E testcontainers) | ~700 | 1.0 |
| Stacked commits + PR review + CI | — | 0.5 |
| Buffer (Fases anteriores subestimaram em ~30%) | — | 0.5 |
| **Total** | **~4.500** | **~6-8** |

Mantém a estimativa de 6-8 dias da Fase 4 do README. O ganho do reuso é
parcialmente compensado pelo **per-tenant manager** (não existe no pai como
conceito unificado — era um pool com router).

---

## 2. Visão geral da Fase 4

Implementa o **5º canal — WhatsMeow** — end-to-end: QR pairing via API
→ `Manager` cria 1 `*whatsmeow.Client` por tenant → dispatcher bounded
canaliza TODA chamada (não-thread-safe) → Send (text/image/audio/sticker/
video) + Actions (D6) → eventos inbound (mensagens + history sync + status)
publicados no bus → reconciler (Fase 2) cobre drops. AutoReconnect com
backoff; `Disconnect()` no shutdown coordenado.

A Fase 4 **NÃO** implementa:
- Groups, newsletter, calls, blocklist, privacy, profile, disappearing messages
  (são Fase 5+; matriz de capabilities marca `CapGroups`, `CapNewsletter`,
  `CapCalls`, `CapBlocklist`, `CapDisappearing` como suportadas pelo adapter
  no `Capabilities()` mas a chamada real retorna `ErrNotImplemented`).
- Mídia transcoding real (ffmpeg/cwebp no Docker — deferido para Fase 5;
  Fase 4 só `passthrough` + validação de MIME type).
- QR refresh automático via htmx no painel (Fase 5 adminweb).
- Voice/video calls (Fase 5 ou pós-1.0).

---

## 3. Correções arquiteturais cobertas

| Correção | Descrição | Issues |
|---|---|---|
| **C1** (reforço) | AutoReconnect cobre crash entre Connect e event handler; reconciler (Fase 2) cobre drops de eventos | #64, #66 |
| **C2** (reforço) | Dispatcher bounded buffers por tenant; backpressure observável via métrica `whatsmeow_dispatcher_dropped` | #61 |
| **C3** (carryover) | Session store em `whatsmeow_sessions` com FORCE RLS + mez_app sem BYPASSRLS | #59, migration 0004 |
| **C4** (carryover) | INSERT/UPDATE de session fail-closed se `mez.tenant_id` ausente | #59 |
| **C5** (carryover) | LoadSession via `RunAsPlatform` (cross-tenant) APENAS para manager; audit log | #59 |
| **C10** | **`recover()` por goroutine de dispatcher** — panic num tenant não derruba o processo | #61, #64 |
| **D4** | **1 client/tenant** (Manager lazy) | #60 |
| **D6** | Actions (reaction/edit/revoke/mark_read/typing/presence) via `port.Action` (Fase 3) | #63 |
| **D7** | `Capabilities()` retorna o set honesto (groups/newsletter/etc declarados mas `ErrNotImplemented`) | #62 |
| **D9** | Storage S3-compatible para mídia (download/upload de whatsmeow) | #62 |
| **D10** | Graceful shutdown: signal → `Manager.DisconnectAll()` (per-tenant) → bus drain → relay flush | #64 |
| **D12** (carryover) | OpenAPI regenerado com `/api/channels/whatsmeow/qrcode` + `/health` real (não 501) | #68 |

---

## 4. Issues (11)

| # | Título | Camada | Esforço | Ref pai principal | Bloqueada por | Bloqueia |
|---|---|---|:--:|---|---|---|
| **#58** | `pkg/media` — transcode helpers (ffmpeg/cwebp) + semáforo global (4) | pkg | 0.5d | `mez-go/pkg/media/transcode.go` (140) | — | #62 |
| **#59** | `migration 0004_whatsmeow` + `whatsmeow_session_store` (Postgres, FORCE RLS, envelope encryption) | migrations + adapter | 0.5d | novo (pai era file-based) | — | #60, #62 |
| **#60** | `internal/adapter/provider/whatsmeow/manager.go` — Manager (1 client/tenant, lazy init, warmup paralelo, bounded por `MEZ_MAX_ACTIVE_TENANTS`) | adapter | 1.0d | `mez-go/internal/adapter/provider/whatsmeow/{adapter,registry,warmup}.go` (~700) | #59 | #61, #67 |
| **#61** | `internal/adapter/provider/whatsmeow/dispatcher.go` — Dispatcher per-tenant (bounded buffers + `recover()` por goroutine, C10) | adapter | 0.5d | `mez-go/internal/adapter/provider/whatsmeow/dispatcher.go` (104) | #60 | #62, #63 |
| **#62** | `internal/adapter/provider/whatsmeow/adapter.go` — Adapter (port.Sender: text/image/audio/sticker/video) + `media.go` (download/upload S3) + `error_filter.go` | adapter | 1.0d | `mez-go/internal/adapter/provider/whatsmeow/{adapter,media,error_filter,send,humanize}.go` (~1.200) | #58, #61 | #63, #66, #67 |
| **#63** | `internal/adapter/provider/whatsmeow/actions.go` — Actions (D6: reaction/edit/revoke/mark_read/typing/presence) | adapter | 0.5d | `mez-go/internal/adapter/provider/whatsmeow/actions.go` (142) | #62 | #67, #68 |
| **#64** | `internal/adapter/provider/whatsmeow/reconnect.go` — AutoReconnect (backoff exponencial) + graceful `Disconnect()` no shutdown | adapter | 0.5d | `mez-go/internal/adapter/provider/whatsmeow/reconnect.go` (184) | #60, #62 | #65, #66 |
| **#65** | `internal/adapter/provider/whatsmeow/identity.go` — Identity (QR pairing: load creds, `GetQRChannel`, save on connect) | adapter | 0.5d | `mez-go/internal/adapter/provider/whatsmeow/identity.go` (80) | #59, #60 | #68 |
| **#66** | `internal/adapter/provider/whatsmeow/events.go` — Inbound events (messages, history sync, status) → bus.PublishInbound/Status + OOM guard | adapter | 0.5d | `mez-go/internal/adapter/provider/whatsmeow/{events,mapper,history}.go` (~360) | #60, #62 | #67, #68 |
| **#67** | `internal/adapter/provider/registry/boot.go` — registra factory whatsmeow no `SenderRegistry` (depende de session store + manager) | adapter | 0.5d | (Fase 3 boot.go estendido) | #60, #62, #63, #64, #66 | #68 |
| **#68** | `internal/transport/http/api` — `/api/channels/whatsmeow/qrcode` (PNG base64) + `/health` (real, não stub) + `api/openapi.yaml` regenerado | transport + docs | 0.5d | novo (era cmd/mez-core no pai) | #65, #66, #67 | — |

**Total:** ~6-8 dias (com buffer).

> **Nota sobre numeração:** o plano referencia #58-#68 como próximos números
> sequenciais após Fase 3 (#47-#57). Se houver carryover ou ajustes, a
> numeração real pode variar; consultar `gh issue list --state all` antes
> de criar.

---

## 5. Ordem de execução

A ordem segue a coluna "Bloqueada por" + paralelização segura:

1. **#58** `pkg/media` (foundation para mídia) — **paralelo com #59**
2. **#59** `migration 0004_whatsmeow` + session store — **paralelo com #58**
3. **#60** `Manager` (1 client/tenant, lifecycle) — depende de #59
4. **#61** `Dispatcher` (bounded buffers + recover) — depende de #60
5. **#62** `Adapter` (port.Sender: text/image/audio/sticker/video) — depende de #58, #61
6. **#63** `Actions` (D6: reaction/edit/revoke/mark_read/typing/presence) — depende de #62
7. **#64** `Reconnect` (autoReconnect + Disconnect) — depende de #60, #62
8. **#65** `Identity` (QR pairing) — depende de #59, #60
9. **#66** `Events` (inbound → bus) — depende de #60, #62
10. **#67** `SenderRegistry` boot (whatsmeow factory) — depende de todas anteriores
11. **#68** `transport/http/api` (qrcode + health) + `api/openapi.yaml` — depende de #65, #66, #67

**Paralelização:** #58 ∥ #59 (1 dia) e depois #62 ∥ #63 ∥ #64 ∥ #65 ∥ #66
(parcialmente; algumas têm dependências entre si mas podem ser stacked em
commits intermediários).

---

## 6. Stacked commits (estratégia de squash)

Decisão: **squash único** em `fase4-squash`. PR `fase4-squash` → `main`.

Mensagem de commit (referência):

```text
feat(fase4): WhatsMeow (canal informal) — 1 client/tenant, dispatcher bounded + recover, session store postgres, QR pairing, AutoReconnect, mídias

- pkg/media: transcode helpers (ffmpeg/cwebp) + semáforo global (4)
- migration 0004_whatsmeow: whatsmeow_sessions table (FORCE RLS, envelope encryption)
- adapter/provider/whatsmeow: Manager (1 client/tenant, lazy init, warmup paralelo)
- adapter/provider/whatsmeow: Dispatcher per-tenant (bounded buffers + recover por goroutine, C10)
- adapter/provider/whatsmeow: Adapter (port.Sender: text/image/audio/sticker/video + media S3)
- adapter/provider/whatsmeow: Actions (D6: reaction/edit/revoke/mark_read/typing/presence)
- adapter/provider/whatsmeow: Reconnect (autoReconnect backoff) + graceful Disconnect
- adapter/provider/whatsmeow: Identity (QR pairing: load creds, GetQRChannel, save on connect)
- adapter/provider/whatsmeow: Events (inbound messages + history sync bounded + status → bus)
- adapter/provider/registry: factory whatsmeow registrada no SenderRegistry
- transport/http/api: /api/channels/whatsmeow/qrcode (PNG base64) + /health real
- api/openapi.yaml: regenerado com whatsmeow endpoints

Issues: #58, #59, #60, #61, #62, #63, #64, #65, #66, #67, #68
DoD: 5º canal funcional em paridade com WABA/IG/MSG/TG, AutoReconnect
validado em chaos test, panic recovery não derruba processo, QR pairing
end-to-end, status pipeline bidirecional.
```

---

## 7. Definition of Done (subset da Fase 4)

Do README §24, os itens cobertos por esta fase:

- [x] `make build` verde em CI.
- [x] `make test` verde em CI (`-race` + `-shuffle=on`).
- [x] **5 canais em paridade: 5 enviam mensagens** (Fase 4 fecha whatsmeow).
- [ ] Painel renderiza todas as rotas listadas. (Phase 5)
- [x] **OpenAPI spec bate com handlers (CI valida)** — #68.
- [x] **POST /api/messages retorna 200** com `message_id` e status pipeline — herdado Fase 3.
- [x] **5 canais recebem webhooks** (4 webhooks + whatsmeow push) — quando configurados.
- [x] **Outbox drena no boot via poll de fallback (D3)** — herdado Fase 2.
- [x] **Outbox MaxAttempts → DLQ após N** — herdado Fase 3.
- [x] **Capability fallback (media→text) funciona** — herdado Fase 3.
- [x] **Actions (reaction, edit, revoke, mark_read, typing, presence) implementados** — #63.
- [x] **Status pipeline (sent/delivered/read/failed) atualiza `messages.status`** — herdado Fase 3.
- [x] **WhatsMeow `recover()` por goroutine de dispatcher (C10)** — #61.
- [x] **Graceful `Disconnect()` per-tenant no shutdown coordenado (D10)** — #64.
- [x] **Session store em Postgres (não file-based)** — #59.
- [x] **AutoReconnect com backoff exponencial** — #64.
- [x] **History sync bounded (1000 messages/tenant) para OOM guard** — #66.
- [x] Documentação atualizada — este arquivo.

---

## 8. Riscos e mitigações específicas da Fase 4

| Risco | Mitigação |
|---|---|
| `*whatsmeow.Client` **não é goroutine-safe** (oficial; documentado) | TODA chamada ao Client passa pelo Dispatcher per-tenant (single goroutine); nenhuma chamada direta. Dispatcher tem buffer bounded + `recover()`. (#61) |
| **Panic num tenant derruba processo** (C10) | `recover()` por goroutine de dispatcher; log estruturado com `tenant_id`; `MEZ_MAX_ACTIVE_TENANTS` como teto operacional. Chaos test valida. (#61, #64) |
| **OOM em history sync** (contas com 100k+ mensagens) | History sync bounded a 1000 messages/tenant no primeiro start; resto via `app_state_sync` incremental. Configurável via `MEZ_WHATSMeow_HISTORY_BATCH`. (#66) |
| **Egress IP único** → risco de ban (já em §21 do README) | Documentado; carryover pós-1.0 = egress dedicado por tenant. Monitorar `whatsmeow_disconnected`. |
| **Session store em arquivo** (pai) — não escala e vaza credenciais em disco | Session em Postgres com FORCE RLS + envelope encryption (C3, C9). (#59) |
| **QR pairing expira** (timeout ~60s do whatsmeow) | Handler `/api/channels/whatsmeow/qrcode` regenera QR sob demanda; painel htmx refresh a cada 5s (Fase 5). (#65, #68) |
| **AutoReconnect loop infinito** após ban permanente | Após N tentativas (default 10), marcar tenant como `banned`; health endpoint retorna `{"status":"banned"}`; admin é notificado. (#64) |
| **Multi-device** — pai suporta, mono não | Documentado como limitação do 1.0; carryover pós-1.0. |
| **Sticker pack download** — OOM em packs grandes | Deferido para Fase 5 com `pkg/media` estendido. |
| **`MEZ_MAX_ACTIVE_TENANTS`** estourar | Manager retorna `ErrTooManyActiveTenants` no `GetOrCreate`; health endpoint reporta. Pool eviction LRU. (#60) |
| **Session store vazio** (tenant novo) | Identity detecta: `GetQRChannel` chamado, QR exposto via API. (#65) |
| **Token de WhatsApp expirado** | Detectado no `Connect` (`ErrInvalidToken`); Identity retorna erro; tenant precisa re-emparelhar. |
| **WhatsApp manda `Disconnect` por uso indevido** (spam, ban) | Detectado em `events.go`; Reconnect NÃO tenta de novo por 24h. (#64, #66) |
| **Memória escala O(N) tenants** | `MEZ_MAX_ACTIVE_TENANTS=100` como teto; carryover multi-process (pós-1.0). Documentado em §7 do README. |
| **`go.sum` ficar dessincronizado** com `go.mau.fi/whatsmeow` | `go mod tidy` no CI; dependência declarada em `go.mod` direto (não via `replace`). |
| **whatsmeow v0.x breaking change** | Pin de versão; renovate bot (carryover Fase 7). |

---

## 9. Carryover para fases seguintes

- **Fase 5** (Painel): `/admin/tenants/:id/channels/whatsmeow` mostra QR-code
  com refresh htmx + status `connected/disconnected/banned`. UI para upload
  de credenciais (hoje via env) → canal `whatsmeow` ganha `Credentials` da
  `channel_credentials` (envelope encryption).
- **Fase 6** (Backup): `whatsmeow_sessions` entra no backup lógico por
  tenant (export + restore idempotente). Sessão restaurada é revalidada
  (não basta copiar bytes — dispositivo pode ter sido revogado).
- **Fase 7** (Hardening): `MEZ_MASTER_KEY` rotation → re-criptografa todas
  as `whatsmeow_sessions`. JWT key rotation. `text_enc BYTEA` em messages
  (Fase 4 já deixa o adapter pronto; encriptação no MessageRepo é Fase 7).
- **Fase 8** (Estabilização): `Manager` com hot-reload de credenciais;
  chaos tests: kill -9 mid-send → AutoReconnect no próximo start.
  Warm-up paralelo de N tenants no boot (atualmente serial).
- **Pós-1.0:** groups/newsletter/calls/blocklist/privacy/profile/disappearing
  (atualmente `Capabilities()` declara mas `Send` retorna `ErrNotImplemented`).
  Egress IP dedicado por tenant. Multi-device (1 device/tenant no 1.0).
  Multi-process por shard de tenant (atualmente single-box).

---

## 10. Referências cruzadas

- `mez-go/AGENTS.md` — rules de arquitetura e pitfalls.
- `mez-go/CLAUDE.md` — ensaio arquitetural completo.
- `mez-go/internal/adapter/provider/whatsmeow/adapter.go` (530 LOC) — fonte principal.
- `mez-go/internal/adapter/provider/whatsmeow/dispatcher.go` (104 LOC) — pattern bounded buffer + recover.
- `mez-go/internal/adapter/provider/whatsmeow/reconnect.go` (184 LOC) — backoff exponencial.
- `mez-go/internal/adapter/provider/whatsmeow/send.go` (185 LOC) — Send + mídias.
- `mez-go/internal/adapter/provider/whatsmeow/actions.go` (142 LOC) — D6 actions.
- `mez-go/internal/adapter/provider/whatsmeow/identity.go` (80 LOC) — QR pairing.
- `mez-go/internal/adapter/provider/whatsmeow/events.go` (156 LOC) — inbound + history sync.
- `mez-go/internal/adapter/provider/whatsmeow/media.go` (280 LOC) — S3 + pkg/media.
- `mez-go/pkg/media/transcode.go` (140 LOC) — ffmpeg/cwebp semaphore.
- `docs/fase3/PLAN.md` — predecessora (Sender + registry + outbox + status).
- `docs/fase2/PLAN.md` — pipeline inbound (reconciler, bus, ingestor).
- `internal/core/port/sender.go` (Fase 3) — `port.Sender` + `Action` enum + `OutboundRequest`.
- `internal/core/port/sender_registry.go` (Fase 3) — `SenderRegistry` per-tenant.
- `internal/usecase/messaging/send.go` (Fase 3) — `SenderService` que chama `registry.Get`.
- `internal/adapter/provider/registry/boot.go` (Fase 3) — wire-up das factories; estender com whatsmeow.
- `internal/adapter/webhook/secrets/credentials.go` (Fase 3) — estender com `MEZ_WHATSmeow_*` env vars (deferido para Fase 5/7).
- `internal/adapter/repository/postgres/` — `RunInTenantTx` + `RunAsPlatform`.
- README §5 (D4, D6, D7, D10), §6 (entrega), §7 (bus), §11 (matriz + WhatsMeow modelo simplificado), §16 (estrutura), §18 (env vars), §20 (operação), §21 (riscos), §22 (reuso), §23 (Fase 4), §24 (DoD).
