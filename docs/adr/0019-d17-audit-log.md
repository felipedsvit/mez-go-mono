# ADR 0019 — D17: Audit log de toda ação admin e cross-tenant

* **Status:** Aceita (mantida + reforço)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D17](../../README.md#5-decisões-arquiteturais)

## Contexto

Compliance (SOC2, ISO 27001, LGPD) exige rastreabilidade de ações
admin. As alternativas:

1. **Log estruturado (zerolog) + agregador externo** — Loki,
   Datadog, ELK. Prós: zero schema, alta cardinalidade. Contras:
   depende de infra externa, e "log perdido" não é detectável.
2. **Tabela `admin_audit_log` no próprio DB** — schema fixo,
   retention controlada, queries SQL. Prós: garantia de
   durabilidade (mesma tx da mutation), queries diretas. Contras:
   crescimento da tabela, partitioning necessário a longo prazo.
3. **Ambas** — DB como fonte primária, log como streaming
   secundário para SIEM.

## Decisão

Adotamos a opção 2 como **fonte primária** + log zerolog como
**streaming** para Loki/ELK em prod.

Tabela `admin_audit_log`:

```sql
CREATE TABLE admin_audit_log (
    id          UUID PRIMARY KEY,
    actor_id    UUID,              -- nullable (criação bootstrap)
    actor_email TEXT NOT NULL,     -- sempre setado (denormalized)
    action      TEXT NOT NULL,     -- enum: 'auth.login.success', etc
    target_type TEXT,              -- 'tenant', 'user', 'role', ...
    target_id   TEXT,
    tenant_id   UUID,              -- nullable (platform-wide actions)
    metadata    JSONB DEFAULT '{}',
    ip          TEXT,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Ações registradas:

- **Auth:** `auth.login.success`, `auth.login.failure`, `auth.logout`
- **Setup:** `setup.bootstrap`, `setup.rebootstrap`
- **Tenant:** `tenant:create`, `tenant:update`, `tenant:status`,
  `tenant:list`, `tenant:backup`, `tenant:restore`, `tenant:reset`
- **User:** `user:create`, `user:status`, `user:role.assign`,
  `user:role.revoke`
- **Role:** `role:create`, `role:permissions`
- **Platform:** `platform:access` (gravada por `RunAsPlatform`,
  C5, atômico)
- **Fase 7 (rotate-kek):** `secrets.rotate_kek.started`,
  `secrets.rotate_kek.tenant`, `secrets.rotate_kek.complete`

## Consequências

### Positivas

- **Compliance-ready:** query "quem deletou tenant X?" é
  `SELECT * FROM admin_audit_log WHERE target_id = 'X'
  AND action = 'tenant:reset'`. Sem grep em logs.
- **Atomic com a mutation:** `RecordWithTx` (Fase 1) garante
  que a mutation e o audit commitam juntos, ou ambos rolam
  back. Sem "audit perdido".
- **Cross-tenant atômico:** `RunAsPlatform` (ADR 0007) garante
  audit em toda operação cross-tenant.
- **Retention controlável:** vacuum/cron de remoção de entries
  > 2 anos. Pós-1.0: partitioning por mês.

### Negativas

- **Tabela cresce:** ~100 bytes/entry × N ações/dia. Para
  10k ações/dia × 2 anos = ~700MB. Aceitável.
- **Insert em hot-path:** toda mutation grava 1 audit row.
  +1 INSERT por mutation. Aceitável — é o preço de compliance.
- **Query de "todos os eventos do ator X" é pesada:** se
  X é admin prolific, retorna muitos rows. Mitigado por
  paginação obrigatória (LIMIT 200 default, 1000 max).
- **PII no metadata:** `metadata` JSONB pode conter dados
  sensíveis. Mitigado por documentar schema de metadata por
  action, e LGPD/GDPR right-to-erasure: drop audit do
  actor_id (manter actor_email para histórico).

## Notas de implementação

Arquivos relevantes:

- `internal/core/admin/audit.go` — `AuditEntry`, `AuditRepo`
  interface, `Action` enum (todos os valores acima)
- `internal/adapter/repository/postgres/admin/audit_repo.go` —
  implementação com `Record` (best-effort) e `RecordWithTx`
  (atômico, dentro de `RunAsPlatform`)
- `migrations/0002_admin.up.sql` — schema `admin_audit_log`
  com índices em `actor_id`, `action`, `tenant_id`,
  `created_at`
- `tests/platform/run_as_platform_test.go` — canary C5
  (audit atômico em `RunAsPlatform`)
