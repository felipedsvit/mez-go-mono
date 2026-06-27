# AGENTS.md — Guia Operacional para mez-go-mono

> **Quick-start para agentes IA trabalhando no mez-go-mono.**
> Leia `CLAUDE.md` (quando criado) para a visão arquitetural completa.
> Este arquivo é a referência rápida de operação.

---

## Identidade do Projeto

- **Repositório:** `github.com/felipedsvit/mez-go-mono`
- **Descrição:** Gateway omnichannel de mensageria em um único binário Go.
- **Status:** Fase 0 + 1 + 2 + 7 + 9 + 10 merged em `main`. Fases 3–8 em planejamento. PR #159 (Fase 9 + 10) aberto em `fase8+`.
- **Modelo:** Monólito modular single-process (vs. `mez-go` pai: 6 binários + NATS).
- **Go version:** 1.22+

---

## Direção da Implementação

Este projeto **porta** ~90% da lógica do pai (`/home/user/felipedsvit/mez-go`, ~51.800 LOC) mas **substitui** infraestrutura (NATS → bus in-process tipado) e collapsa multi-processo em um binário único.

**Fonte de verdade:** `README.md` §16 (estrutura) e §23 (roadmap 8 fases).

---

## Comandos Principais

```bash
make build                          # go build ./...
make test                           # go test -race -shuffle=on ./...
make test-integration               # testcontainers tests
make vet                            # go vet ./...
make openapi-gen                    # oapi-codegen → api/openapi.gen.go
docker compose -f deployments/docker-compose.yml up -d
```

---

## Receita: port-phase — Portar uma Fase

Use quando iniciar Fase 3–8. Preencha parâmetros:

| Parâmetro | Exemplo (Fase 3) |
|-----------|------------------|
| `[FASE_N]` | `3` |
| `[ISSUES]` | `#46 #47 #48 #49 #50` |
| `[PACKAGES_ALVO]` | `usecase/messaging/send`, `adapter/provider/waba`, `adapter/provider/instagram`, `adapter/provider/messenger`, `adapter/provider/tgbot`, `transport/websocket` |

---

### Fase 1 — Reconhecimento (read-only)

1. **Leia o plano:**
   ```bash
   cat /home/user/felipedsvit/mez-go-mono/docs/plan.md | grep -A 100 "^### Fase [FASE_N]"
   cat /home/user/felipedsvit/mez-go-mono/docs/fase[FASE_N]/PLAN.md  # if exists
   ```

2. **Para cada package em `[PACKAGES_ALVO]`, localize no pai:**
   ```bash
   find /home/user/felipedsvit/mez-go/internal -type d -name "<pkg>"
   find /home/user/felipedsvit/mez-go/internal/<pkg> -name "*.go" ! -name "*_test.go" -exec wc -l {} +
   ```

3. **Issues abertos:**
   ```bash
   gh issue list --state open --label fase[FASE_N]
   ```

**Saída esperada:** lista de arquivos com classificação (MECHANICAL | ADAPT | REWRITE | OUT_OF_SCOPE).

---

### Fase 2 — Tabela de Divergência (reference)

Aplicar tabela **universal** abaixo a todo arquivo. Válida para Fases 3–8.

| Aspecto | mez-go (pai) | mez-go-mono (alvo) | Ação de porte |
|---------|---------|---------|---------|
| **Broker** | NATS JetStream | In-process typed bus | `jetstream.Publish` → `bus.PublishInbound/Outbound`; remover `Nats-Msg-Id`, `AckWait`, subjects |
| **Multi-binary** | `cmd/mez-core`, `cmd/mez-worker-whatsmeow`, etc. | `cmd/server/wire.go` (único) | Reescrever boot determinístico + graceful shutdown (C12). Remover `pkg/shard`. |
| **RLS isolação** | 1 role `mez_app` + SECURITY DEFINER | 3 roles: `mez_migrate`/`mez_app`/`mez_platform` | Substituir SECURITY DEFINER por `RunAsPlatform` (Go). `tenant_id` via context, nunca parâmetro. |
| **Outbox relay** | SECURITY DEFINER no DB | `OutboxRelayRepo.ForEachTenant` + `RunAsPlatform` | Iterar por tenant em Go com pool mez_platform. |
| **Session store** | Redis | In-memory (`adapter/cache/memory/session.go`) | Trocar; `goleak.VerifyTestMain` obrigatório. |
| **Rate limit** | Redis | In-memory token bucket | Trocar; sem Redis no 1.0. |
| **Sealer** | Local + Vault Transit | LocalSealer only (`pkg/crypto/envelope.go`) | Não portar `vault.go`; interface permite pós-1.0. |
| **Routing (ACD)** | Queues + skills + sticky | Simplificado: `Assign()` com `defaultAgentID` | Portar apenas básico. Full ACD é Fase 5+. |
| **Sinks** | OpenSearch + Redis + Webhooks | Fora do 1.0 | **Não portar** `adapter/sink/`. |
| **Sharding** | `pkg/shard/shard.go` (crc32 % N) | 1 client/tenant em memória | **Não portar** `pkg/shard`. |
| **Telegram** | long-poll em mez-core | Webhook (já em `adapter/webhook/telegram/`) | Apenas mapper; não long-poll. |
| **Auth API** | JWT HS256 + API key + OIDC | Bearer JWT + session cookie | Não portar `adapter/auth/apikey`. |

---

### Guardrails — O que NUNCA Portar

```
❌  usecase/{analytics, crm, automation, campaigns, marketplace, featureflags, gdpr, audit, media}
❌  adapter/{sink/, ratelimit/redis/, cache/redis/, secret/sealer/vault.go}
❌  adapter/broker/nats/
❌  pkg/shard/
❌  cmd/{mez-analytics-consumer, mez-automation, mez-campaigns, mez-ui}
❌  transport/otel/
❌  sdk/typescript/
```

**Regra de ouro:** Se assuma NATS, Redis, Vault, multi-processo, ou feature fora de README §2 → não porta.

---

### Fase 3 — Plano de Implementação

Para cada issue em `[ISSUES]`, crie tabela em `docs/fase[FASE_N]/PLAN.md`:

```markdown
| Issue | Arquivo(s) alvo | Arquivo(s) pai | Classificação | Esforço | Bloqueado |
|-------|---|---|---|---|---|
| #46   | `usecase/messaging/send.go` | `mez-go/internal/usecase/messaging/send.go` | ADAPT | 1.0d | #45 |
```

**Regras estimativa:**
- `MECHANICAL` = swap + adapt types, sem lógica nova → 0.3–0.5d/arquivo
- `ADAPT` = mesma lógica, divergência aplicada → 0.5–1d
- `REWRITE` = semântica diferente (NATS→bus, multi-pool→1-client) → 1–3d
- `NEW` = sem precedente → ~200 LOC/dia

**Ordem:** migrations → repos → usecases → adapters → transport → wire → tests → openapi.

**Stacked commits:** squash em `fase[FASE_N]-squash` → PR único para `main`.

---

### Fase 4 — Implementação (editar código)

**10 padrões obrigatórios (herdados do pai):**

1. **RLS via context, nunca parâmetro** — `RunInTenantTx(ctx, tenantID, fn)`
2. **Dedup atômico** — `ON CONFLICT (tenant_id, channel, provider_message_id) WHERE provider_message_id <> ''`
3. **Outbox transacional** — INSERT na mesma tx da mensagem
4. **Capability negotiation** — adapters expõem `Capabilities()`, sender fallback `media→text`
5. **Functional options** — `New(WithX, WithY)` para deps opcionais
6. **Graceful shutdown** — SIGTERM → HTTP stop → WS drain → bus drain → `Disconnect()` por tenant
7. **recover() por goroutine** — panic de 1 tenant não derruba processo (C10)
8. **Non-blocking PublishInbound** — drop-safe; reconciler cobre (C1)
9. **Comentários português** — consistência com pai
10. **FORCE RLS em multi-tenant** — C3 + C4

**Checklist por arquivo:**
- [ ] `go build ./...` verde
- [ ] `go vet ./...` verde
- [ ] Teste (caminho feliz + erro)
- [ ] Sem imports: `adapter/sink`, `broker/nats`, `pkg/shard`, `cache/redis`, `secret/sealer/vault`
- [ ] Se schema: migration `up.sql` + `down.sql`

---

### Fase 5 — Verificação

```bash
make build
make test
make test-integration
make openapi-gen && git diff --exit-code api/openapi.gen.go
go test -tags integration -race -run TestRLSFailClosed ./tests/rls/...
go test -tags integration -race -run TestRunAsPlatform ./tests/platform/...
```

**PR format:**
- Branch: `fase[FASE_N]-squash`
- Base: `main`
- Title: `Fase [FASE_N]: <descrição>` 
- Body: tabela layers, DoD checklist, `Closes [ISSUES]`

---

## Referências Rápidas

| O quê | Onde |
|-------|------|
| Arquitetura | `README.md` (divergência → código) |
| Roadmap | `docs/plan.md` |
| Capacidades | `mez-go/docs/canais/README.md` |
| Envelope types | `mez-go/internal/core/event/event.go` |
| Webhook Meta (HMAC) | `mez-go/internal/transport/http/webhook_meta.go` |
| Outbox relay | `mez-go/internal/outbox/relay.go` |
| Ingestor (dedup) | `mez-go/internal/usecase/messaging/ingest.go` |
| Testcontainers | `mez-go/internal/testutil/pgtest/pgtest.go` |
| Pitfalls pai | `mez-go/AGENTS.md` seção "Pitfalls" |

---

## Regras Arquiteturais (Critical)

1. **RLS fail-closed:** `RunInTenantTx` obrigatório; query fora dele deve **falhar** com erro (C4).
2. **`whatsmeow.Client` não thread-safe** — serializar com mutex; buffers bounded (2048 events, 8 history).
3. **1 client/tenant em memória** — sem sharding no 1.0; IP saída compartilhado.
4. **Non-blocking bus** — `PublishInbound` nunca bloqueia handler HTTP (drop-safe, reconciler cobre).
5. **Reconciler (C1)** — `FOR UPDATE SKIP LOCKED`, varredura boot + 30s periódico.
6. **Graceful shutdown coordenado** — SIGTERM → HTTP stop → drain → `Disconnect()` por tenant.
7. **Envelope encryption local** — AES-256-GCM, KEK from env, DEK/tenant (C9).
8. **App-level config NÃO vai em env var** (Fase 10, issue #177) — vai em
   `system_settings` (DB, cifrado pela master KEK). Env vars são
   apenas bootstrap (DB URL, master key, JWT secret). Ver § "Fase 10
   — DB-backed system_settings" abaixo.

---

## Fase 7 — Hardening

A partir da Fase 7, credenciais de canal (Meta tokens, Telegram bot
tokens) **não vivem mais em env vars**. São cifradas por tenant
(DEK/tenant wrapped por KEK) na tabela `channel_credentials`. A
classe `Keyring` (em `internal/usecase/secrets/keyring.go`) é o
orquestrador de Encrypt/Decrypt; o `LocalSealer` (em
`internal/adapter/crypto/local_sealer.go`) é o adapter que satisfaz
`port.Sealer` + `port.Encryptor`.

**Como setar credenciais (em runtime):**

```bash
# Após migrate, o seed de credenciais é via API admin (Fase 5b) ou
# via SQL ad-hoc (apenas dev):
psql $MEZ_DATABASE_URL -c "
INSERT INTO channel_credentials (tenant_id, channel, wrapped_dek, encrypted, kek_version)
VALUES ('<tenant-uuid>', 'waba', '<base64-wrapped-dek>', '<base64-encrypted>', 1);"
```

**Como rodar `rotate-kek`:**

```bash
# 1. Gerar nova KEK
NEW_KEK=$(openssl rand -base64 32)

# 2. Dry-run (sem persistir)
MEZ_MASTER_KEY=$OLD_KEK \
MEZ_MASTER_KEY_NEW=$NEW_KEK \
make rotate-kek ARGS="--dry-run"

# 3. Real (atômico via RunAsPlatform)
MEZ_MASTER_KEY=$OLD_KEK \
MEZ_MASTER_KEY_NEW=$NEW_KEK \
make rotate-kek

# 4. Após sucesso, atualize o env var:
export MEZ_MASTER_KEY=$NEW_KEK
unset MEZ_MASTER_KEY_NEW
```

**Auditoria:** toda rotação grava:

- 1 linha `secrets.rotate_kek.started` antes de iterar
- N linhas `secrets.rotate_kek.tenant` (1 por (tenant, channel))
- 1 linha `secrets.rotate_kek.complete` no fim

Mais 1 linha `platform:access` por `UpdateWrappedDEK` (C5 atômico
via `RunAsPlatform`). Auditar com:

```sql
SELECT created_at, actor_email, action, target_id, tenant_id, metadata
FROM admin_audit_log
WHERE action LIKE 'secrets.rotate_kek%' OR action = 'platform:access'
ORDER BY created_at DESC LIMIT 50;
```

**Caveats:**

- Perda da KEK = perda de TODAS as credenciais. Backup offline
  (paper, HSM) é responsabilidade do operador. Ver ADR 0020.
- Cache de DEK em memória (TTL 5min) — `zero(dek)` após uso.
  Single-process assume IP de saída compartilhado (C10).
- `cmd/server rotate-kek` é **offline** (não roda junto com `serve`).
  Pode ser executado em janela de manutenção.

---

## Fase 10 — DB-backed system_settings (substitui env vars)

> **Issue #177.** Decisão arquitetural: **NÃO usar env vars para
> app-level config**. Tudo o que for configuração de runtime (ex.:
> `whatsmeow.enabled`, `whatsmeow.device_dsn`, `ffmpeg.concurrency`,
> `bus.inbound.buffer`, `reconcile.interval`) vive na tabela
> `system_settings`, cifrada com a master KEK, editável via admin
> panel (`/admin/settings`).

### O que mudou (vs. Fase 7–9)

| Antes (env var) | Agora (system_settings) | Por quê |
|---|---|---|
| `MEZ_WHATSMEOW_ENABLED` | `whatsmeow.enabled` (bool) | editável em runtime sem restart |
| `MEZ_WHATSMEOW_DEVICE_DSN` | `whatsmeow.device_dsn` (string, cifrado) | sem secrets em env |
| `MEZ_WHATSMEOW_IDENTITY_KIND` | `whatsmeow.identity.kind` (string) | hot-reload de anti-ban profile |
| `MEZ_WHATSMEOW_IDENTITY_OS` | `whatsmeow.identity.os` (string) | hot-reload de anti-ban profile |
| `MEZ_FFMPEG_CONCURRENCY` | `ffmpeg.concurrency` (int) | scale up sem redeploy |
| `MEZ_BUS_INBOUND_BUFFER` | `bus.inbound.buffer` (int) | tuning sem redeploy |
| `MEZ_BUS_OUTBOUND_BUFFER` | `bus.outbound.buffer` (int) | tuning sem redeploy |
| `MEZ_RECONCILE_INTERVAL` | `reconcile.interval` (string) | tuning sem redeploy |

### Env vars de bootstrap (mínimo aceitável)

As seguintes env vars **continuam obrigatórias** (bootstrap mínimo —
o app precisa delas ANTES de poder ler qualquer config dele mesmo):

```bash
MEZ_DATABASE_URL             # DSN do app pool (mez_app role)
MEZ_MIGRATE_DATABASE_URL     # DSN do migrate role
MEZ_PLATFORM_DATABASE_URL    # DSN do platform role (rotates KEK)
MEZ_MASTER_KEY               # KEK (32 bytes, base64) — pode vir de arquivo/vault
MEZ_API_JWT_SECRET           # HS256 secret (32+ chars)
MEZ_SESSION_SECRET           # session secret (32+ chars)
MEZ_S3_ENDPOINT              # endpoint S3-compatível
MEZ_S3_ACCESS_KEY            # S3 access key
MEZ_S3_SECRET_KEY            # S3 secret
MEZ_S3_BUCKET                # bucket de mídia
MEZ_S3_BACKUP_BUCKET         # bucket de backup
```

Próximas iterações (issue #160) vão migrar essas para `config.yaml` cifrado.

### Como funciona o cifrado (diferente de channel_credentials)

| Aspecto | `channel_credentials` (Fase 7) | `system_settings` (Fase 10) |
|---|---|---|
| Granularidade | por (tenant, channel) | platform-wide |
| Modelo | DEK por tenant, wrappada por KEK | cifra **direto** com a KEK |
| API | `Keyring.Encrypt(tenant, ...)` | `Envelope.SealSystem(plaintext)` |
| Por que? | tenants têm chaves isoladas (revogação) | settings são globais (não há "tenant de system config") |

```go
// pkg/crypto/envelope.go
func (e *Envelope) SealSystem(plaintext []byte) ([]byte, error)   // AES-256-GCM(KEK, plaintext)
func (e *Envelope) OpenSystem(ciphertext []byte) ([]byte, error) // verifica auth tag
```

### Hot-reload via Watch

`settings.Service.Watch(ctx)` retorna `<-chan SystemSettingEvent`.
Quando `Set()` é chamado, todos os watchers recebem o evento.
**Limitação atual (issue #160 sub-issue B):** mudanças em
`whatsmeow.*` logam "restart required" — o reconnect per-tenant sem
restart é trabalho futuro.

```go
go func() {
    for ev := range settingsSvc.Watch(ctx) {
        if strings.HasPrefix(ev.Key, "whatsmeow.") {
            log.Warn("whatsmeow.* setting changed; restart required for full effect", "key", ev.Key)
        }
    }
}()
```

### RLS fail-closed (C3+C4)

| Role | Permissão | Por quê |
|---|---|---|
| `mez_app` | SELECT | app lê config no boot, **não escreve** |
| `mez_platform` | ALL (SELECT/INSERT/UPDATE/DELETE) | admin / jobs de management |
| `mez_migrate` | ALL | migrations + `SeedDefaults` inicial |

`FORCE ROW LEVEL SECURITY` está ativo — `BYPASSRLS` não é suficiente,
RLS é obrigatória mesmo para owners.

### Como setar uma setting (3 caminhos)

#### 1. Admin panel (recomendado para dev)

```bash
# Inicia o app e acessa:
open http://localhost:8080/admin/settings
# Lista todas as settings, edita in-place, audit log automático.
```

#### 2. SQL direto (apenas dev/CI)

```sql
-- O valor precisa ser cifrado PRIMEIRO (a app não expõe o KEK):
-- Use `mez-go-mono settings encrypt "true"` ou o script abaixo.
INSERT INTO system_settings (key, value_encrypted, description, updated_by)
VALUES (
    'whatsmeow.enabled',
    decode('base64-ciphertext', 'base64'),
    'Liga o canal WhatsApp Web real',
    'admin@local'
);
```

#### 3. Via API (futuro — issue #160 sub-issue A)

```bash
curl -X POST http://localhost:8080/api/admin/settings/whatsmeow.enabled \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"value": true, "description": "Liga o canal WhatsApp Web real"}'
```

### Como adicionar uma nova setting

1. **Adicione o default em `internal/usecase/settings/service.go`** (função `SeedDefaults`).
2. **Use `settings.Service.Get(ctx, key, &dst, default)` no wire-up.**
3. **Documente no `settings.templ`** (a UI mostra automaticamente).
4. **Não adicione env var** — em vez disso, defina no DB e remova o `os.Getenv` correspondente.

```go
// Padrão de uso:
var enabled bool
if err := settingsSvc.Get(ctx, "whatsmeow.enabled", &enabled, false); err != nil {
    return fmt.Errorf("read whatsmeow.enabled: %w", err)
}
```

### Auditoria

Toda mudança em `system_settings` é logada em `admin_audit_log` com
`action='setting.update'` e `metadata={key, old_hash, new_hash,
actor_email}`. (Ver issue #160 sub-issue C para hardening completo.)

```sql
SELECT created_at, actor_email, metadata
FROM admin_audit_log
WHERE action = 'setting.update'
ORDER BY created_at DESC LIMIT 50;
```

### Migração entre versões da KEK

Quando a KEK rotaciona, o `cmd/server rotate-kek` re-cifra **todos**
os valores de `system_settings` no mesmo loop em que re-cifra
`channel_credentials`. O `settings.Service.InvalidateCache()` é chamado
no fim para forçar releitura.

---

## Config

```bash
MEZ_HTTP_ADDR=:8080
MEZ_DATABASE_URL=postgres://mez_app@localhost/mez
MEZ_MIGRATE_DATABASE_URL=postgres://mez_migrate@localhost/mez
MEZ_PLATFORM_DATABASE_URL=postgres://mez_platform@localhost/mez
MEZ_MASTER_KEY=$(openssl rand -base64 32)
MEZ_S3_ENDPOINT=http://localhost:9000
MEZ_OIDC_ISSUER=https://accounts.google.com
MEZ_SESSION_SECRET=$(openssl rand -base64 32)
```

```bash
docker compose -f deployments/docker-compose.yml up -d
```

### Migration 0007 (Fase 10)

Aplica a tabela `system_settings` com RLS fail-closed. **Idempotente**
(usa `IF NOT EXISTS` em tudo). Aplicar com:

```bash
MEZ_MIGRATE_DATABASE_URL="postgres://mez_migrate:mez_dev_pass@localhost:5432/mezgo?sslmode=disable" \
    make migrate-up
```

Verificação manual:

```sql
\d system_settings
-- Espera ver: pkey em key, 3 policies (migrate/platform/app-read),
-- FORCE RLS ativo.

SELECT * FROM system_settings;
-- Espera 0 rows (defaults são seedados pelo código Go, não pela migration).
```

Após migrate, inicialize os defaults rodando a app uma vez:

```bash
./bin/mez-go-mono serve
# Logs esperam: "settings: seeded 8 defaults" (na primeira boot).
```

---

*Última atualização: junho/2026. Fase 0–10 (Fase 10 in progress — PR #159 aberto).*
