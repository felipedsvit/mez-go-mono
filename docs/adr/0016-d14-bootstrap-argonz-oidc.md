# ADR 0016 — D14: Bootstrap wizard (Argon2id) + OIDC para os demais

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D14](../../README.md#5-decisões-arquiteturais)

## Contexto

O primeiro admin de uma instalação nova precisa ser criado sem
autenticação pré-existente. As alternativas:

1. **Env-var-defined admin** — `MEZ_BOOTSTRAP_ADMIN_EMAIL` +
   `MEZ_BOOTSTRAP_ADMIN_PASSWORD` lidos no boot. Operador seta
   antes do primeiro start.
2. **Bootstrap wizard** — `POST /setup` (rota pública) cria o
   primeiro admin. Após isso, a rota é desabilitada.
3. **Both** — wizard + fallback env-var para CI/dev.

Para logins subsequentes:

- **Senha (Argon2id)** — para o admin criado via wizard.
- **OIDC** (Google, GitHub, etc.) — para demais admins.

## Decisão

Adotamos a opção 3 com split:

- **Bootstrap** via `POST /setup` (wizard) é o caminho **primário**.
  Aplica-se em fresh install. Após criar o primeiro admin, a rota
  retorna 404 (lock-down).
- **Fallback env-var** (`MEZ_BOOTSTRAP_ADMIN_EMAIL/PASSWORD`)
  para **CI e testes automatizados** — documentado como "use only
  in dev, NEVER in production".
- **Logins subsequentes** usam:
  - **Email + senha (Argon2id)** para o admin criado via wizard.
  - **OIDC** (qualquer provider compatível) para demais admins,
    configurável por tenant ou global.

Argon2id com parâmetros: `time=2, memory=64MB, threads=2, keyLen=32`.
Validação contra lista de parâmetros recomendados pela OWASP 2024.

## Consequências

### Positivas

- **Onboarding claro:** o operador sabe o que fazer (abrir `/setup`
  no browser) sem ler docs longas.
- **OIDC reduz atrito:** admins corporativos entram com o IdP da
  empresa. Sem senha adicional para lembrar.
- **Argon2id resistente a GPU/ASIC:** parameters conservador
  (~250ms por hash em hardware moderno) torna rainbow tables
  inviáveis.
- **Audit de bootstrap:** `ActionSetupBootstrap` é gravado no
  audit log, mesmo que o "actor" seja anônimo (não há user_id
  ainda — `actor_id = ''`).

### Negativas

- **Wizard lock-down exige cuidado:** se o operador esquece
  a senha do único admin, não há reset sem drop do DB.
  Mitigado por suporte a múltiplos admins via OIDC.
- **Parâmetros Argon2id consomem CPU:** 64MB × 2 threads por
  hash. Login leva ~250ms. Aceitável — login não é hot-path.
- **OIDC adiciona dependência externa:** se o IdP está down,
  login falha. Sem fallback para senha em OIDC-only users.
  Mitigado por: (a) senha local sempre disponível como backup,
  (b) lockout throttling no middleware.
- **MAU / quota de OIDC:** providers como Google têm rate limit
  de token exchange. Em deployments grandes, considerar IdP
  próprio (Authentik, Keycloak).

## Notas de implementação

Arquivos relevantes:

- `cmd/server/setup.go` — subcomando `setup` (idempotente após
  primeiro admin)
- `internal/transport/adminweb/handler_setup.go` — `POST /setup`
- `internal/adapter/auth/argon2/` — `Argon2id` com parâmetros
  validados contra OWASP
- `internal/core/oidc/` — provider OIDC genérico
- `internal/core/admin/audit.go:14-16` — `ActionSetupBootstrap`,
  `ActionSetupRebootstrap`
