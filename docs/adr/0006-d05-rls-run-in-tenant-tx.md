# ADR 0006 — D5: RLS via RunInTenantTx + FORCE RLS + role sem BYPASSRLS

* **Status:** Aceita (mantida + reforço C3/C4)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D5](../../README.md#5-decisões-arquiteturais)

## Contexto

Multi-tenancy no DB tem 3 modelos clássicos:

1. **Schema por tenant** — isolation física, mas exige migrar N schemas
   e conexão por schema. Custos operacionais inviáveis para +100 tenants.
2. **Coluna `tenant_id` + WHERE em todo SELECT** — flexível, mas
   depende de disciplina: um SELECT sem `WHERE tenant_id = ?` vaza
   dados. **Fail-open.**
3. **Coluna `tenant_id` + Row-Level Security (RLS)** — o DB rejeita
   leituras/escritas cross-tenant via policy. Application não precisa
   lembrar do filtro; o DB enforces. **Fail-closed**, mas só se a
   policy estiver correta e o owner não bypassar.

A opção 3 é claramente superior, mas tem um **pitfall** documentado:
**o owner da tabela (role que fez `CREATE TABLE`) bypassa RLS por
default**. Sem `FORCE ROW LEVEL SECURITY`, um bug que conecta com o
owner (tipicamente o role de migrate) em produção vaza dados cross-tenant.

As alternativas:

1. RLS + role app sem BYPASSRLS + **FORCE RLS** — fail-closed até
   para o owner. Custo: o role de migrate continua com BYPASSRLS
   (necessário para migrations), mas as policies aplicam a todos os
   outros.
2. RLS + role app sem BYPASSRLS sem FORCE — fail-open para o owner.
3. App-level filter (opção 2 clássica) — fail-open.

## Decisão

Adotamos **opção 1 com 3 roles**:

- `mez_migrate` — owner das tabelas, BYPASSRLS, usado **apenas**
  pelo subcomando `migrate`. Nunca conectado em runtime.
- `mez_app` — role da aplicação, **SEM BYPASSRLS**, sujeito a RLS.
  É o único role usado pelo pool `appPool`.
- `mez_platform` — role admin, **COM BYPASSRLS**, usado
  exclusivamente pelo wrapper `RunAsPlatform` (ADR 0007). Audit log
  atômico em C5.

Toda query do app passa por `RunInTenantTx(ctx, tenantID, fn)` que:

1. Abre transação no `appPool` (mez_app).
2. Executa `SELECT set_config('mez.tenant_id', $1, true)`.
3. Chama `fn(ctx)` — o `ctx` carrega a tx, e os repositórios usam
   `appQFromCtx` para extrair a tx.
4. Commita.

A policy é `USING (tenant_id = current_setting('mez.tenant_id', false)::uuid)`.
O `false` no `current_setting` significa "lança erro se não estiver
setado" — fail-closed (C4): query sem `RunInTenantTx` falha com
"unrecognized configuration parameter" ou retorna zero rows.

## Consequências

### Positivas

- **Isolamento real enforced pelo DB:** mesmo se um dev esquecer
  um `WHERE tenant_id`, a policy bloqueia. Risco de leak é
  **zero** se a policy estiver correta.
- **Audit-friendly:** o `mez.tenant_id` é uma GUC visível em
  `pg_stat_activity`, então qualquer query lenta ou bloqueada pode
  ser atribuída ao tenant.
- **Teste de fail-closed:** os testes em `tests/rls/fail_closed_test.go`
  garantem que uma query fora de `RunInTenantTx` falha. Regressões
  são detectadas no CI.

### Negativas

- **Latência do `set_config`:** cada tx abre, faz SET, executa.
  ~0.1ms de overhead. Aceitável — invisível comparado ao INSERT.
- **Debugging mais complexo:** stack traces de queries incluem o
  GUC. Operador precisa entender RLS para interpretar `pg_stat_activity`.
- **Migrations devem usar `mez_migrate`:** se o operador conectar
  com `mez_app` para fazer um `ALTER TABLE` ad-hoc, falha (não tem
  permissão). Mitigado por o subcomando `migrate` documentado
  (`make run-migrate`).
- **Testcontainers devem criar os 3 roles:** os testes E2E
  instalam os roles via `ALTER ROLE ... LOGIN PASSWORD` antes
  de aplicar policies. Aumenta o setup, mas é o preço da
  isolation real.

## Notas de implementação

Arquivos relevantes:

- `migrations/0001_init.up.sql:14-26` — criação dos 3 roles
- `migrations/0001_init.up.sql:177-236` — `FORCE ROW LEVEL SECURITY`
  em todas as tabelas + policies
- `internal/adapter/repository/postgres/db.go:88-117` —
  `TxRunner.RunInTenantTx` (o wrapper que seta o GUC)
- `tests/rls/fail_closed_test.go` — canary C4
