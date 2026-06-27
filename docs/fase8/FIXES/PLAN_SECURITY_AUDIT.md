# Auditoria de Segurança — Plano de Correção (Fase 8)

> **Origem:** `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (junho/2026).
> **Metodologia:** 5 sub-agentes paralelos (injection, crypto+secrets, web+headers, auth+authz, concurrency+deps) + agregação. STRIDE + DREAD.
> **Escopo aprovado:** tratar os **10 CRITICAL** (DREAD ≥ 7.2) como production-blockers; HIGH subsequentes entram em hardening pass. Items 3.3 Domain Events já está como `wontfix-1.0` (#128) — sem sobreposição.

## Status atual

| Item | Issue alvo | DREAD | Sev | Resumo | Status |
|---|---|---:|---|---|---|
| **C1** | #129 | 8.6 | CRITICAL | WebSocket `CheckOrigin: return true` (CSWSH) | ⏳ |
| **C2** | #130 | 8.4 | CRITICAL | JWT sem `exp` + secret default público | ⏳ |
| **C3** | #131 | 9.0 | CRITICAL | Cookie `__Host-` sem `Secure` | ⏳ |
| **C4** | #132 | 9.0 | CRITICAL | Admin handlers: auth sem authz | ⏳ |
| **C5** | #133 | 9.0 | CRITICAL | IDOR API REST (sem `RunInTenantTx`) | ⏳ |
| **C6** | #134 | 8.6 | CRITICAL | Actor backup controlável via header | ⏳ |
| **C7** | #135 | 8.6 | CRITICAL | Privilege escalation via role editor | ⏳ |
| **C8** | #136 | 6.6 (PII) | CRITICAL | Webhook body PII no log | ⏳ |
| **C9** | #137 | 7.6 | CRITICAL | Backup restore aceita `_table` arbitrário | ⏳ |
| **C10** | #138 | 7.2 | CRITICAL | S3 keys sem validar `tenantID` | ⏳ |
| **H1** | #139 | 8.0 | HIGH | OIDC `next` open redirect | ⏳ |
| **H2** | #140 | 5.8 | HIGH | OIDC `nonce` não validado | ⏳ |
| **H3** | #141 | 6.2 | HIGH | Master key file sem 0600 check (TOCTOU) | ⏳ |
| **H4** | #142 | 7.6 | HIGH | Setup CLI password sem validação | ⏳ |
| **H5** | #143 | 6.2 | HIGH | `RunAsPlatform` audit best-effort | ⏳ |
| **H6** | #144 | 5.0 | HIGH | JWT secret sem length check | ⏳ |
| **H7** | #145 | 7.2 (web) | HIGH | CSRF `/setup` sem validação | ⏳ |
| **H8** | #146 | 2.8 (conc:14) | HIGH | `Bus.Unsubscribe*` por `reflect.Pointer()` | ⏳ |
| **H9** | #147 | 2.6 (conc:13) | HIGH | `OutboxRepo.ClaimNext` sem tx | ⏳ |
| **H10** | #148 | 2.6 (conc:13) | HIGH | `Bus.Publish*` TOCTOU com Drain | ⏳ |
| **H11** | #149 | 2.2 | HIGH | Labstack Echo em dead code | ⏳ |
| **H12** | #150 | 7.4 | HIGH | No TLS termination / redirect | ⏳ |
| **H13** | #151 | 6.8 | HIGH | Security headers `secure=false` | ⏳ |
| **H14** | #152 | 6.0 | HIGH | `ReadHeaderTimeout` 0 (slow-loris) | ⏳ |
| **H15** | #153 | 7.2 | HIGH | `actorEmail` não setado em login audit | ⏳ |
| M1–M16 | #154–#169 | 2.0–7.4 | MEDIUM | Defense-in-depth, error leaks, audit hardening | ⏳ |
| L1–L18 | #170–#187 | 1.0–2.4 | LOW | Docker, helpers, env hygiene | ⏳ |

## Sequência de execução

### Sprint 1 — Production-blockers (5 dias, 1 dev)

**Dia 1-2 (top 5, DREAD ≥ 7.5)**

| # | Item | Arquivo(s) | Tipo | Esforço | Bloq. |
|---|---|---|---|---|---|
| 1 | **C1** WebSocket CheckOrigin | `internal/transport/websocket/hub.go:198-205` + `wire.go` | MECH | 0.1d | — |
| 2 | **C2** JWT exp + ValidateServe | `bearer.go:62-115` + `pkg/config/config.go:95-99` | ADAPT | 0.5d | — |
| 3 | **C3** Cookie `__Host-` Secure | `handlers_auth.go:78-86, 148-156` + `wire.go:308-319` | ADAPT | 0.5d | — |
| 4 | **C8** Webhook body PII no log | `meta/handler.go:138` + zerolog hook | MECH | 0.2d | — |
| 5 | **C5** IDOR API REST | `api/handlers.go` (todos) + `port.RunInTenantTx` | REWRITE | 1.0d | — |
| 6 | **C6** Actor backup do JWT | `api/handlers_backup.go:52-61` | MECH | 0.3d | — |

**Dia 3 (authz + redirect)**

| # | Item | Arquivo(s) | Tipo | Esforço | Bloq. |
|---|---|---|---|---|---|
| 7 | **C4** Wire `Principal.Permissions` + `Evaluate` | `session.go:37-43` + handlers | REWRITE | 1.0d | — |
| 8 | **C7** Role editor `ScopePlatform` | `handlers_roles.go:43-69` + `usecase/admin/roles.go` | ADAPT | 0.5d | C4 |
| 9 | **H1** OIDC next open redirect | `handlers_auth.go:107-115` + `login.go:160-190` | MECH | 0.2d | — |

**Dia 4 (defense in depth + crypto)**

| # | Item | Arquivo(s) | Tipo | Esforço | Bloq. |
|---|---|---|---|---|---|
| 10 | **C9** Allowlist `backupableTables` | `restore.go:316-381` | ADAPT | 0.5d | — |
| 11 | **C10** UUID validation `tenantID` | `backup/{export,restore,reset}.go` | MECH | 0.3d | — |
| 12 | **H3** Master key file 0600 | `config.go:80-86` + `rotate_kek.go:135-143` | MECH | 0.3d | — |
| 13 | **H6** JWT secret length ≥ 32 | `config.go:95-99` | MECH | 0.1d | C2 |
| 14 | **H14** `ReadHeaderTimeout: 5s` | `wire.go:346-352` | MECH | 0.1d | — |

**Dia 5 (validation + headers + concurrency)**

| # | Item | Arquivo(s) | Tipo | Esforço | Bloq. |
|---|---|---|---|---|---|
| 15 | **H4** `ValidatePassword` shared | novo `internal/core/admin/password.go` + `setup.go:48` | NEW | 0.5d | — |
| 16 | **H7** CSRF `/setup` POST | `handlers_setup.go` | MECH | 0.2d | — |
| 17 | **H13** Security headers `secure=true` | `secheaders.go:5-19` + `wire.go:308-319` | MECH | 0.3d | C3 |
| 18 | **H15** `actorEmail` no login audit | `login.go:219-227` + `audit_repo.go:64-104` | MECH | 0.3d | — |

### Sprint 2 — Hardening (3 dias)

| # | Item | Tipo | Esforço |
|---|---|---|---|
| 19 | **H2** OIDC nonce validation | ADAPT | 0.3d |
| 20 | **H5** `RunAsPlatform` atomic | ADAPT | 0.5d |
| 21 | **H8** Bus `Subscribe→Handle` opaque token | REWRITE | 0.5d |
| 22 | **H9** `OutboxRepo.ClaimNext` em tx | ADAPT | 0.3d |
| 23 | **H10** Bus drain TOCTOU fix | MECH | 0.2d |
| 24 | **H11** Deletar `api/openapi.gen.go` Echo | MECH | 0.1d |
| 25 | **H12** TLS termination config | NEW | 0.5d |
| 26 | M1–M8 (audit hardening, RealIP scope, error messages) | mixed | 1.0d |
| 27 | M9–M16 (Argon2, lockout, role ID UUID, password reset) | mixed | 1.0d |

### Sprint 3 — Long tail (2 dias)

| # | Item | Tipo | Esforço |
|---|---|---|---|
| 28 | L1–L18 (Dockerfile root, compose dev creds, helpers) | DOC/MECH | 1.0d |
| 29 | `govulncheck ./...` no CI | CI | 0.3d |
| 30 | Testes de regressão (CSWSH, IDOR, role escalation) | NEW | 1.0d |
| 31 | Threat model review STRIDE completo | DOC | 0.3d |

## Decisões Arquiteturais

1. **Sprint 1 é production-blocker.** Os 10 CRITICAL devem fechar antes do 1.0. A ordem é: autenticação (C1+C2+C3), autorização (C4+C5+C6+C7), defesa (C8+C9+C10).
2. **C4 é REWRITE genuíno.** Requer carregar `ListRoleBindings` + `roles[i].Permissions` no session middleware e injetar `Principal.Permissions`/`Roles`. Isso muda o contrato do `admin.Principal`. PR dedicado.
3. **C5 wrap vs WHERE tenant_id.** Decisão: wrap em `RunInTenantTx(ctx, jwtTenantID, ...)`. Razão: a infraestrutura RLS já está fail-closed no `mez_app` role (BYPASSRLS=false). Wrap preserva a invariante e protege contra deployment errado.
4. **C9 allowlist `backupableTables`.** Mantemos uma allowlist fechada de tabelas e colunas. Restore rejeita `_table` fora. Bloqueia 100% do self-tenant pollution via SQLi.
5. **JWT `exp` é obrigatório.** Tokens sem `exp` ou com `exp=0` são rejeitados. ValidateServe exige `APIJWTSecret` ≥ 32 bytes e rejeita o literal "dev-only-not-secure-replace-in-prod".
6. **Cookie `__Host-` Secure plumbed via config.** Default `true` em qualquer build não-dev. Honra `X-Forwarded-Proto` de proxy confiável.
7. **H8, H9, H10 ficam no Sprint 2** porque o H7 já tem testes de chaos cobrindo comportamento. O concurrency review é DREAD baixo (2.6-2.8) mas severity-subjetivo alto. Triage: Sprint 2.
8. **H11 (Echo dead code) deletar `api/openapi.gen.go`.** Regenerar com `-generate types,chi-server` quando precisar.
9. **H12 TLS termination.** Decisão: documentar e exigir TLS-only no reverse proxy + detecção de forwarded-proto para HSTS dinâmico. `MEZ_TLS_ENABLED` flag para `ListenAndServeTLS` direto (cenário dev).
10. **L1-L18 ficam para Sprint 3.** Não bloqueiam 1.0.

## Definition of Done (Sprint 1)

- [ ] C1: `CheckOrigin` valida `Origin` contra allowlist plumbed via config
- [ ] C2: `parseAndValidateJWT` rejeita tokens sem `exp` válido; `ValidateServe` exige `APIJWTSecret ≥ 32`
- [ ] C3: Cookie `__Host-mez_admin` tem `Secure: true` quando `secure=true` na config
- [ ] C4: `Principal.Permissions` populado no session middleware; `Evaluate(principal, perm, scope)` em todo state-changing handler
- [ ] C5: Todos os handlers `/api/*` (exceto health) embrulhados em `RunInTenantTx`
- [ ] C6: `actorFromRequest` lê `sub`/`email` do JWT; header `X-Admin-Email` removido
- [ ] C7: `handleRolePermissions` exige `PermUpdateRoles, ScopePlatform`; `RoleService.SetPermissions` rejeita platform-only perms em role `Scope=Tenant`
- [ ] C8: Webhook handler loga `body_len`, não `body`; zerolog hook scrubs PII
- [ ] C9: `backupableTables` allowlist; restore rejeita `_table` fora
- [ ] C10: `tenantID` validado como UUIDv4 no boundary
- [ ] H1: `next` validado (path-only, sem `//`)
- [ ] H3: Master key file `0o600` + `O_NOFOLLOW`
- [ ] H4: `ValidatePassword` compartilhada (CLI + HTTP)
- [ ] H6: `APIJWTSecret` length ≥ 32 no `ValidateServe`
- [ ] H7: CSRF aplicado a `/setup` POST
- [ ] H13: Security headers com `secure=true` plumbed
- [ ] H14: `ReadHeaderTimeout: 5s` no `http.Server`
- [ ] H15: `actorEmail` setado em login audit; SELECT em `actor_email` no `AuditRepo.List`
- [ ] `go test -race -shuffle=on ./...` verde para todos os packages afetados
- [ ] `govulncheck ./...` verde
- [ ] Testes de regressão para: CSWSH, IDOR (cross-tenant), role escalation, JWT expired, PII scrub
- [ ] PR atualizado com tabela de items fechados

## Não-Objetivos (explícitos)

- **3.3 Domain Events** (já `wontfix-1.0` em #128)
- **Vault Sealer** (sprint pós-1.0)
- **Multi-region, multi-worker** (Fase 5+)
- **Fuzzing** (sprint pós-1.0)
- **API rate limiting per-endpoint** (CWE-770 detalhamento) — básico via `chimiddleware.Timeout(60s)` já cobre; per-endpoint é polish
- **CORS policy formal** (não há clients cross-origin no 1.0)
- **WAF rules** (responsabilidade do reverse proxy)
- **L1-L18** (Sprint 3)

## Referências

- Audit completo: `003_SECURITY_AUDIT.md` (616 linhas, 5 sub-agentes)
- Plano DDD-Hex: `PLAN_DDD_HEXAGONAL.md` (template seguido)
- Issue wontfix 3.3: #128
- `mez-go` pai AGENTS: `/home/user/felipedsvit/mez-go/AGENTS.md`
