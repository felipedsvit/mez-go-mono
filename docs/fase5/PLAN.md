# Fase 5 — Painel completo (adminweb + /app/* + WS real-time)

> **Status:** planejamento aprovado (junho/2026).
> **Escopo:** 10 issues (#70–#79) · ~5-6 dias estimados · single commit (squash) em `fase5-squash` → `main`.
> **Pré-requisitos:** Fases 0, 1, 2, 3 e 4 merged.
> **Base de reuso:** `internal/transport/adminweb/` (Fase 1: setup, login, dashboard, tenants, users, roles, audit) + `templ + htmx` (Fase 0) + `internal/transport/http/api/` (Fase 3: Bearer JWT) + WS hub pattern do pai `mez-go/internal/transport/websocket/` (~178 LOC).

---

## 1. Análise do projeto pai (mez-go)

A Fase 5 do `mez-go-mono` estende o `adminweb` (Fase 1) com a área
**tenant-scoped** (`/app/*` = inbox + conversas + send) e os endpoints admin
restantes. Também introduz o **WS hub per-tenant** para push de mensagens
inbound em tempo real na inbox — sem WS, o polling htmx é a única opção
(degrada UX).

### 1.1 Inventário de código reusável (porte mecânico + extensão)

| Componente do pai | Caminho | LOC | Issue destino | Tipo de porte |
|---|---|---:|---|---|
| WS hub (per-tenant in-memory fan-out) | `mez-go/internal/transport/websocket/` | ~178 | #70 | **mecânico** — channel Go + subscribe pattern |
| `/app/conversations` (lista) | `mez-go/cmd/mez-ui/` (templ templates) | ~300 | #71 | **reescrita** — mono usa `templ + htmx`; pai usa SPA-like |
| `/app/conversations/:id` (thread) | `mez-go/cmd/mez-ui/` | ~400 | #72 | **reescrita** — htmx `hx-trigger="new-message from:body"` |
| `/app/conversations/:id/messages` (send) | `mez-go/cmd/mez-core/handleSendMessage` | ~40 | #73 | **mecânico** — mono já tem `SenderService` (Fase 3) |
| `/admin/services` (health + métricas) | `mez-go/cmd/mez-ui/` | ~200 | #74 | **mecânico** — expõe `pkg/health.Checker` + `pkg/metrics` |
| `/admin/tenants/:id/channels` (5 canais) | `mez-go/cmd/mez-core/admin_tenants_channels.go` | ~350 | #75 | **mecânico** — lista providers + capabilities |
| `/admin/tenants/:id/qrcode` (whatsmeow) | `mez-go/cmd/mez-worker-whatsmeow/watchQR` | ~80 | #76 | **mecânico** — usa `Manager.CurrentQR` (Fase 4 #68) + htmx refresh |
| `/admin/tenants/:id/agents` (CRUD) | `mez-go/cmd/mez-core/admin_tenants_agents.go` | ~300 | #77 | **mecânico** — CRUD sobre `core/admin` (Fase 1) |
| CSRF middleware (já existe no mono) | `mez-go/internal/transport/http/middleware/csrf.go` | ~120 | #78 | **já feito** (Fase 1) — wiring |
| `htmx` extension WS (`htmx-ext-ws`) | `htmx.org/extensions/ws` | n/a | #72, #78 | **novo** — primeira vez que mono usa htmx + WS nativo |

**LOC reusáveis (porte mecânico):** ~1.500 LOC Go + ~300 LOC templates.
**LOC genuinamente novos:** ~1.000 LOC (WS hub, htmx integration, novos templates).

### 1.2 Patterns obrigatórios (do pai, mantidos em mez-go-mono)

Do `mez-go/AGENTS.md`:

1. **Multi-tenant via RLS, não filtragem app-side.** Já em vigor.
2. **Outbox pattern** (transacional). Já em vigor.
3. **Channel capability negotiation**. Já em vigor.
4. **Outbound action-aware** (D6). Já em vigor.
5. **`templ + htmx`** (D13, sem build JS, sem SPA). Já em vigor (Fase 0).
6. **Session cookies HttpOnly + CSRF token** (D16). **A aplicar em #78.**
7. **Audit log de toda ação admin** (D17). Já em vigor.
8. **Bearer JWT** para API programática. Já em vigor (Fase 3).
9. **WS hub per-tenant** (in-memory fan-out; bounded buffers; htmx `ws-connect`). **A aplicar em #70.**
10. **Logging zerolog** + **comentários em português**. Já em vigor.

### 1.3 Divergências arquiteturais entre pai e mez-go-mono

| Aspecto | mez-go (pai) | mez-go-mono | Impacto na Fase 5 |
|---|---|---|---|
| UI stack | `templ + htmx` (mas com `htmx-ext-ws` opcional) | `templ + htmx` (idêntico) | Trivial — Fase 5 herda Fase 0/1 |
| WS hub | Channel per-tenant + hub central | **Idêntico** (mono) | Padrão do pai funciona direto |
| Autenticação UI | Session cookie admin | Session cookie admin **+** tenant_id via OIDC (Fase 1) | Handler extra: extrair tenant do token OIDC |
| QR whatsmeow | HTTP server no `cmd/mez-worker-whatsmeow/watchQR` | `/admin/tenants/:id/qrcode` (Fase 5 #76) | Usa `Manager.CurrentQR` da Fase 4; sem processo separado |
| Inbox (`/app/*`) | `mez-ui` SPA-like + WS | `templ + htmx` + htmx WS extension | htmx nativo, sem JS hand-written |
| `htmx-ext-ws` | Usado em algumas rotas | **A habilitar** (Fase 5 #72) | Mono: 1ª vez usando a extensão |
| **Single-binary** | UI em `cmd/mez-ui` separado | **Mono** — `adminweb` é parte do mesmo binário | WS hub + adminweb no mesmo processo (race conditions?) — mitigação: `sync.RWMutex` no hub, channels bounded |
| Tenant isolation | RLS | RLS + tenant_id do token OIDC | `RunInTenantTx` via tenant do claim |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| Porte mecânico (handlers + templates + WS hub) | ~1.500 | 2.0 |
| Reescrita parcial (htmx + htmx-ext-ws + integração com bus) | ~600 | 1.0 |
| Genuinamente novo (templates `/app/*` + admin/services + channels + qrcode) | ~1.000 | 2.0 |
| Tests (adminweb + WS + /app/* E2E) | ~400 | 0.5 |
| Buffer (Fases 3+4 subestimaram em ~30%) | — | 0.5 |
| **Total** | **~3.500** | **~5-6** |

Mantém a estimativa de 5-6 dias do README §23.

---

## 2. Visão geral da Fase 5

Implementa a **camada de UI completa** do mez-go-mono: `/app/*` (inbox
tenant-side com WS real-time) + endpoints admin restantes (`/admin/services`,
`/admin/tenants/:id/channels`, `/admin/tenants/:id/qrcode`,
`/admin/tenants/:id/agents`). Habilita o **htmx-ext-ws** para push de
mensagens inbound na inbox. CSRF wiring em todo POST/PUT/DELETE.

A Fase 5 **NÃO** implementa:
- Backup/Restore/Reset UI (Fase 6)
- Painel de canais **editáveis** (criação de credenciais via UI): Fase 7
- Multi-tenant dashboard global (`/admin/services` mostra health por tenant)

---

## 3. Correções arquiteturais cobertas

| Correção | Descrição | Issues |
|---|---|---|
| **D13** | `templ + htmx` (sem build JS) — todo o painel é HDA | #70-#78 (todos) |
| **D16** | CSRF token em forms POST/PUT/DELETE | #78 |
| **D17** | Audit log em toda ação admin (criação/edição de tenant/agent/role) | #75, #77 |
| **C2** (reforço) | WS hub com bounded buffers por tenant | #70 |
| **C10** (carryover) | `recover()` por goroutine de WS handler (panic de 1 cliente não derruba o processo) | #70 |

---

## 4. Issues (10)

| # | Título | Camada | Esforço | Ref pai principal | Bloqueada por | Bloqueia |
|---|---|---|:--:|---|---|---|
| **#70** | `internal/transport/websocket/hub.go` — WS hub per-tenant + recover (C10) | transport | 0.5d | `mez-go/internal/transport/websocket/` (~178) | — | #71, #72, #78 |
| **#71** | `adminweb` handler `/app/conversations` (lista inbox) + template | transport | 0.5d | `mez-go/cmd/mez-ui/handlers_inbox.go` | #70 | #72 |
| **#72** | `adminweb` handler `/app/conversations/:id` (thread) + template + htmx WS | transport | 1.0d | `mez-go/cmd/mez-ui/handlers_thread.go` | #70, #71 | #73, #78 |
| **#73** | `adminweb` handler `POST /app/conversations/:id/messages` (send via `SenderService`) | transport | 0.5d | `mez-go/cmd/mez-core/handleSendMessage` (40) | #72 | #78 |
| **#74** | `adminweb` handler `/admin/services` (health + métricas) + template | transport | 0.5d | `mez-go/cmd/mez-ui/services.go` (~200) | — | #78 |
| **#75** | `adminweb` handler `/admin/tenants/:id/channels` (5 canais UI) + template | transport | 0.5d | `mez-go/cmd/mez-core/admin_tenants_channels.go` | — | #78 |
| **#76** | `adminweb` handler `/admin/tenants/:id/qrcode` (whatsmeow PNG + htmx refresh) | transport | 0.5d | `mez-go/cmd/mez-worker-whatsmeow/watchQR` (Fase 4 #68) | #70 (usa `Manager.CurrentQR`) | #78 |
| **#77** | `adminweb` handler `/admin/tenants/:id/agents` (CRUD) + template | transport | 0.5d | `mez-go/cmd/mez-core/admin_tenants_agents.go` | — | #78 |
| **#78** | middleware CSRF wiring em todo POST/PUT/DELETE do adminweb + `/app/*` (D16) | transport | 0.5d | (Fase 1 já tem `csrf.go` — wire only) | #70-#77 | #79 |
| **#79** | `tests/adminweb` — E2E (htmx + WS + CSRF + templates) | tests | 0.5d | `mez-go/internal/transport/adminweb/*_test.go` (mín.) | #70-#78 | — |

**Total:** ~5-6 dias (com buffer).

---

## 5. Ordem de execução

A ordem segue a coluna "Bloqueada por" + paralelização segura:

1. **#70** WS hub (foundation) — paralelizável com #74/#75/#77
2. **#74** `/admin/services` + **#75** `/admin/tenants/:id/channels` + **#77** `/admin/tenants/:id/agents` (paralelo, sem deps)
3. **#71** `/app/conversations` (lista) — depende de #70
4. **#72** `/app/conversations/:id` (thread + htmx WS) — depende de #70, #71
5. **#73** POST `/app/conversations/:id/messages` — depende de #72
6. **#76** `/admin/tenants/:id/qrcode` (whatsmeow PNG) — depende de #70 (WS broadcast do QR refresh)
7. **#78** CSRF wiring em todos os POST/PUT/DELETE — depende de #70-#77
8. **#79** tests E2E — depende de todos

---

## 6. Stacked commits (estratégia de squash)

Decisão: **squash único** em `fase5-squash`. PR `fase5-squash` → `main`.

Mensagem de commit (referência):

```text
feat(fase5): painel completo (adminweb + /app/* inbox + WS real-time + htmx + CSRF)

- transport/websocket: Hub per-tenant (subscribe + broadcast) + recover (C10)
- transport/adminweb: handler /app/conversations (lista inbox)
- transport/adminweb: handler /app/conversations/:id (thread + htmx WS extension)
- transport/adminweb: handler POST /app/conversations/:id/messages (SenderService)
- transport/adminweb: handler /admin/services (health + métricas)
- transport/adminweb: handler /admin/tenants/:id/channels (5 canais UI)
- transport/adminweb: handler /admin/tenants/:id/agents (CRUD)
- transport/adminweb: handler /admin/tenants/:id/qrcode (whatsmeow PNG + htmx refresh 5s)
- transport/adminweb: CSRF wiring em POST/PUT/DELETE (D16)
- transport/adminweb: 9 templates templ (inbox, thread, services, channels, agents, qrcode)
- tests/adminweb: E2E (htmx + WS + CSRF + templates)

Issues: #70, #71, #72, #73, #74, #75, #76, #77, #78, #79
DoD: 5 canais visíveis na UI, inbox real-time (WS), CSRF em forms,
QR-code whatsmeow com refresh htmx, audit log em ações admin.
```

---

## 7. Definition of Done (subset da Fase 5)

Do README §24, os itens cobertos por esta fase:

- [x] `make build` verde em CI.
- [x] `make test` verde em CI (`-race` + `-shuffle=on`).
- [x] **Painel renderiza todas as rotas listadas** (adminweb + /app/*).
- [x] **WS real-time na inbox** (htmx `ws-connect` extension).
- [x] **CSRF middleware** em todo POST/PUT/DELETE.
- [x] **QR-code whatsmeow** com refresh htmx (`hx-trigger="every 5s"`).
- [x] **Audit log** em toda ação admin (criação/edição de tenant/agent).
- [x] Documentação atualizada — este arquivo.

---

## 8. Riscos e mitigações específicas da Fase 5

| Risco | Mitigação |
|---|---|
| **WS hub em single-binary = race conditions** no estado compartilhado | `sync.RWMutex` no hub; channels bounded por tenant; `recover()` por goroutine de handler |
| **htmx-ext-ws** ainda não usado no mono (1ª vez) | Build tag check; fallback para polling 5s se a extensão não carregar |
| **CSRF** quebra se wire errado em templates | Test E2E valida que POST sem token → 403 |
| **WS broadcast** de mensagem inbound pode chegar fora de ordem | OK pelo design (idempotente + reconciler cobre gap — Fase 2) |
| **QR-code whatsmeow expira** em ~60s (limite do whatsmeow) | htmx refresh a cada 5s; detecta `event=success` e para refresh |
| **Single-box blast radius** se WS handler panicar | `recover()` por goroutine + métrica `ws_handler_panics_total` |
| **Memory leak** de conexões WS órfãs | Heartbeat ping/pong 30s; cleanup em `context.Done()` |
| **Tenant isolation quebrada** em `/app/*` (lista conversas de outro tenant) | `RunInTenantTx` com `tenant_id` do token OIDC; RLS garante fail-closed |
| **OpenAPI drift** entre UI e API | `make openapi-gen` regenera `openapi.gen.go`; CI valida diff |
| **CSRF + Bearer JWT API** misturados | CSRF só em cookie session; JWT só em `Authorization: Bearer` (sem CSRF) |

---

## 9. Carryover para fases seguintes

- **Fase 6** (Backup/Restore/Reset): UI `/admin/tenants/:id/backup` + `/restore` + `/reset` (confirmação dupla) com htmx polling de progresso
- **Fase 7** (Hardening): UI de rotação de KEK + audit dashboard + Secrets management (substituir env por `channel_credentials` encriptado)
- **Fase 8** (Estabilização): boot cold com N tenants (warm-up paralelo); WS reconnect strategy em deploy downtime
- **Pós-1.0:** UI de multi-tenant analytics (`/admin/analytics` com River/RiverUI); per-tenant egress IP UI

---

## 10. Referências cruzadas

- `mez-go/internal/transport/websocket/` (~178 LOC) — WS hub
- `mez-go/cmd/mez-ui/` (templ templates + handlers) — referência de UI
- `docs/fase4/PLAN.md` — predecessora (whatsmeow + `Manager.CurrentQR`)
- `docs/fase3/PLAN.md` — predecessora (`SenderService` para #73)
- `docs/fase1/PLAN.md` — predecessora (csrf.go, templates, audit)
- `internal/transport/adminweb/` (Fase 1) — base existente
- `internal/transport/http/api/handlers.go` (Fase 3+4) — `/api/*` paralelo
- `internal/core/admin/` — `Tenant`, `Agent`, `Role` types
- README §5 (D13, D15, D16, D17), §11 (matriz), §15 (rotas painel), §23 (Fase 5), §24 (DoD)
