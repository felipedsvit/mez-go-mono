# ADR 0007 — D5b: RunAsPlatform — caminho cross-tenant auditado

* **Status:** Aceita (criada na revisão C5)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D5b](../../README.md#5-decisões-arquiteturais)

## Contexto

O `RunInTenantTx` (ADR 0006) cobre o caminho app → DB com RLS
enforced. Mas há operações legítimas que precisam atravessar
**todos os tenants**: admin global, backup/restore cross-tenant,
outbox relay, rotate-kek (Fase 7 #92).

As alternativas:

1. **Conectar com `mez_migrate` (owner, BYPASSRLS) diretamente** —
   simples, mas **invisível para auditoria**. O admin conecta, faz
   o que quer, e não há rastro. Falha em qualquer cenário de
   compliance.
2. **Criar `mez_platform` (BYPASSRLS) e wrapper `RunAsPlatform`
   que grava audit C5 atomic** — toda operação cross-tenant é
   embrulhada em uma tx que **escreve o audit row PRIMEIRO**, e
   só então executa a mutation. Se a mutation falha, a tx
   inteira (incluindo o audit) sofre rollback.
3. **Cada operação cross-tenant grava seu próprio audit
   best-effort** — `defer audit.Record(...)`. Audit pode ser
   perdido se o processo crashar entre mutation e audit.

## Decisão

Adotamos a opção 2: **wrapper `RunAsPlatform(ctx, actor, action,
targetID, targetType, tenantID, fn)` que grava 1 audit row
"platform_access" com a action solicitada, dentro da mesma tx**.

O wrapper:

1. Abre tx no `platformPool` (mez_platform, BYPASSRLS).
2. Executa `INSERT INTO admin_audit_log (..., action='platform:access',
   metadata={requested_action: $action})`.
3. Chama `fn(ctx)` — a mutation roda na mesma tx.
4. Se `fn` usar `RecordWithTx` para gravar audit próprio, ambos
   os rows commitam atomicamente.
5. Se qualquer passo falha, a tx inteira sofre rollback — o
   audit "platform:access" também some, mas o **efeito da
   mutation também** (atomic).

Garantia: **toda operação cross-tenant tem 1 audit row "platform:access"
correspondente**, ou a operação foi revertida. Não há janela
"mutation sem audit".

## Consequências

### Positivas

- **Compliance-ready:** qualquer ferramenta que lê `admin_audit_log`
  vê TODA operação cross-tenant registrada. SOC2, ISO 27001, LGPD —
  o requisito "rastreabilidade de acesso cross-tenant" é satisfeito
  por construção.
- **Crash-safe:** o pai `mez-go` tinha o pattern "audit best-effort
  via defer", que perdia audit em crash entre mutation e audit. O
  mono elimina essa janela.
- **Detecção de anomalia trivial:** queries como "platform:access
  para tenant X nos últimos 30 dias" viram SQL simples contra
  `admin_audit_log`. Operador consegue responder "quem mexeu em
  tenant X?" sem grep em logs.

### Negativas

- **Latência adicional:** 1 INSERT extra por operação cross-tenant.
  Aceitável — a operação cross-tenant já é rara (admin), não está
  no hot-path de webhook.
- **Audit table cresce:** `admin_audit_log` é append-only e nunca
  tem VACUUM agressivo. Mitigado por partitioning por mês (pós-1.0).
- **Cuidado com ordem de inserção:** se o código faz o INSERT do
  audit DEPOIS da mutation (e não antes), o wrapper não tem
  como garantir atomicidade. Documentado em comentário da função.
- **`mez_platform` é um superuser-like:** um bug que conecta com
  `mez_platform` no lugar errado vaza dados. Mitigado pelo
  fato de que **só o `TxRunner` usa esse pool**; nenhum
  repositório aceita `platformPool` como dependência.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/repository/postgres/db.go:146-161` —
  `TxRunner.RunAsPlatform` (admin DB versão em `admin/db.go:63-119`)
- `internal/core/admin/tx.go:26` — interface `TxRunner.RunAsPlatform`
- `internal/core/admin/audit.go:35` — `ActionPlatformAccess`
- `tests/platform/run_as_platform_test.go` — canary C5 (audit atômico)
