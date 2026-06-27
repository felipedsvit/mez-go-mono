# Fase 7 — Hardening (envelope encryption + rotate-kek + CI + ADRs)

> **Status:** planejamento aprovado (junho/2026) · **Em andamento**.
> **Escopo:** 8 issues (#88–#95) · ~2.9d solo estimado · single commit (squash) em `fase7-squash` → `main`.
> **Pré-requisitos:** Fases 0, 1, 2, 3, 4, 5 e 6 merged.
> **Base de reuso:** `pkg/crypto/envelope.go` (Fase 0) + `internal/core/port/sealer.go` (Fase 0) + `migrations/0001 channel_credentials` (Fase 0) + `RunAsPlatform` (Fase 1, C5) + `adminrepo.AuditRepo` (Fase 1).

---

## 1. Análise do projeto pai (mez-go) e carryover do mono

A Fase 7 ativa o **envelope encryption** (C9) end-to-end: credenciais de
canal (Meta tokens, Telegram bot tokens) deixam de viver em variáveis de
ambiente (`MEZ_*_CREDENTIALS`, carryover da Fase 3) e passam a ser cifradas
por tenant (DEK/tenant wrapped por KEK) na tabela `channel_credentials`. A
`Keyring` decifra em runtime com cache de DEK em memória (TTL 5min). A
operação `cmd/server rotate-kek` re-wrap de todos os DEKs sem perda
(auditada, dry-run, recovery via `kek_version` + `rotation_window`).

### 1.1 Inventário de código reusável (mono, carryover)

| Componente | Caminho | LOC | Issue destino | Tipo |
|---|---|---:|---|---|
| `crypto.Envelope` (AES-256-GCM, wrap/unwrap DEK) | `pkg/crypto/envelope.go` | 129 | #88 | reuso direto |
| `port.Sealer` / `port.Encryptor` (interfaces) | `internal/core/port/sealer.go` | 21 | #88 | reuso direto + renomear `Keyring` → `Encryptor` |
| Tabela `channel_credentials` (wrapped_dek + encrypted) | `migrations/0001_init.up.sql:152` | schema | #90 | reuso direto |
| `RunAsPlatform` auditado (cross-tenant, C5) | `internal/adapter/repository/postgres/db.go:146` | 47 | #92 | reuso direto |
| `adminrepo.AuditRepo.Record/RecordWithTx` | `internal/adapter/repository/postgres/admin/audit_repo.go` | 120 | #92 | reuso direto |
| `config.Config.MasterKey/MasterKeyFile` | `pkg/config/config.go:78-89` | 12 | #92 | reuso direto |
| `EnvCredentials` (env-based, **a ser removido**) | `internal/adapter/webhook/secrets/credentials.go` | 176 | #91 | **substituído + deletado** |

### 1.2 Referência semântica no pai (`mez-go`)

| Componente (pai) | Caminho | LOC | Aprendizado para mono |
|---|---:|---:|---|
| `LocalSealer` (pai usa KEK = DEK, sem DEK/tenant) | `mez-go/internal/adapter/secret/sealer/local.go` | 60 | mono **NÃO** segue o flat-envelope do pai: mono usa DEK/tenant real (C9), `wrapped_dek` separado de `encrypted` |
| `VaultSealer` (pós-1.0) | `mez-go/internal/adapter/secret/sealer/vault.go` | n/a | mono **NÃO** porta (decisão §2 + §22) — interface `Sealer` permite plugar pós-1.0 |
| `pkg/crypto.Cipher` (pai, sem DEK) | `mez-go/pkg/crypto/cipher.go` | n/a | mono já tem `Envelope` mais sofisticado (Fase 0) |

### 1.3 Patterns obrigatórios (do AGENTS.md, mantidos)

1. **RLS via context, nunca parâmetro** — `ChannelCredentialsRepo` recebe `tenantID` apenas via `RunInTenantTx`.
2. **FORCE RLS** (C3) — `channel_credentials` já está na migration 0001; `kek_version` herdará a mesma policy.
3. **Functional options** — `NewKeyring(WithRepo, WithSealer, WithCacheTTL)`.
4. **Audit log em toda ação admin** (D17) — `RotateKEK` registra por (tenant, channel).
5. **Comentários português** — manter consistência com pai e mono.
6. **Sem imports proibidos** — guardrails: sem `sink/`, `broker/nats`, `pkg/shard`, `cache/redis`, `secret/sealer/vault`.

### 1.4 Divergências arquiteturais pai → mono

| Aspecto | mez-go (pai) | mez-go-mono (Fase 7) | Impacto |
|---|---|---|---|
| **Envelope** | flat (KEK cifra diretamente, sem DEK) | **DEK/tenant** wrapped por KEK | Cifra credencial com DEK do tenant; wrap DEK com KEK; persiste `wrapped_dek` e `encrypted` |
| **Backend sealer** | Local + Vault Transit (2 impls) | **Local only** (pós-1.0 Vault fica opcional) | Implementa só `LocalSealer`; interface `Sealer` permite plugar `VaultTransitSealer` depois |
| **Credenciais storage** | variáveis de ambiente (legado) | **DB only** (`channel_credentials`) | **Remove** `EnvCredentials`; resolve via `Keyring.Encrypt/Decrypt` |
| **KEK rotation** | Vault Transit rotaciona a chave-mestra nativamente; pai não tem rotação KEK local | **Operação offline** `cmd/server rotate-kek` | Usecase `RotateKEK` itera `RunAsPlatform`, re-wrap por tenant, audit log por linha |
| **KEK versionamento** | Vault Transit mantém múltiplas versões | **Coluna `kek_version`** + `rotation_window_until` em `channel_credentials` (migration 0006) | Keyring escolhe DEK certo durante window de rotação |
| **Audit** | Audit centralizado | Audit centralizado + audit específico de `rotate_kek` por tenant | `RotateKEK` registra 1 linha por (tenant, channel) em `audit_log` |
| **Testes** | `local_test.go` cobre Seal/Open | mono: cobre Seal + Wrap/Unwrap + Keyring round-trip + rotate-kek | `pkg/crypto/envelope_test.go` mantém; adiciona `internal/adapter/crypto/local_sealer_test.go` e `tests/secrets/integration_test.go` |

### 1.5 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| **NEW** (LocalSealer + Keyring + ChannelCredentialsRepo + RotateKEK + ADRs + tests) | ~1.700 | 1.8 |
| **REWRITE** (substituir `EnvCredentials` por `Keyring` em todos os providers + remover `MEZ_*_CREDENTIALS`) | ~500 | 0.5 |
| **MECHANICAL** (Makefile targets + wire.go + subcomando rotate-kek) | ~200 | 0.2 |
| **Buffer** (Fase 6 ~30% subestimou) | — | 0.4 |
| **Total** | **~2.400** | **~2.9** |

Mantém a estimativa de 2-3 dias do README §23.

---

## 2. Visão geral da Fase 7

Implementa o **envelope encryption ativo** end-to-end (C9): credenciais de
canal (Meta tokens, Telegram bot tokens) deixam de viver em env vars e
passam a ser cifradas por tenant (DEK/tenant wrapped por KEK) na tabela
`channel_credentials`. A `Keyring` decifra em runtime, com cache de DEK em
memória (TTL 5min). A operação `cmd/server rotate-kek` re-wrap de todos os
DEKs sem perda (auditada, dry-run, recovery). CI valida `govulncheck` via
Makefile. 20 arquivos ADR documentam as decisões D1–D18 do README §5 (D2 e D5 têm sub-ADRs).

### A Fase 7 **NÃO** implementa

- Vault Transit sealer (pós-1.0, já é decisão §2).
- Rotação automática de DEK (apenas DEK manual por tenant é necessária no 1.0).
- Quórum de KEK (split-key, M-of-N) — single KEK no 1.0 (limitação assumida §25).
- Painel UI para inserir credenciais (handler stub em `/admin/tenants/:id/channels` é Fase 5b).

---

## 3. Issues

| Issue | Título | Arquivo(s) alvo | Origem | Classif. | Esforço | Bloq. |
|-------|--------|-----------------|--------|----------|--------:|-------|
| **#88** | `LocalSealer` + `port.Encryptor` (adapter de criptografia) | `internal/adapter/crypto/{local_sealer,local_sealer_test}.go` | `pkg/crypto/envelope.go` (carryover) + `port.Sealer/Encryptor` | NEW | 0.4d | — |
| **#89** | Migration `0006_kek_version.up/down.sql` | `migrations/0006_*.sql` | schema gap | NEW | 0.1d | — |
| **#90** | `ChannelCredentialsRepo` (`RunInTenantTx` + variante cross-tenant) | `internal/adapter/repository/postgres/{credentials_repo,credentials_repo_test}.go` + `internal/core/domain/credentials.go` | `migrations/0001 channel_credentials` + `RunAsPlatform` (Fase 1) | NEW | 0.5d | #89 |
| **#91** | Usecase `Keyring` (cifra/decifra por tenant) | `internal/usecase/secrets/{keyring,cache,keyring_test}.go` | compõe #88 + #90 | NEW | 0.5d | #88, #90 |
| **#92** | Usecase `RotateKEK` + subcomando `cmd/server rotate-kek` | `internal/usecase/secrets/rotate_kek.go` + `cmd/server/rotate_kek.go` (substituir stub) + `cmd/server/wire.go` | `RunAsPlatform` + `AuditRepo` | REWRITE | 0.5d | #90, #91 |
| **#93** | `Makefile` alvos `govulncheck` + `openapi-validate` + `rotate-kek` | `Makefile` | CI já chama direto; falta alvo | MECHANICAL | 0.1d | — |
| **#94** | ADRs D1–D18 (20 arquivos) + docs finais | `docs/adr/0001..0020-*.md` + `README.md` §23 + `AGENTS.md` + `CLAUDE.md` | README §3 (C1–C12) + §5 (D1–D18) | NEW | 0.4d | — |
| **#95** | Testes E2E testcontainers (rotate-kek zero-loss, keyring round-trip) | `tests/secrets/{keyring,rotate_kek}_test.go` | testcontainers carryover Fase 2/6 | NEW | 0.4d | #92 |

**Total:** 8 issues · 2.9d solo · ~2.400 LOC.

---

## 4. Detalhamento por issue

### #88 — LocalSealer + port.Encryptor

**Arquivos:**
- `internal/adapter/crypto/local_sealer.go` (NOVO, ~100 LOC)
  - `type LocalSealer struct { env *crypto.Envelope }`
  - `func NewLocalSealer(masterKeyB64 string) (*LocalSealer, error)` — wrap `crypto.NewEnvelope`
  - `func (s *LocalSealer) Wrap(ctx context.Context, plaintext []byte) ([]byte, error)` — delega `env.wrapDEK`; satisfaz `port.Sealer`
  - `func (s *LocalSealer) Unwrap(ctx context.Context, wrapped []byte) ([]byte, error)` — delega `env.unwrapDEK`; satisfaz `port.Sealer`
  - `func (s *LocalSealer) EncryptForTenant(wrappedDEK, plaintext []byte) ([]byte, error)` — `env.Encrypt`; satisfaz `port.Encryptor`
  - `func (s *LocalSealer) DecryptForTenant(wrappedDEK, ciphertext []byte) ([]byte, error)` — `env.Decrypt`; satisfaz `port.Encryptor`
  - **NÃO** exporta `Name()` ou `Ref()` (mono não tem mistura de backends no 1.0)
- `internal/adapter/crypto/local_sealer_test.go` (NOVO, ~150 LOC)
  - Wrap/Unwrap round-trip
  - Encrypt/Decrypt with random wrapped DEK
  - Erro com KEK inválida (length != 32)
  - Erro com wrapped DEK adulterado (GCM auth tag)

**Padrões:** port adapter puro — satisfaz `port.Sealer` + `port.Encryptor` sem lógica de cache ou repo.

**DoD #88:**
- [ ] `port.Keyring` **renomeada** para `port.Encryptor` em `internal/core/port/sealer.go` com métodos `EncryptForTenant`/`DecryptForTenant`.
- [ ] `LocalSealer` satisfaz `port.Sealer` E `port.Encryptor` (compilação: `var _ port.Sealer = (*LocalSealer)(nil); var _ port.Encryptor = (*LocalSealer)(nil)`).
- [ ] Tests: round-trip + tampering + invalid key.
- [ ] `make test` verde, `-race -shuffle=on`.

---

### #89 — Migration `0006_kek_version`

**Arquivos:**
- `migrations/0006_kek_version.up.sql` (NOVO, ~30 LOC)
  ```sql
  BEGIN;
  ALTER TABLE channel_credentials
      ADD COLUMN IF NOT EXISTS kek_version INT NOT NULL DEFAULT 1;
  ALTER TABLE channel_credentials
      ADD COLUMN IF NOT EXISTS rotation_window_until TIMESTAMPTZ;
  -- Index para o reconciler de rotação
  CREATE INDEX IF NOT EXISTS idx_channel_credentials_kek_version
      ON channel_credentials(kek_version) WHERE rotation_window_until IS NOT NULL;
  -- mez_platform precisa ler/escrever para RunAsPlatform em rotate-kek
  GRANT SELECT, INSERT, UPDATE, DELETE ON channel_credentials TO mez_platform;
  COMMIT;
  ```
- `migrations/0006_kek_version.down.sql` (NOVO, ~5 LOC)

**DoD #89:**
- [ ] `make migrate-up` verde local.
- [ ] `migrate -database ... version` mostra 6.
- [ ] `channel_credentials` tem `kek_version=1` em todas as linhas existentes.

---

### #90 — ChannelCredentialsRepo

**Arquivos:**
- `internal/adapter/repository/postgres/credentials_repo.go` (NOVO, ~200 LOC)
  - `type ChannelCredentialsRepo struct { appPool, platformPool *pgxpool.Pool; tx *TxRunner }`
  - `Get(ctx, tenantID, channel) (*ChannelCredentials, error)` — via `RunInTenantTx`
  - `Upsert(ctx, tenantID, channel, wrappedDEK, encrypted)` — via `RunInTenantTx`
  - `Delete(ctx, tenantID, channel)` — via `RunInTenantTx`
  - `ForEachTenant(ctx, fn func(tenantID, channel, wrappedDEK, kekVersion) error) error` — via `RunAsPlatform(actor="system:rotate-kek", …)`, cross-tenant
  - `UpdateWrappedDEK(ctx, tenantID, channel, newWrappedDEK, newKekVersion) error` — `mez_platform` para rotação; audit
- `internal/core/domain/credentials.go` (NOVO, ~30 LOC)
  - `type ChannelCredentials struct { TenantID string; Channel string; WrappedDEK []byte; Encrypted []byte; KEKVersion int; RotationWindowUntil *time.Time }`
- `internal/adapter/repository/postgres/credentials_repo_test.go` (NOVO, ~200 LOC)
  - Get/Upsert/Delete happy path
  - RLS: tenant A não lê tenant B (fail-closed)
  - `ForEachTenant` itera todos os tenants via `RunAsPlatform`
  - Testcontainers Postgres (carryover)

**DoD #90:**
- [ ] Repo com testes unitários + integração.
- [ ] `RunInTenantTx` everywhere exceto `ForEachTenant` (cross-tenant).
- [ ] Audit log automático em `RunAsPlatform`.

---

### #91 — Usecase Keyring

**Arquivos:**
- `internal/usecase/secrets/cache.go` (NOVO, ~80 LOC)
  - `type dekCache struct { mu sync.Mutex; m map[string]dekEntry }`
  - `dekEntry{ dek []byte; wrappedDEK []byte; kekVersion int; expiresAt time.Time }` — TTL configurável, default 5 min
  - `Get(tenantID string) (dek, wrappedDEK []byte, kekVersion int, ok bool)`
  - `Put(tenantID string, dek, wrappedDEK []byte, kekVersion int)`
  - `Invalidate(tenantID string)` — zera entry e chama `zero(dek)` para reduzir janela de exposição em memória
- `internal/usecase/secrets/keyring.go` (NOVO, ~180 LOC)
  - `type Keyring struct { repo CredentialsRepository; sealer port.Encryptor; cache *dekCache; log zerolog.Logger }`
  - `New(repo CredentialsRepository, sealer port.Encryptor, opts ...KeyringOption) *Keyring`
  - `func (k *Keyring) Encrypt(ctx context.Context, tenantID, channel string, plaintext []byte) ([]byte, error)` — get-or-create DEK + cache + `sealer.EncryptForTenant`
  - `func (k *Keyring) Decrypt(ctx context.Context, tenantID, channel string, ciphertext []byte) ([]byte, error)` — cache lookup + `sealer.DecryptForTenant`
  - `func (k *Keyring) ResolveCredentials(ctx context.Context, tenantID, channel string) (*domain.ChannelCredentials, error)` — decifra e devolve struct tipada
  - `func (k *Keyring) SetCredentials(ctx context.Context, tenantID, channel string, plaintext []byte) error` — gera DEK + wrap + `repo.Upsert` + `cache.Invalidate`
  - `func (k *Keyring) Invalidate(tenantID string)` — delega a `cache.Invalidate`; chamado por `SetCredentials` e pelo `InvalidateFn` de `RotateKEK`
- `internal/usecase/secrets/keyring_test.go` (NOVO, ~250 LOC)
  - Set → Resolve round-trip
  - Cache hit/miss
  - TTL expiry
  - Tenant A não decifra credenciais de tenant B
  - Erro quando credencial não configurada (`ErrCredentialsNotFound`)

**DoD #91:**
- [ ] `Keyring.ResolveCredentials` retorna `ErrCredentialsNotFound` quando ausente (consistente com `EnvCredentials`).
- [ ] `cache.Invalidate` é chamado em `SetCredentials`; `Invalidate(tenantID)` é o callback entregue ao `RotateKEK` via `InvalidateFn`.
- [ ] `Keyring` **não** implementa nenhuma interface `port.*` — é orquestrador concreto, não port adapter.
- [ ] Testes `-race` verdes.

---

### #92 — RotateKEK + subcomando

**Arquivos:**
- `internal/usecase/secrets/rotate_kek.go` (NOVO, ~250 LOC)
  ```go
  type RotateKEKOpts struct {
      OldKEK       []byte
      NewKEK       []byte
      DryRun       bool
      Actor        string
      BatchSize    int
      InvalidateFn func(tenantID string) // nil = no-op; injeta keyring.Invalidate em prod
  }

  func Rotate(ctx context.Context, repo ChannelCredentialsRepository,
      auditRepo AuditRepository, opts RotateKEKOpts) (Report, error)
  ```
  - Algoritmo:
    1. Validar `len(OldKEK) == 32` e `len(NewKEK) == 32`.
    2. `repo.ForEachTenant(ctx, opts.Actor, fn)` — `ForEachTenant` já abre `RunAsPlatform`
       internamente; `Rotate` **não** aninha outra tx platform.
       Para cada `(tenantID, channel string, wrappedDEK []byte, kekVersion int)`:
       a. `oldSealer.Unwrap(ctx, wrappedDEK)` → DEK em claro; `defer zero(dek)`.
       b. `newSealer.Wrap(ctx, dek)` → `newWrappedDEK`.
       c. Se `DryRun`: registrar no `Report`, **não** persistir; continuar.
       d. `repo.UpdateWrappedDEK(ctx, tenantID, channel, newWrappedDEK, kekVersion+1)`.
       e. `auditRepo.Record("rotate_kek_tenant", meta{tenant, channel, oldVersion, newVersion})`.
       f. `if opts.InvalidateFn != nil { opts.InvalidateFn(tenantID) }` — expurga DEK do cache em memória para forçar re-fetch com `newWrappedDEK` no próximo `Encrypt`.
       g. **NÃO** toca `encrypted` — o DEK (chave de dados) não muda; só o wrap da KEK muda.
       h. Erros por linha são coletados em `Report.Errors` (não abortam o lote).
    3. `auditRepo.Record("rotate_kek_complete", meta{tenants, channels, errors})`.
    4. Retorna `Report{Tenants, Channels, DurationMs, Errors}`.
- `cmd/server/rotate_kek.go` (REESCRITA do stub, ~80 LOC)
  - Parse `os.Args`: `rotate-kek [--dry-run] [--actor <email>] [--json]`.
  - Lê `MEZ_MASTER_KEY` + `MEZ_MASTER_KEY_NEW`; ou variantes `_FILE`.
  - Valida ambas (base64 decode → 32 bytes cada).
  - Escreve audit `"rotate_kek_started"` **antes** de chamar `Rotate` (via `auditRepo.Record`).
  - Constrói `RotateKEKOpts{..., InvalidateFn: keyring.Invalidate}`.
  - Chama `secrets.Rotate(ctx, repo, auditRepo, opts)`.
  - Imprime relatório tabular (`text/tabwriter`) ou JSON (`--json`).
  - Exit code: `0` sucesso total · `1` erro parcial (`Report.Errors > 0`) · `2` erro total (validação/panic).
- `cmd/server/wire.go` (UPDATE, ~20 LOC)
  - Inicializa `LocalSealer` a partir de `cfg.MasterKey`.
  - Inicializa `Keyring` com `repo` + `sealer` + cache TTL.
  - Inicializa dependências de `rotate-kek` (lazy — só ativo no subcomando).
  - **Substitui** `secrets.EnvCredentials` por `*usecase/secrets.Keyring` em `providerregistry.Build(...)`.

**DoD #92:**
- [ ] `cmd/server rotate-kek --dry-run` lista o que seria feito, sem alterar nada.
- [ ] `cmd/server rotate-kek` real re-wrap de 100% dos (tenant, channel) com sucesso.
- [ ] Após rotação, `Keyring.Decrypt` continua funcionando (DEK igual, só wrap mudou).
- [ ] `Keyring.Encrypt` pós-rotação usa o novo `wrappedDEK` (cache foi invalidado por `InvalidateFn`).
- [ ] Audit log: audit `rotate_kek_started` (CLI) + 1 linha `rotate_kek_tenant` por (tenant, channel) + 1 linha `rotate_kek_complete`.
- [ ] Erro em 1 tenant NÃO derruba o lote (continue com próximo); relatório final mostra `Errors: N`.

---

### #93 — Makefile targets

**Arquivo:** `Makefile` (UPDATE, +15 LOC)
```makefile
.PHONY: ... govulncheck openapi-validate rotate-kek ...

govulncheck:
	$(GO) install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	govulncheck ./...

openapi-validate: openapi-gen
	git diff --exit-code api/openapi.gen.go

rotate-kek:
	$(GO) run ./cmd/server rotate-kek --actor "operator:$${USER:-unknown}"
```

**DoD #93:**
- [ ] `make govulncheck` verde (zero vulnerabilidades).
- [ ] `make openapi-validate` exit 0 quando spec sincronizado.
- [ ] `make rotate-kek` disponível (reuso de env vars).

---

### #94 — ADRs D1–D18

**Diretório:** `docs/adr/` (NOVO)
- 20 arquivos `0001-decisao-…md` (formato MADR; D1–D18, com D2 e D5 gerando sub-ADRs `d02b` e `d05b`):
  - 0001-d01-bus-tipada.md
  - 0002-d02-persist-before-2xx.md
  - 0003-d02b-reconciler-c1.md
  - 0004-d03-outbox-poll-fallback.md
  - 0005-d04-1-client-whatsmeow.md
  - 0006-d05-rls-run-in-tenant-tx.md
  - 0007-d05b-run-as-platform.md
  - 0008-d06-actions-aware.md
  - 0009-d07-capability-negotiation.md
  - 0010-d08-meta-webhook-unificado.md
  - 0011-d09-storage-s3.md
  - 0012-d10-graceful-shutdown.md
  - 0013-d11-migrate-embed.md
  - 0014-d12-openapi-codegen.md
  - 0015-d13-templ-htmx.md
  - 0016-d14-bootstrap-argonz-oidc.md
  - 0017-d15-sem-prefixo-api.md
  - 0018-d16-csrf-cookies.md
  - 0019-d17-audit-log.md
  - 0020-d18-envelope-encryption-local.md
- Cada um: **Contexto** / **Decisão** / **Consequências** (formato MADR, ~100-150 LOC cada, total ~1.800-2.200 LOC).

**Arquivos adicionais (docs finais):**
- `README.md` §23 — tabela de fases: marcar Fase 7 ✅; atualizar contador de LOC se cresceu.
- `AGENTS.md` — seção "Fase 7 — Hardening" com uso de `Keyring` + `rotate-kek`.
- `CLAUDE.md` — adicionar subseção "Envelope encryption (C9)" no índice de arquitetura.

**DoD #94:**
- [ ] 20 arquivos ADR em `docs/adr/` (18 decisões D1–D18; D2 e D5 com sub-ADRs).
- [ ] Cada ADR linka o item da tabela §5 do README.
- [ ] README §23 atualizado.

---

### #95 — Testes E2E

**Arquivos:**
- `tests/secrets/keyring_test.go` (NOVO, ~150 LOC)
  - testcontainers Postgres + 2 tenants × 4 canais = 8 credenciais
  - Set + Resolve round-trip para todos
  - Tenant A não resolve tenant B
  - Cache hit/miss com TTL
- `tests/secrets/rotate_kek_test.go` (NOVO, ~250 LOC)
  - 3 tenants × 4 canais
  - `RotateKEK` chamada via Go (não shell)
  - Validar: `wrapped_dek` mudou, `kek_version=2`, `encrypted` **inalterado** (DEK igual)
  - `Keyring.Decrypt` com newSealer funciona
  - Audit log: 12 linhas `rotate_kek_tenant` + 1 `rotate_kek_complete`
  - Dry-run não altera nada

**DoD #95:**
- [ ] `make test-integration` verde.
- [ ] Cobertura `internal/adapter/crypto` + `internal/usecase/secrets` ≥ 80%.

---

## 5. Definition of Done — Fase 7

- [ ] **#88–#95** mergeadas em `fase7-squash` → `main`.
- [ ] `make build` verde.
- [ ] `make test` verde (`-race -shuffle=on -count=1`).
- [ ] `make test-integration` verde (testcontainers).
- [ ] `make govulncheck` verde.
- [ ] `make openapi-validate` verde.
- [ ] `cmd/server rotate-kek` re-wrap sem perda, auditado.
- [ ] 20 arquivos ADR D1–D18 em `docs/adr/` (D2 e D5 com sub-ADRs).
- [ ] README §23 marca Fase 7 ✅; DoD global §24 revalidado (itens pós-Fase 7 marcados como tal).
- [ ] Zero novos imports proibidos (sem `sink/`, `broker/nats`, `pkg/shard`, `cache/redis`, `secret/sealer/vault`).
- [ ] Sem novas deps em `go.mod` (apenas stdlib + libs já usadas).
- [ ] PR único: `Fase 7: hardening — envelope encryption ativo + rotate-kek + CI govulncheck + ADRs (#88..#95)`.
- [ ] Branch: `fase7-squash`. Base: `main`. Body: tabela de issues, DoD, `Closes #88..#95`.

---

## 6. Riscos da Fase 7

| Risco | Severidade | Mitigação |
|-------|:----------:|-----------|
| Remover `EnvCredentials` quebra dev quickstart | média | `AGENTS.md` atualizado com nova forma (`POST /admin/tenants/:id/channels` ou seed); seed script opcional (Fase 5b) |
| `rotate-kek` em produção com crash no meio | média | Versionamento (`kek_version` + `rotation_window_until`); keyring escolhe DEK certo durante window; operator pode re-rodar |
| `wrapped_dek` vazar em logs | baixa | Adicionar `MuteBytes` helper; nunca logar `wrapped_dek` ou `encrypted` em claro (apenas `kek_version`) |
| Cache de DEK em memória = vetor de extração | média | TTL 5min + `zero(dek)` após uso; aceitar limitação do modelo single-process |
| `govulncheck` falha por dependência transitiva | baixa | Fix semanal; `make govulncheck` permite `--no-fail` em dev |

---

## 7. Próximos passos (Fase 8 — Estabilização do processo único)

- Boot determinístico + graceful shutdown coordenado (C12) — já parcialmente em `cmd/server/wire.go:runWithGracefulShutdown`.
- Testes de chaos (`kill -9` em pontos críticos) — `tests/chaos/`.
- Teste de boot frio com N tenants (warm-up paralelo) — `tests/boot/`.
- `migrate` fail-fechada no entrypoint — `deployments/entrypoint.sh`.

---

*Este plano é vivo: revisitar ao fim da fase e ajustar prazos/complexidade com base no que o código ensina.*
