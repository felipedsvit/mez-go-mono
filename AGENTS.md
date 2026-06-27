# AGENTS.md

Quick-reference for agents working in `github.com/felipedsvit/mez-go-mono` (Go,
hexagonal/clean, single-binary monorepo). Read `CLAUDE.md` for the full
architecture essay; this file is the high-signal operational subset.

## 1. Layout (single-binary monorepo)

- `cmd/server` — **single binary** with subcommands: `serve | migrate | setup | rotate-kek`.
  Wires bus, ingestor, routing, outbox relay, reconciler, webhooks e API REST.
- `internal/core/{domain,event,port}` — sem deps externas. Domain types, bus
  event envelopes, ports.
- `internal/usecase/{messaging,routing,outbox,reconcile,auth,admin,channels,backup}` —
  application logic. Sem deps de adapters.
- `internal/adapter/{broker,repository,storage,cache,idp,webhook,auth}` —
  driven adapters. `repository/postgres` implementa FORCE RLS + 3 roles
  (`mez_app`/`mez_migrate`/`mez_platform`).
- `internal/transport/{http,websocket,adminweb}` — driving adapters.
  - `http/api` é a API REST (Fase 2: `/api/*` sem version prefix).
  - `adminweb` é o painel templ+htmx.
- `pkg/{config,logger,health,metrics,crypto}` — app-agnostic helpers.
- `api/openapi.yaml` + `api/openapi.gen.go` — OpenAPI 3.1 (gerado por
  `oapi-codegen`).
- `migrations/` — golang-migrate embedded (subcommand `migrate`).
- `tests/{rls,auth,platform,inbound}` — testcontainers E2E (`-tags integration`).

## 2. Commands

```bash
make tools                # instala templ, oapi-codegen, golang-migrate
make build                # go build ./...
make test                 # go test -race -count=1 ./...
make test-integration     # go test -race -count=1 -tags=integration ./...
make lint                 # go vet + golangci-lint
make fmt                  # gofmt -l -w .
make generate             # templ + oapi-codegen
make openapi-gen          # regenera api/openapi.gen.go
make migrate-up           # aplica migrations (local)
make docker               # build da imagem
make run                  # go run ./cmd/server serve
```

Single-test:
```bash
go test -run TestName ./path/to/pkg
go test -tags integration -race -count=1 -timeout 10m ./...
```

## 3. Config

- Override via env vars: prefixo `MEZ_`, `.` → `_`
  (ex: `MEZ_DATABASE_URL`, `MEZ_PLATFORM_DATABASE_URL`,
  `MEZ_MASTER_KEY`, `MEZ_RECONCILE_INTERVAL`).
- `cfg.Load()` é fail-fast. `cfg.ValidateServe()` valida campos do `serve`
  (exige `MEZ_SESSION_SECRET` e `MEZ_API_JWT_SECRET`).
- 3 pools Postgres:
  - `MEZ_DATABASE_URL` — role `mez_app` (SEM BYPASSRLS, RLS aplica).
  - `MEZ_PLATFORM_DATABASE_URL` — role `mez_platform` (BYPASSRLS, auditado).
  - `MEZ_MIGRATE_DATABASE_URL` — role `mez_migrate` (owner das tabelas).
- **NUNCA** conectar como `mez_migrate` em runtime. `migrate` é subcommand
  único que pode usar esse role.
- App role em runtime = `mez_app` (RLS via `set_config('mez.tenant_id', ...)`).
- `MEZ_API_JWT_SECRET` — HS256 secret para tokens Bearer da API.
- `MEZ_MASTER_KEY` ou `MEZ_MASTER_KEY_FILE` — KEK 32 bytes para envelope
  encryption (C9).

## 4. ⚠️ PR + Issues — REGRA OBRIGATÓRIA

**Toda PR que resolve issues DEVE usar keywords de fechamento na descrição.
O GitHub fecha as issues automaticamente quando a PR é mergeada em `main`.**

### Sintaxe aceita (case-insensitive, aceita `:` depois)

```text
Closes #34
Fixes #35
Resolves #36
```

Keywords suportadas: `close`, `closes`, `closed`, `fix`, `fixes`, `fixed`,
`resolve`, `resolves`, `resolved`.

### Múltiplas issues

```text
Resolves #34, resolves #35, resolves #36, resolves #37, resolves #38,
resolves #39, resolves #40, resolves #41, resolves #42, resolves #43,
resolves #44, resolves #45
```

### Cross-repo (issue em outro repo)

```text
Fixes owner/repo#123
```

### Gotcha crítico

> As keywords **só são interpretadas se a PR tem como target a branch
> default do repo** (`main` no mez-go-mono). Se a PR target qualquer outra
> branch, as keywords são IGNORADAS e o merge não fecha nenhuma issue.

### Comando correto

```bash
gh pr create \
  --base main \
  --head faseN-squash \
  --title "feat(faseN): ..." \
  --body "$(cat <<'EOF'
# Fase N — ...

## Issues

Closes #34
Closes #35
Closes #36
...

## Resumo
...
EOF
)"
```

### Verificação pré-merge

Antes de executar `gh pr merge`:

1. `gh pr view <N> --json body | grep -iE "closes|fixes|resolves"` — confirma
   que as keywords estão presentes.
2. `gh pr view <N> --json baseRefName` — confirma que `baseRefName == "main"`.
3. Se faltar keyword, **edite a descrição** antes de mergear:
   `gh pr edit <N> --body "..."`.

### ❌ Anti-pattern (causou o incidente da Fase 2)

```bash
# ERRADO: PR body sem keywords → issues ficam abertas após merge
gh pr create --body "Implementa pipeline inbound. Resolvido em #46."
gh pr merge 46 --squash
# Result: 12 issues (#34-#45) ficaram OPEN; só o PR foi mergeado.
```

**Correção aplicada retroativamente:**
- `gh issue close 34..45 --comment "Resolvido por #46"`
- `gh issue comment 34..45 --body "Cross-link ao PR #46"`

## 5. Architecture rules agents will trip on

- **Multi-tenant via RLS, NÃO filtragem app-side.** Repos recebem `TenantID`
  apenas via `context.Context` populado por `db.RunInTenantTx`
  (sets `mez.tenant_id` via `set_config(..., is_local=true)`).
  Repos **nunca** recebem `tenantID` como parâmetro.
- **FORCE ROW LEVEL SECURITY** está habilitado em todas as tabelas
  multi-tenant (C3). `mez_app` **não** tem `BYPASSRLS` — query fora de
  `RunInTenantTx` falha (fail-closed, C4).
- **`RunAsPlatform`** (C5) é o ÚNICO caminho cross-tenant, com role
  `mez_platform` (BYPASSRLS, auditado em `audit_log`).
- **Atomic dedup** no inbound: `MessageRepo.Insert` usa
  `ON CONFLICT (tenant_id, channel, provider_msg_id) WHERE provider_msg_id IS NOT NULL`
  (migrations 0001 + 0003).
- **Outbox pattern**: o ingestor grava outbox row **na mesma** tenant-tx que
  a mensagem. `OutboxRelay` drena com sinal in-process + poll 5s fallback (D3).
- **Outbox cross-tenant** usa `RunAsPlatform` (mez_platform, BYPASSRLS) em
  vez de SECURITY DEFINER.
- **Bus in-process tipado**: `PublishInbound`/`PublishOutbound`/etc. (sem
  `interface{}`). Buffer de `inbound` é non-blocking/drop-safe (C2) —
  reconciler cobre o drop.
- **Reconciler (C1)** varre `messages WHERE status='received'` no boot e a
  cada 30s, marca como `routed` via `FOR UPDATE SKIP LOCKED`.
- **Channel capability negotiation**: `port.Channel` adapters expõem
  `Capabilities()` em runtime; sender faz fallback media→text.
- **Migrations são subcommand** `migrate` (D11). Roda no boot — falha
  derruba container (fail-closed). Migrations destrutivas exigem janela.
- **OpenAPI 3.1 é contrato** (D12). Spec autoral em `api/openapi.yaml`;
  `make openapi-gen` regenera `api/openapi.gen.go`. CI valida diff.
- **Webhook signature verification fail-closed** (D8):
  - Meta: `X-Hub-Signature-256` HMAC-SHA256; sem app secret → 503.
  - Telegram: `X-Telegram-Bot-Api-Secret-Token`; sem secret → 503.
  - Assinatura inválida → 403 (não 401, é signature fail).
- **API sem version prefix** (D15): rotas em `/api/*` (não `/api/v1/*`).

## 6. Pitfalls (these are real, not theoretical)

- **Graceful shutdown é mandatório** (D10 + C12). SIGINT/SIGTERM →
  `srv.Shutdown` → `bus.Drain` → `relay.Stop` → `reconciler.Stop` →
  pools close. Sem isto, bus perde notificação.
- **whatsmeow client is not goroutine-safe** (Fase 4). Não tocar até lá.
- **Bus buffer cheio em `inbound` → drop-safe, NÃO bloquear**. O
  reconciler cobre o drop. Bloquear esgota o pool HTTP.
- **OutboxRelay com `Sender` noop (Fase 2)**: deixa em `pending` e loga
  warn a cada tick. Sender real pluga na Fase 3 sem mudar infra.
- **HMAC secret em memória** (vetor de dump): `defer zero(secret)` no fim
  do handler. Mitigação completa é Vault Transit (pós-1.0).
- **Reconciler race com routing consumer**: ambos usam
  `FOR UPDATE SKIP LOCKED` no mesmo select de `messages WHERE status='received'`.
- **Logger é zerolog** (`github.com/rs/zerolog`). Structured key/value.
- **Comentários em português** (consistência com mez-go e a equipe).
- **CI roda `-race -count=1 -shuffle=on`** para detectar data races (crítico
  no bus e whatsmeow dispatcher).
- **`migrate` no boot = fail-closed**: se migration falhar, container
  não sobe (outage intencional).
- **Single-box blast radius = 100%**: panic não recuperado derruba todos
  os tenants. `recover()` por goroutine de dispatcher/tenant.
- **PostgreSQL 16+** (testcontainers). `mez_app` tem `FORCE RLS` aplicado
  na migration 0001; testes de regressão em `tests/rls/`.

## 7. CI

`.github/workflows/ci.yml` jobs (push/PR to `main`/`develop`):

| Job | Command |
|-----|---------|
| **build-and-test** | `gofmt -l .`, `go vet ./...`, `go build ./...`, `go test -race -shuffle=on -count=1 ./...` |
| **openapi** | regenera `openapi.gen.go`, `git diff --exit-code` |
| **security** | `govulncheck` + `gosec` → SARIF |
| **integration** | `go test -tags integration -race -count=1 -shuffle=on -timeout 10m ./...` via testcontainers |

Local: `make fmt && make vet && make test`.

## 8. Workflow de fase (padrão usado em Fase 0/1/2)

1. **Planejamento**: `docs/faseN/PLAN.md` com issues, dependências, ordem.
2. **Branch**: criar `faseN` (work) e `faseN-squash` (target PR).
3. **Stacked commits** (opcional) ou squash único (decisão da fase).
4. **Push**: `git push origin faseN-squash`.
5. **PR** (com keywords!): `gh pr create --base main --head faseN-squash
   --body "Closes #X, Closes #Y, ..."` ← **NÃO ESQUECER**.
6. **CI verde**: build + test + openapi-validate.
7. **Merge**: `gh pr merge <N> --squash --delete-branch`.
8. **Verificar issues fechadas**: `gh issue list --state closed --limit 20`.
9. **Comentário de release** se aplicável.

## 9. Onde olhar quando em dúvida

- `README.md` — visão geral + decisões arquiteturais (C1-C12, D1-D18).
- `docs/plan.md` — roadmap macro (Fase 0 → 8).
- `docs/faseN/PLAN.md` — plano detalhado da fase N.
- `internal/core/event/event.go` — bus event envelopes.
- `internal/core/port/` — interfaces (repos, channels, sealer).
- `migrations/` — schema autoritativo. Mudanças sempre via migration nova.
- `api/openapi.yaml` — contrato de API (source of truth).
- `mez-go/AGENTS.md` + `mez-go/CLAUDE.md` — referência do pai (single source
  de patterns, embora divergente por causa do broker).

## 10. Histórico de incidentes & mitigações

| Data | Incidente | Mitigação |
|------|-----------|-----------|
| 2026-06-27 (Fase 2) | PR #46 mergeada sem keywords `Closes #N`; 12 issues (#34-#45) ficaram OPEN após o merge. | Fechamento manual via `gh issue close` + cross-link comments. Regra §4 adicionada a este arquivo. |

Para evitar reincidência, valide **sempre** o `gh pr view` antes de
executar `gh pr merge` (ver §4).
