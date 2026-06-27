# AGENTS.md — Guia Operacional para mez-go-mono

> **Quick-start para agentes IA trabalhando no mez-go-mono.**
> Leia `CLAUDE.md` (quando criado) para a visão arquitetural completa.
> Este arquivo é a referência rápida de operação.

---

## Identidade do Projeto

- **Repositório:** `github.com/felipedsvit/mez-go-mono`
- **Descrição:** Gateway omnichannel de mensageria em um único binário Go.
- **Status:** Fase 0 + 1 + 2 merged em `main` (12.000+ LOC). Fases 3–8 em planejamento.
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

---

*Última atualização: junho/2026. Fase 0–2 merged.*
