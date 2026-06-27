# Plano de Correção — Alinhamento README vs Código

> **Status:** aprovado (junho/2026) · tracking em `fixes-tracking`
> **Base normativa:** `README.md` §2 (escopo 1.0), §11 (canais), §15 (painel web), §24 (Definition of Done)
> **Problema:** 4 gaps identificados onde o README descreve funcionalidade que o código não implementa (stubs)

---

## Gaps detectados

| # | Gap | O que o README diz | Realidade no código |
|---|-----|---------------------|---------------------|
| 1 | `templ` | §15: "templ + htmx" | ~~Zero arquivos `.templ`; admin usa `html/template` + `embed.FS`; `/app/*` usa `stubRenderer` com HTML inline placeholder~~ **RESOLVIDO** (jun/2026, issues #109–#116) |
| 2 | Providers HTTP | §11: 5 canais em paridade | Todos os HTTP clients (WABA, IG, MSG, TG) retornam valores fixos (`"wamid-stub"`, `"ig-mid-stub"`); `telegram_bot` usa `stubBotClient`; `whatsmeow` usa `NewStubClient` no registry |
| 3 | Painel `/app/*` | §15: inbox, thread, qrcode, channels, agents | ~~6 `stubRenderer` que produzem `<h1>{name}</h1><pre>"Fase 5 build verde — templ stub"</pre>` e ignoram todos os dados~~ **RESOLVIDO** (issues #112, #113) |
| 4 | Testes ausentes | §19: test suite | 9 pacotes sem testes; 5 têm testes portáveis do parent (`mez-go`), 2 são novos (backup, registry), 1 é reescrita (cmd/server) |

---

## Fase 1: Providers HTTP reais

Substituir todos os stubs dos clients por implementações HTTP reais, portando do `mez-go`.

### 1.1 WABA — Graph API

**Arquivos:** `internal/adapter/provider/waba/`

- **Criar** `client.go` com client HTTP real para Meta Graph API:
  - `SendMessage` → `POST /{version}/{phone_number_id}/messages`
  - `MarkRead` → `POST /{version}/{phone_number_id}/messages`
  - `DeleteMessage` → `DELETE /{version}/{phone_number_id}/messages`
- **Portar** mappers de payload do parent (`mapper.go` + `mapper_test.go`)
- **Remover** stubs de `waba.go` (substituir retorno fixo `"wamid-stub"` por chamada real)
- **Testes:** portar `client_test.go`, `mapper_test.go` do parent

**Estimativa:** 1.0d

### 1.2 Instagram — Graph API

**Arquivos:** `internal/adapter/provider/instagram/`

- **Criar** `client.go` com client HTTP real:
  - `SendMessage` → `POST /{version}/{ig_user_id}/messages`
  - Handover API (primary/secondary apps)
- **Portar** mappers + handover logic do parent
- **Remover** stubs
- **Testes:** portar `client_test.go`, `handover_test.go`, `mapper_test.go`

**Estimativa:** 0.5d

### 1.3 Messenger — Send API

**Arquivos:** `internal/adapter/provider/messenger/`

- **Criar** `client.go` com client HTTP real:
  - `SendMessage` → `POST /{version}/me/messages`
  - Sender Actions, Persistent Menu, One-Time Notification
- **Portar** 6 arquivos de teste do parent
- **Remover** stubs
- **Testes:** portar `otn_test.go`, `send_test.go`, `capabilities_test.go`, `persistent_menu_test.go`, `handover_test.go`, `mapper_test.go`

**Estimativa:** 1.0d

### 1.4 Telegram Bot — Bot API

**Arquivos:** `internal/adapter/provider/telegram_bot/`

- **Criar** `client.go` usando `github.com/go-telegram/bot` SDK real:
  - `SendMessage`, `SendChatAction`, `SendMedia`, etc.
- **Remover** `stubBotClient` do `registry/boot.go`
- **Adaptar** interface `BotClient` para o SDK real ou remover a interface e usar o client direto
- **Testes:** adaptar 12 testes do parent (caminho `tgbot/` → `telegram_bot/`)

**Estimativa:** 1.0d

### 1.5 Whatsmeow — Client real

**Arquivos:** `internal/adapter/provider/whatsmeow/`, `internal/adapter/provider/registry/boot.go`

- Já é o provider mais completo (1.291 LOC, 8 arquivos)
- Trocar `NewStubClient` → client real no registry boot
- A interface `Client` já está bem definida em `client.go`; só falta conectar o client real
- **Testes:** já existe `whatsmeow_test.go` (básico); portar testes adicionais do parent se necessário

**Estimativa:** 0.3d

### 1.6 Registry boot — limpeza

**Arquivos:** `internal/adapter/provider/registry/boot.go`

- Remover `stubBotClient` e `stubBotClient.SendMessage`/`SendChatAction`
- Passar dependências reais via `BuildOpts`
- **Testes:** escrever do zero (NEW)

**Estimativa:** 0.3d

**Total Fase 1: ~4.0d**

---

## Fase 2: Migração `html/template` → `templ` ✅ (jun/2026)

> **Decisão revista (jun/2026):** o §2 original desta fase optou por manter
> `html/template` e atualizar o README §15. Esta decisão foi revertida.
> O código passa a usar `github.com/a-h/templ` (alinhado com §15/D13).
> `templ` já está instalado via `make tools`; o alvo `templ-gen` existe
> no `Makefile` (linha 74).
>
> O pai (`mez-go`) também usa `html/template` (decisão registrada em
> `mez-go/internal/transport/adminweb/render/render.go:1-6`), portanto
> esta migration é trabalho **REWRITE genuíno** no mono, não porte.
> README §22 já previa 1.500-2.000 LOC para a rewrite — esta fase a
> executa.

Issues de tracking: #109 (setup), #110 (layout), #111 (14 templates),
#112 (stubs `/app/*`), #113 (8 templates do pai), #114 (remoção
`html/template`), #115 (snapshot tests), #116 (docs).

### 2.0 Setup + Makefile (0.3d) — #109

- Adicionar `github.com/a-h/templ` em `go.mod` (já indirect; vira
  direct via imports).
- `Makefile` ganha `templ-gen` (falha se `templ` ausente em CI; não skip)
  e `templ-check` (espelho de `openapi-validate`: regenera, diff contra
  commit, exit 1 se drift).
- `PATH` no `Makefile` inclui `$(HOME)/go/bin` para achar o tool.
- Verificar que `go mod tidy` mantém a dep como direct.

### 2.1 Layout base tipado (0.5d) — #110

- `internal/transport/adminweb/templates/layout.templ` define os
  primitives compartilhados:
  - `templ BaseLayout(...)` (shell `<html>` + CSS inline + Header + NavBar)
  - `templ Layout(title, p)` (equivale ao `base.html` com `{{block "content" .}}`)
  - `templ Header(p)`, `templ NavBar(active, p)`,
    `templ ErrorBanner(msg)`, `templ SuccessBanner(msg)`,
    `templ CSRFInput(token)`
- CSS inline (decisão: manter o CSS atual do mono, não migrar para
  Pico CSS do pai). Pequeno ajuste: classes `card-grid`, `topbar`,
  `sidebar`, `layout-body` adicionadas.
- `types.go` define `PageData` (struct tipado exportado, usado como
  prop em todos os components) + helpers `Truncate`, `FormatDate`,
  `HasPermission`, `IsPlatform` (substituem `template.FuncMap`).

### 2.2 Conversão dos 14 templates `.html` → `.templ` (5.0d) — #111

Cada template vira um component com prop tipada (não mais `map[string]any`).
Handlers são atualizados para construir o struct apropriado.

| Antigo `.html` | Novo `.templ` | Prop tipada | LOC templ |
|---|---|---|---:|
| `dashboard.html` | `dashboard.templ` | `PageData` | 30 |
| `login.html` | `login.templ` | `PageData, oidcEnabled bool` | 28 |
| `audit.html` | `audit.templ` | `AuditData{Page, Entries []admin.AuditEntry}` | 50 |
| `users.html` | `users.templ` | `UsersData{Page, Users []UserRow}` | 80 |
| `user_new.html` | `users.templ` | `UserNewData{Page, Roles []admin.Role}` | 40 |
| `roles.html` | `roles.templ` | `RolesData{Page, Roles []admin.Role}` | 50 |
| `role_detail.html` | `roles.templ` | `RoleDetailData{Page, Role, AllPerms, HasPerms}` | 50 |
| `role_new.html` | `roles.templ` | `RoleNewData{Page, Role}` | 35 |
| `tenants.html` | `tenants.templ` | `TenantsData{Page, Tenants []admin.Tenant}` | 60 |
| `tenant_new.html` | `tenants.templ` | `TenantNewData{Page}` | 25 |
| `tenant_detail.html` | `tenants.templ` | `TenantDetailData{Page, Tenant}` | 50 |
| `backup.html` | `backup.templ` | `BackupData{Page, TenantID, Backups, Job}` | 70 |
| `backup_list.html` | `backup.templ` | `BackupListData{Page, TenantID, Backups}` | 35 |
| `backup_status.html` | `backup.templ` | `BackupStatusFragment(job, csrfToken)` | 25 |
| `reset.html` | `reset.templ` | `ResetData{Page, TenantID}` | 30 |

LOC total gerado: ~700 (templ) + ~200 (props tipados). Build verde após
cada commit (hard cut por template; ver §"Sequência de execução").

### 2.3 Substituir `stubRenderer`/`stubR`/`Renderer` (1.5d) — #112

`internal/transport/adminweb/handlers_app.go` perde:

- 6 `stubRenderer` factories (`InboxPage`, `ThreadPage`, `QRCodePage`,
  `ServicesPage`, `ChannelsPage`, `AgentsPage`).
- struct `stubR` e método `Render` com HTML inline placeholder.
- interface `Renderer`.
- `csrfTokenFromCtx` stub (agora lê do cookie real via
  `csrfTokenFromContext`).

Handlers passam a chamar `templates.Inbox(InboxData{...}).Render(ctx, w)`
diretamente. `handlers_admin.go` (services/channels/agents) também
migrado.

### 2.4 Portar 8 templates do pai (2.5d) — #113

Portados de `mez-go/internal/transport/adminweb/templates/`, adaptados
para o CSS inline do mono (não adota Pico):

| Origem pai | Mono `.templ` | Prop |
|---|---|---|
| `inbox.html` (33 LOC) | `inbox.templ` | `InboxData{Page, Conversations []InboxConv}` |
| `thread.html` (55 LOC) | `thread.templ` | `ThreadData{Page, Conv, Messages, WSURL}` |
| `fragments.html` | `fragments.templ` | `HealthData{Page, Checks []HealthCheck}` + `QRCodeData` + `ErrorData` |
| `error.html` | `fragments.templ` | `ErrorData` |
| `health.html` (15 LOC) | `fragments.templ` | `HealthData` |
| `tenant_detail.html` (59 LOC) | `tenants.templ` | `TenantDetailData` |
| `user_detail.html` (79 LOC) | `user_detail.templ` | `UserDetailData` |
| `role_new.html` (28 LOC) | `roles.templ` | `RoleNewData` |

### 2.5 Remoção de `html/template` (0.5d) — #114

- **Deletar** `internal/transport/adminweb/render/render.go` (substituído
  por chamada direta a `templ.Component.Render`).
- **Deletar** `internal/transport/adminweb/embed.go` (templ gera `.go`
  puro; só assets estáticos precisariam de embed, e não temos nenhum
  ainda).
- **Reescrever** `internal/transport/adminweb/server.go`:
  - remover `tpls fs.FS`, `funcmap` (now/truncate/hasPerm), `renderPage`.
  - adicionar `renderTempl(w, templ.Component)` que seta
    `Content-Type: text/html` e chama `c.Render(ctx, w)`.
  - adicionar `basePageData(r)` que pré-popula `Principal`, `CSRFToken`,
    `Now` (substitui o boilerplate repetido em todos os handlers).
- Atualizar 9 `handlers_*.go` para chamar components.

### 2.6 Snapshot tests (1.0d) — #115

- `internal/transport/adminweb/templates/snapshot_test.go` valida
  HTML output de cada component via `templ.Component.Render(ctx, &buf)`.
- 15 testes:
  - `TestLogin_RendersTitle`, `TestLogin_RendersSSO_WhenOIDCEnabled`
  - `TestDashboard_RendersTilesForPlatformPrincipal`
  - `TestAudit_EmptyEntries`, `TestAudit_WithEntries`
  - `TestTenants_EmptyList`, `TestTenants_WithRows`
  - `TestRoles_Renders`
  - `TestChannels_RendersFiveChannels`
  - `TestInbox_EmptyConversations`
  - `TestReset_RendersConfirmationForm`
  - `TestErrorPage_RendersStatus`
  - `TestHealth_RendersChecks`
  - `TestTruncate`, `TestFormatDate_Empty`, `TestFormatDate_NonEmpty`

### 2.7 Documentação (0.3d) — #116

- README §22 marca rewrite templ como executado (issue #116).
- README §23 adiciona Fase 2b (10-14d) ao roadmap, com totais
  atualizados (45-64d acumulado).
- Este `000_FIXES.md` (§2 reescrito, DoD atualizado, totais ajustados).

**Total Fase 2: ~13.6d**

---

## Fase 3: Testes

| Package | Ação | Origem | Esforço |
|---------|------|--------|--------:|
| `provider/waba` | Portar `client_test.go`, `mapper_test.go` | MECHANICAL | 0.3d |
| `provider/instagram` | Portar `client_test.go`, `handover_test.go`, `mapper_test.go` | MECHANICAL | 0.3d |
| `provider/messenger` | Portar 6 test files | MECHANICAL | 0.5d |
| `provider/telegram_bot` | Adaptar 12 test files (`tgbot/` → `telegram_bot/`) | ADAPT | 0.5d |
| `usecase/admin` | Portar 7 test files do parent | MECHANICAL | 0.5d |
| `repository/postgres/admin` | Portar 1 test (`admin_integration_test.go`) | MECHANICAL | 0.3d |
| `usecase/backup` | Escrever do zero (backup round-trip, export/restore) | NEW | 1.0d |
| `provider/registry` | Escrever do zero (factory tests, credential resolution) | NEW | 0.5d |
| `cmd/server` | Smoke test básico (build + serve + health check) | NEW | 0.3d |

**Total Fase 3: ~4.0d**

---

## Dependências

```
Fase 1 (providers HTTP)  ──────►  Fase 3 (testes)
       │                              │
       └── providers reais            └── testes dependem dos providers reais
           necessários para               (especialmente waba, messenger,
           os testes de integração         telegram_bot)

Fase 2 (templates)       ──────►  ✅ CONCLUÍDA (jun/2026, #109-#116)
       │
       └── independente de Fase 1 e 3
```

Fase 1 e Fase 3 são dependentes. Fase 2 é independente e já está fechada.

---

## Resumo

| Fase | Descrição | Dias | Status |
|:----:|-----------|:----:|--------|
| 1 | Providers HTTP reais (WABA, IG, MSG, TG, WM, registry) | ~4.0 | pendente |
| 2 | Migração `html/template` → `templ` (14 convertidos + 8 porte + stubs `/app/*` + remoção `html/template`) | ~13.6 | ✅ concluída (jun/2026) |
| 3 | Testes portados + novos | ~4.0 | depende Fase 1 |
| **Total** | | **~21.6d** | Fase 2 fechada; Fases 1+3 restantes: ~8.0d |

---

## Definition of Done

- [x] `make build` verde em CI (parcial — Fase 8 cmd/server tem débitos pré-existentes, fora do escopo).
- [x] `make test` verde (`-race -shuffle=on -count=1`) — adminweb + templates.
- [ ] `make test-integration` verde (depende Fase 1).
- [x] Nenhum `stubRenderer`/`stubR`/`Renderer` (interface) no código.
- [x] Nenhum `html/template` em imports de `adminweb/`.
- [x] Nenhum `embed.FS` para templates (templ gera `.go` puro).
- [ ] Todos os 5 providers com client HTTP real para suas respectivas APIs (Fase 1).
- [x] `/app/conversations`, `/app/conversations/{id}`, `/app/qrcode` renderizam HTML real com dados.
- [x] 14 templates admin convertidos para `.templ` + 8 templates portados do pai.
- [x] `make templ-check` disponível no CI.
- [x] Snapshot tests verdes para cada component (`templates/snapshot_test.go`).
- [x] `goleak.VerifyTestMain` nos pacotes críticos (Fase 8 #105 — independente desta fase).
