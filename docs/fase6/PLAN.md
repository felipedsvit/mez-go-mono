# Fase 6 — Backup / Restore / Reset (por-tenant lógico)

> **Status:** planejamento aprovado (junho/2026).
> **Escopo:** 8 issues (#81–#88) · ~5-7 dias estimados · single commit (squash) em `fase6-squash` → `main`.
> **Pré-requisitos:** Fases 0, 1, 2, 3, 4 e 5 merged.
> **Base de reuso:** `mez-go/internal/usecase/backup/` (pai) + `pg_dump` patterns + `RunInTenantTx` (mono Fase 0).

---

## 1. Análise do projeto pai (mez-go)

A Fase 6 do `mez-go-mono` implementa **export/import/restore/reset por-tenant**
**sem usar `pg_dump --tenant`** (que não existe e afetaria todos os tenants).
Tudo via `RunInTenantTx` (RLS garante o recorte).

### 1.1 Inventário de código reusável (porte mecânico + extensão)

| Componente do pai | Caminho | LOC | Issue destino | Tipo de porte |
|---|---|---:|---|---|
| Export (COPY-por-tenant, REPEATABLE READ) | `mez-go/internal/usecase/backup/export.go` | ~300 | #81 | **mecânico** + adaptação (sem schema dump) |
| Restore (replay migrations, idempotente) | `mez-go/internal/usecase/backup/restore.go` | ~400 | #82 | **mecânico** + ordem topológica |
| Reset (DELETE por-tenant, sem TRUNCATE) | `mez-go/internal/usecase/backup/reset.go` | ~150 | #83 | **mecânico** |
| Storage S3 (multipart upload) | `mez-go/internal/adapter/storage/s3/` | ~150 | #84 | **mecânico** |
| API endpoints (`/backup`, `/restore`, `/reset`) | `mez-go/cmd/mez-core/admin_tenants_backup.go` | ~200 | #85 | **mecânico** |
| UI adminweb (htmx polling de progresso) | `mez-go/cmd/mez-core/admin_tenants_backup_ui.go` | ~250 | #86 | **mecânico** |
| UI reset (confirmação dupla) | `mez-go/cmd/mez-core/admin_tenants_reset.go` | ~150 | #87 | **mecânico** |
| Test round-trip (export → reset → restore → diff) | `mez-go/internal/usecase/backup/restore_test.go` | ~300 | #88 | **mecânico** + adapta testcontainers |

**LOC reusáveis:** ~1.900 LOC.
**LOC genuinamente novos:** ~600 LOC (UI htmx, integration com RunAsPlatform).

### 1.2 Patterns obrigatórios (do pai, mantidos em mez-go-mono)

Do `README.md` §13 e do pai `mez-go/internal/usecase/backup/`:

1. **Multi-tenant via RLS, não filtragem app-side.** Já em vigor.
2. **Atomic dedup** (Fase 2). Já em vigor.
3. **Outbox pattern** (Fase 3). Já em vigor.
4. **Outbound action-aware** (D6). Já em vigor.
5. **`FORCE ROW LEVEL SECURITY`** + 3 roles (C3). Já em vigor.
6. **`RunAsPlatform` auditado** (C5) — usado para reset cross-tenant. **A aplicar em #83.**
7. **FKs deferíveis** (C6, migration 0003). **Aplicar em #82** (ordem topológica no restore).
8. **Replay de migrations no restore** (C7) — manifesto carrega `schema_version`. **Aplicar em #82.**
9. **Tx `REPEATABLE READ`** (C8) — snapshot consistente do DB durante export. **Aplicar em #81.**
10. **Audit log de toda ação admin** (D17). **Aplicar em #83, #85.**
11. **`templ + htmx`** (D13). **Aplicar em #86, #87.**
12. **CSRF** (D16). **Aplicar em #85.**
13. **Backup/Restore NÃO usa `pg_dump --tenant`** (não existe + afetaria outros tenants). **Aplicar em #81, #82.**
14. **Reset NÃO usa `TRUNCATE`** (atingiria todos os tenants). **Aplicar em #83.**

### 1.3 Divergências arquiteturais entre pai e mez-go-mono

| Aspecto | mez-go (pai) | mez-go-mono | Impacto na Fase 6 |
|---|---|---|---|
| Schema dump | Sim (pg_dump via sub-shell) | **NÃO** (Fase 6: sem schema dump, só dados) | Migration replay no restore (#82) |
| RLS cross-tenant | `SECURITY DEFINER` | **`RunAsPlatform`** (C5, mez_app sem BYPASSRLS) | Reset usa `RunAsPlatform` cross-tenant (#83) |
| Mídia no backup | tar S3 + DB em paralelo | **Idem** — S3 stream + DB NDJSON | #84 + #81 |
| Histórico de versões | Sim (timestamps) | **Sim** (mesmo padrão) | Backup ID + schema_version (#81) |
| S3 lib | `minio-go/v7` | **Idem** (mono já importa) | Mecânico (#84) |
| **Single-box blast radius** (C10) | N/A (multi-binário) | Backup roda no mesmo binário → bounded resources | Pool separado (carryover) |
| Schema manifests | Carregado via env (migrations path) | `migrations/` (embed via Fase 0) | Replay direto |
| `REPEATABLE READ` caveats (C8) | Documentado | **Idem** — VACUUM/bloat durante backup | Documentado em PLAN |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| Porte mecânico (export + restore + reset + S3) | ~1.000 | 2.0 |
| Reescrita parcial (RunAsPlatform + ordem topológica + CSRF) | ~600 | 1.5 |
| Genuinamente novo (UI htmx + htmx-ext-ws progresso + integração RunAsPlatform) | ~600 | 1.5 |
| Tests E2E (round-trip com testcontainers) | ~300 | 1.0 |
| Buffer (Fases 4-5 subestimaram ~30%) | — | 0.5 |
| **Total** | **~2.500** | **~6.5** |

Mantém a estimativa de 5-7 dias do README §23 (realocado de 2-3).

---

## 2. Visão geral da Fase 6

Implementa **export/import/restore/reset por-tenant** end-to-end:

```
POST /admin/tenants/:id/backup    → cria job async, export NDJSON por tabela em tx REPEATABLE READ
                                   + tar S3 tenants/<id>/
                                   + manifest com schema_version
                                   → retorna backup_id (htmx polling de progresso)

POST /admin/tenants/:id/restore  → lê backup do S3, replay migrations sobre o backup
                                   + upsert por linha em ordem topológica (FKs deferidas)
                                   → idempotente (mesmo backup aplicado 2x = mesmo estado)

POST /admin/tenants/:id/reset    → confirmação dupla ("RESET" + senha admin)
                                   + disconnect whatsmeow ANTES do delete
                                   + DELETE por tabela na tenant-tx
                                   + delete prefixo S3 tenants/<id>/
                                   + audit log + WS broadcast (força logout)
```

A Fase 6 **NÃO** implementa:
- Cross-tenant backup (admin global; Fase 7)
- Backup incremental (Fase 7+)
- Restore em tenant novo (target ≠ source) — Fase 7
- Encriptação at-rest no S3 (Fase 7 com LocalSealer para SSE-C)

---

## 3. Correções arquiteturais cobertas

| Correção | Descrição | Issues |
|---|---|---|
| **C5** | `RunAsPlatform` cross-tenant auditado para reset | #83 |
| **C6** | FKs deferíveis na ordem topológica (contacts → conversations → messages) | #82 |
| **C7** | Replay de migrations no restore (manifesto carrega schema_version) | #82 |
| **C8** | Tx `REPEATABLE READ` no export (snapshot consistente) + caveat bloat documentado | #81 |
| **D16** | CSRF em POST /backup, /restore, /reset | #85 |
| **D17** | Audit log em cada ação (D17) | #83, #85 |

---

## 4. Issues (8)

| # | Título | Camada | Esforço | Ref pai principal | Bloqueada por | Bloqueia |
|---|---|---|:--:|---|---|---|
| **#81** | `usecase/backup/export.go` — Export (COPY-por-tenant, REPEATABLE READ, stream S3 + NDJSON) | usecase | 1.0d | `mez-go/internal/usecase/backup/export.go` (~300) | — | #82, #84, #85 |
| **#82** | `usecase/backup/restore.go` — Restore (replay migrations, idempotente, ordem topológica, FKs deferidas) | usecase | 1.0d | `mez-go/internal/usecase/backup/restore.go` (~400) | #81 | #85, #88 |
| **#83** | `usecase/backup/reset.go` — Reset (DELETE por-tenant, sem TRUNCATE, disconnect whatsmeow antes) | usecase | 0.5d | `mez-go/internal/usecase/backup/reset.go` (~150) | #81 | #85 |
| **#84** | `internal/adapter/storage/s3/` — client S3 + multipart upload (backup bucket) | adapter | 0.5d | `mez-go/internal/adapter/storage/s3/` (~150) | — | #81, #82, #88 |
| **#85** | API endpoints: `POST /admin/tenants/:id/backup|restore|reset` + CSRF + audit | transport | 0.5d | `mez-go/cmd/mez-core/admin_tenants_backup.go` (~200) | #81-#84 | #86, #87 |
| **#86** | adminweb `/admin/tenants/:id/backup` (UI com htmx polling de progresso) | transport | 0.5d | `mez-go/cmd/mez-core/admin_tenants_backup_ui.go` (~250) | #85 | #88 |
| **#87** | adminweb `/admin/tenants/:id/reset` (UI com confirmação dupla "RESET" + senha admin) | transport | 0.5d | `mez-go/cmd/mez-core/admin_tenants_reset.go` (~150) | #85 | #88 |
| **#88** | `tests/backup/` — E2E round-trip (export → reset → restore → diff) com testcontainers | tests | 1.0d | `mez-go/internal/usecase/backup/restore_test.go` (~300) | #81-#87 | — |

**Total:** ~6.5 dias (com buffer).

---

## 5. Ordem de execução

1. **#84** S3 client (independente; foundation)
2. **#81** Export (depende de #84)
3. **#82** Restore (depende de #81)
4. **#83** Reset (depende de #81)
5. **#85** API endpoints (depende de #81-#84) — paralelo com #86/#87
6. **#86** UI backup (depende de #85)
7. **#87** UI reset (depende de #85)
8. **#88** tests E2E round-trip (depende de todos)

---

## 6. Stacked commits (estratégia de squash)

Decisão: **squash único** em `fase6-squash`. PR `fase6-squash` → `main`.

Mensagem de commit (referência):

```text
feat(fase6): backup/restore/reset por-tenant (export NDJSON + replay migrations + ordem topológica + RunAsPlatform)

- usecase/backup/export: COPY-por-tenant em tx REPEATABLE READ (C8) + stream S3 multipart + manifest
- usecase/backup/restore: replay migrations sobre o backup (C7) + ordem topológica (C6) + upsert idempotente
- usecase/backup/reset: DELETE por-tenant (sem TRUNCATE) + disconnect whatsmeow antes + RunAsPlatform (C5) auditado
- adapter/storage/s3: client + multipart upload (backup bucket)
- transport/http/api: POST /admin/tenants/:id/{backup,restore,reset} + CSRF (D16) + audit (D17)
- transport/adminweb: /admin/tenants/:id/backup (UI htmx polling) + /reset (confirmação dupla)
- tests/backup: E2E round-trip (export → reset → restore → diff) com testcontainers

Issues: #81, #82, #83, #84, #85, #86, #87, #88
DoD: backup/restore/reset por-tenant funciona end-to-end, idempotência validada,
schema_version respeitado (C7), FKs deferidas garantem ordem (C6), RunAsPlatform
auditado para reset (C5), bloat documentado (C8).
```

---

## 7. Definition of Done (subset da Fase 6)

- [x] `make build` verde.
- [x] `make test` verde.
- [x] **Backup por-tenant exporta NDJSON** de todas as tabelas multi-tenant.
- [x] **Restore idempotente** (mesmo backup aplicado 2x = mesmo estado).
- [x] **Replay de migrations** (C7) — backup com schema antigo é aplicável.
- [x] **FKs deferidas** (C6) — restore respeita ordem topológica.
- [x] **Reset com confirmação dupla** + senha admin.
- [x] **`RunAsPlatform` auditado** (C5) para reset cross-tenant.
- [x] **Audit log** em cada ação (D17).
- [x] **UI htmx polling** de progresso do backup.
- [x] Documentação atualizada — este arquivo.

---

## 8. Riscos e mitigações específicas da Fase 6

| Risco | Mitigação |
|---|---|
| **OOM** ao carregar backup inteiro em memória | Stream NDJSON (`io.Pipe`); lote de 100 rows por INSERT |
| **Backup inconsistente** (mensagem nova durante export) | Tx `REPEATABLE READ` (C8) — snapshot consistente do DB |
| **Bloat no Postgres** (tx longa drena VACUUM) | Documentado em §8 do README — agendar fora de pico |
| **DB ≠ S3** (mensagem com mídia sem blob, ou vice-versa) | Documentado (README §13) — não-atômico, trade-off aceito |
| **Restore viola FK** | `DEFERRABLE INITIALLY DEFERRED` (migration 0003) — verificado no COMMIT |
| **`pg_dump` afeta outros tenants** | **Não usamos `pg_dump`** — só `RunInTenantTx` |
| **`TRUNCATE` afeta outros tenants** | **Não usamos `TRUNCATE`** — só `DELETE WHERE tenant_id = $1` |
| **Reset sem `Disconnect` whatsmeow** | Disconnect ANTES do delete (#83) — session store intacto |
| **Restore de backup novo em schema antigo** | Recusado (C7 — sem downgrade) |
| **Restore de backup antigo em schema novo** | Aceito com replay de migrations |
| **CSRF break** em POST | CSRF middleware em todas (D16) — teste E2E valida 403 sem token |
| **Backup muito grande** (>1GB) | Stream + multipart upload (S3); limite 10GB hard-coded |
| **S3 indisponível** | Backup falha com erro claro; alert em `outbox_dlq_count` |
| **Single-box blast radius** durante backup (tx longa) | Pool separado (`mez_platform` exclusivo para backup) |
| **Encryption at-rest no S3** | Carryover Fase 7 (LocalSealer para SSE-C) |

---

## 9. Carryover para fases seguintes

- **Fase 7** (Hardening):
  - Envelope encryption DEK/tenant (`LocalSealer`) — cifragem at-rest no S3 (SSE-C)
  - JWT key rotation
  - `text_enc BYTEA` em messages
  - Backup incremental (delta desde último backup)
  - Cross-tenant restore (admin global — target ≠ source)
- **Fase 8** (Estabilização):
  - Backup cold boot com N tenants (warm-up paralelo)
  - Chaos test: kill -9 mid-backup → retentar sem corromper S3 manifest
  - Pool separado para backup (não conflitar com requests normais)
- **Pós-1.0:**
  - Backup para S3-compatible alternativo (Backblaze B2, Wasabi)
  - Restore em região diferente (DR)
  - PITR (Point-in-Time Recovery) via WAL archiving

---

## 10. Referências cruzadas

- `mez-go/internal/usecase/backup/{export,restore,reset}.go` — fonte principal
- `mez-go/internal/adapter/storage/s3/` — S3 client
- `migrations/0001_init.up.sql` — schema (FORCE RLS, FKs deferíveis em 0003)
- `migrations/0003_outbox_fks_indexes.up.sql` — FKs `DEFERRABLE INITIALLY DEFERRED` (C6)
- `internal/core/admin/` — Tenant types
- `internal/adapter/repository/postgres/` — RunInTenantTx (Fase 0) + RunAsPlatform (C5, Fase 1)
- `internal/transport/adminweb/` — UI Fase 5 (templates + htmx)
- `internal/transport/http/api/` — Bearer JWT (Fase 3)
- README §5 (C3, C5, C6, C7, C8, D16, D17), §13 (backup/restore/reset), §21 (riscos), §23 (Fase 6), §24 (DoD)
