# Auditoria de Segurança — mez-go-mono (Fase 8, pré-1.0)

> **Data:** junho/2026 · **Escopo:** 5 domínios paralelos (injection, crypto+secrets, web+headers, auth+authz, concurrency+deps) · **Metodologia:** STRIDE + DREAD, com data-flow tracing · **Persona:** Senior Go security engineer
> **Modo:** Audit (5 sub-agentes paralelos + agregação)
> **Status:** pré-1.0, **10 vulnerabilidades production-blocker** (5 CRITICAL, 5 HIGH com DREAD ≥ 7.5)

---

## TL;DR

Cinco vulnerabilidades CRITICAL e cinco HIGH com DREAD ≥ 7.5 bloqueiam a release 1.0. O tema central é **failing-closed pretendido mas failing-open real**: o design do projeto (RLS, validação de origem, autorização por papel) é correto no papel, mas a implementação deixa pelo menos 4 furos por onde um atacante autenticado (ou não) consegue ler/escrever/destruir dados cross-tenant.

**Top 5 (1-2 dias de fix):**
1. WebSocket `CheckOrigin: return true` (CSWSH)
2. JWT sem `exp` + secret default público no fallback
3. Cookie `__Host-` sem `Secure` (browsers rejeitam OU aceitam em cleartext)
4. Admin handlers: autenticação sim, autorização não — qualquer admin opera qualquer tenant
5. IDOR na API REST (handlers não usam `RunInTenantTx`; RLS é a única barreira e a API a ignora)

**Top 6-10 (3-5 dias):**
6. `actorFromRequest` em backup aceita header `X-Admin-Email` forjado
7. Privilege escalation via editor de role (qualquer um cria role com scope Platform)
8. Webhook body em cleartext no log (PII leak)
9. Backup restore aceita `_table` e colunas arbitrárias (CWE-89 defense-in-depth)
10. S3 keys/prefixos sem validar `tenantID` (path confusion)

**Concorrência (1 dia):** `UnsubscribeInbound` por `reflect.Pointer()` + `OutboxRepo.ClaimNext` sem tx.

---

## Findings — CRITICAL (DREAD ≥ 7.5)

### C1. WebSocket `CheckOrigin` aceita qualquer origem — CSWSH
- **Arquivo:** `internal/transport/websocket/hub.go:260-267`
- **CWE:** CWE-1385 (Missing Origin Validation in WebSockets) · CWE-352 (CSRF)
- **DREAD:** D=9 R=9 E=8 A=9 D=8 → **8.6**
- **Confirmado por:** 3 sub-agentes (web, injection, concurrency)
- **Descrição:** `var Upgrader = websocket.Upgrader{ CheckOrigin: func(*http.Request) bool { return true } }`. O handler em `handler.go:34` só lê `tenantID` do ctx (vindo do session cookie). Browser envia o cookie em cross-origin WebSocket. Atacante em `evil.com` faz `new WebSocket("wss://target/app/ws")` → upgrade sucede → assinante do fan-out do tenant da vítima → toda mensagem inbound do tenant é exfiltrada em tempo real.
- **Fix:** Construir o `Upgrader` em `wire.go` com `CheckOrigin` config-driven comparando `Origin` contra allowlist.

### C2. JWT sem `exp` validation + secret default público
- **Arquivo:** `internal/transport/http/middleware/bearer.go:70-116`; fallback em `internal/transport/http/server/server.go:82-88`
- **CWE:** CWE-613 (Insufficient Session Expiration) · CWE-798 (Hard-coded Credentials)
- **DREAD:** D=9 R=9 E=8 A=9 D=7 → **8.4** (composto)
- **Confirmado por:** 3 sub-agentes (crypto, web, auth)
- **Descrição:** `parseAndValidateJWT` lê `Claims.Exp` mas **nunca valida**. Token com `exp=0` (1970) é aceito. Combinado com o fallback `"dev-only-not-secure-replace-in-prod"` quando `MEZ_API_JWT_SECRET` está vazio (e `ValidateServe` só checa `SessionSecret`, não `APIJWTSecret`): uma instância implantada sem env setada aceita JWTs forjados por qualquer um com acesso ao source.
- **Fix:** Validar `exp`/`nbf` em `parseAndValidateJWT`; em `ValidateServe`, exigir `APIJWTSecret` com `len ≥ 32`; rejeitar o literal dev no startup; remover o fallback do `server.go`.

### C3. Cookie `__Host-mez_admin` sem `Secure: true`
- **Arquivos:** `cmd/server/wire.go:313` (nome) + `internal/transport/adminweb/handlers_auth.go:81-89, 151-159` (emissão sem `Secure`)
- **CWE:** CWE-614 (Sensitive Cookie Without Secure)
- **DREAD:** D=9 R=10 E=7 A=9 D=10 → **9.0**
- **Confirmado por:** 2 sub-agentes (web, auth)
- **Descrição:** O prefixo `__Host-` (RFC 6265bis) **exige** `Secure=true`. Browsers modernos ou rejeitam o cookie (auth quebra) ou aceitam em cleartext (sessão sniffável em WiFi de café). O ADR 0018 documenta a intenção, mas o código contradiz.
- **Fix:** Plumar `Secure bool` da config para o `handlers_auth.go`; default `true` em qualquer build não-dev; honrar `X-Forwarded-Proto` de proxy confiável.

### C4. Admin handlers: autenticação sem autorização
- **Arquivos:** `internal/transport/adminweb/handlers_users.go:11-87`, `handlers_tenants.go:11-91`, `handlers_roles.go:11-73`, `handlers_audit.go:10-25`, `handlers_backup.go:28-167`, `handlers_reset.go:23-69`
- **CWE:** CWE-862 (Missing Authorization) · CWE-285 (Improper Authorization)
- **DREAD:** D=9 R=10 E=9 A=9 D=8 → **9.0**
- **Confirmado por:** 2 sub-agentes (injection, auth)
- **Descrição:** Cada handler chama `principalOrEmpty(r)` para extrair o user, mas **nenhum chama `admin.Evaluate(principal, perm, scope)`**. A única porta é o `RequireAuth` middleware, que só verifica que existe sessão. O middleware de sessão injeta apenas `UserID` + `Email` — `Principal.Permissions` e `Principal.Roles` são sempre `nil`. Resultado: qualquer tenant owner de A suspende/desativa/edita qualquer tenant/user/role de B, e roda backup/restore/reset em qualquer tenant.
- **Fix:** Reconstruir `Principal.Permissions`/`Roles` no session middleware (carregar `ListRoleBindings` + `roles[i].Permissions`). Adicionar `Evaluate(principal, perm, scope)` em todo state-changing handler.

### C5. IDOR em endpoints REST: API não usa `RunInTenantTx`
- **Arquivos:** `internal/transport/http/api/handlers.go:89-452`; `internal/transport/http/middleware/bearer.go:62-115`
- **CWE:** CWE-639 (IDOR) · CWE-284 (Improper Access Control)
- **DREAD:** D=10 R=10 E=10 A=8 D=7 → **9.0**
- **Confirmado por:** auth (Critical) + injection (High via defense-in-depth)
- **Descrição:** O doc-comment em `handlers.go:16` diz "RLS via RunInTenantTx (claim tenant_id do token)" mas nenhum handler chama `txRunner.RunInTenantTx`. Eles chamam `h.convRepo.ListByTenant`, `h.msgRepo.Get`, `h.convRepo.Upsert` direto. O `appQFromCtx` fallback (`db.go:29-34`) usa o pool raw quando não há tx no ctx. Contra o schema real (RLS FORCED + mez_app sem BYPASSRLS), toda query falha; ou — se o deployment deu BYPASSRLS — o IDOR é totalmente explorável: tenant A com JWT válido insere/lee/edita rows de tenant B. `listMessages`, `postReaction`, `patchMessage`, `deleteMessage`, `conversationAssign`, `conversationResolve` todos affected.
- **Fix:** (a) embrulhar todo handler em `txRunner.RunInTenantTx(ctx, jwtTenantID, …)`, OU (b) adicionar `WHERE tenant_id = $1` em todo `Get`/`List`/`Upsert` e rejeitar mismatch no handler.

### C6. Actor do backup/restore/reset é controlável pelo atacante
- **Arquivo:** `internal/transport/http/api/handlers_backup.go:52-61`
- **CWE:** CWE-285 · CWE-345 (Insufficient Verification of Data Authenticity)
- **DREAD:** D=10 R=10 E=10 A=7 D=6 → **8.6**
- **Confirmado por:** auth
- **Descrição:** O handler constrói `actor` a partir de `r.Header.Get("X-Admin-Email")` (com fallback `"unknown@admin"`). O JWT só é parseado para `tenant_id` (outros claims ignorados). `actor.ID` é **sempre** vazio. O serviço grava no `audit_log` o email do header — qualquer um com JWT válido para qualquer tenant dispara backup/restore/reset para qualquer tenant e atribui o ato a um email arbitrário. O `RequireScope` middleware é um `// TODO Fase 5` no-op (`bearer.go:129-135`).
- **Fix:** Extrair `sub`/`email` do JWT, rejeitar se ausentes, usar como `Actor.ID`/`Actor.Email`. Apagar o path do header `X-Admin-Email`.

### C7. Privilege escalation via editor de role
- **Arquivos:** `internal/transport/adminweb/handlers_roles.go:47-73`, `internal/usecase/admin/roles.go:48-64`
- **CWE:** CWE-269 (Improper Privilege Management)
- **DREAD:** D=10 R=10 E=9 A=8 D=6 → **8.6**
- **Confirmado por:** auth
- **Descrição:** `handleRolePermissions` aceita qualquer role ID + lista de permissions e chama `RoleService.SetPermissions`. Único guard é `if role.IsBuiltin { return ErrProtectedRole }`. Combinado com a falta de autorização do C4, qualquer usuário autenticado pode: (a) criar role com `Scope=Platform` via `RoleUseCase.Create` (acessível pelo wiring interno), (b) editar qualquer role não-builtin para incluir `PermCreateTenants`/`PermDeleteTenants` etc. O `Evaluate` exige `ScopePlatform` para platform-scoped perms, mas isso é moot quando o criador pode setar `Scope=Platform`.
- **Fix:** (a) Restringir `handleRolePermissions` a platform-admins via `Evaluate(principal, PermUpdateRoles, ScopePlatform)`. (b) `RoleService.SetPermissions` deve rejeitar platform-only perms em roles `Scope=Tenant`. (c) `RoleService.Create` com `Scope=Platform` deve exigir caller platform-admin.

### C8. Webhook body em cleartext no log (PII leak)
- **Arquivo:** `internal/adapter/webhook/meta/handler.go:138`
- **CWE:** CWE-532 (Insertion of Sensitive Information into Log File)
- **DREAD:** D=8 R=7 E=7 A=5 D=6 → **6.6 (composite)** — Critical por causa do PII/regulatory
- **Confirmado por:** crypto
- **Descrição:** Em erro de JSON decode, o handler loga `body` (até 2 MiB) com `log.Warn().Bytes("body", body)`. Inbound Meta webhooks carregam `text.body`, contatos, template params — PII real. Loki/ELK indexa em full. Toda mensagem que falha parse vaza PII.
- **Fix:** Logar apenas `body_len`; gating de full-body atrás de `MEZ_DEBUG_WEBHOOK_BODY=true`; zerolog hook que scrub `body`/`dek`/`wrapped_dek`/`encrypted`/`password`/`token`/`secret`.

### C9. Backup restore aceita `_table` e colunas arbitrárias (defense-in-depth SQLi)
- **Arquivo:** `internal/usecase/backup/restore.go:316-381`; helpers em `export.go:397-407`
- **CWE:** CWE-89 (defense-in-depth)
- **DREAD:** D=8 R=7 E=6 A=9 D=8 → **7.6**
- **Confirmado por:** injection
- **Descrição:** `pgQuoteIdent` é correto (escapa `"`), mas não *valida* o identifier. `_table` é lido do NDJSON sem allowlist. `mez_app` tem `GRANT INSERT` em todas as tabelas. Atacante com S3 write access (ou backup envenenado) pode: (a) escrever em qualquer tabela do schema, (b) sobrescrever rows via `id` (ON CONFLICT). RLS ainda limita `tenant_id`, mas self-tenant pollution é trivial.
- **Fix:** Maintainer uma allowlist fechada de `backupableTables` com allowlist de colunas por tabela. Rejeitar `_table` fora da allowlist; rejeitar colunas fora da allowlist.

### C10. S3 keys/prefixos sem validar `tenantID`
- **Arquivos:** `internal/usecase/backup/export.go:284-285`, `restore.go:72-73`, `reset.go:166-167`
- **CWE:** CWE-22 (Path Traversal) · CWE-639 (IDOR)
- **DREAD:** D=7 R=7 E=7 A=8 D=7 → **7.2**
- **Confirmado por:** injection
- **Descrição:** `fmt.Sprintf("tenants/%s/backups/%s/...", req.TenantID, backupID)`. `tenantID` vem de `chi.URLParam`. `backupID` é UUIDv4 (safe), mas `tenantID` aceita `*` (S3 wildcard) ou prefixo `tenants/X` (lista tudo). Combinado com C4 (sem authz por tenant), admin wipe cross-tenant: `POST /admin/tenants//reset` ou `POST /admin/tenants/_/reset`.
- **Fix:** Validar `tenantID` é UUID no boundary; authz check de scope tenant-specific.

---

## Findings — HIGH (DREAD 5.5-7.4)

### H1. OIDC `state` não validado contra sessão; `next` permite open redirect
- **Arquivos:** `internal/transport/adminweb/handlers_auth.go:110-166`; `internal/usecase/auth/login.go:160-190`
- **CWE:** CWE-601 (Open Redirect) · CWE-352
- **DREAD:** D=6 R=10 E=10 A=6 D=8 → **8.0** (open redirect)
- **Confirmado por:** auth
- **Descrição:** `next` do query string vai direto para `OIDCState.RedirectAfter` e para `s.redirect(w, r, redirectAfter)` sem validar path interno. Atacante crafta `https://target/auth/oidc/start?next=https://evil.com/phish`; pós-login, user aterrissa em `evil.com` (que pode coletar mais credenciais).
- **Fix:** `if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") { next = "/admin/" }`.

### H2. OIDC `nonce` não validado (replay de ID-token)
- **Arquivos:** `internal/adapter/idp/oidc/oidc.go:46-47, 81-103`; `verifier.go:28-33`
- **CWE:** CWE-294 (Authentication Bypass by Capture-Replay)
- **DREAD:** D=7 R=5 E=5 A=7 D=5 → **5.8**
- **Confirmado por:** auth
- **Descrição:** `provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})` não seta `Config.Nonce`. PKCE está correto (S256), mas o ID-token, uma vez capturado (log do server, XSS no IdP, MITM), pode ser replayed.
- **Fix:** Gerar nonce random em `StartOIDC`, persistir em `OIDCState`, passar em `AuthCodeURL`, validar em `VerifyIDToken` com `Config.Nonce`.

### H3. Master key file sem check de permissão 0600 (TOCTOU)
- **Arquivo:** `pkg/config/config.go:82-93`; mesmo padrão em `cmd/server/rotate_kek.go:131-143`
- **CWE:** CWE-732 (Incorrect Permission Assignment) · CWE-367 (TOCTOU)
- **DREAD:** D=6 R=5 E=6 A=7 D=7 → **6.2**
- **Confirmado por:** crypto
- **Descrição:** `os.ReadFile(masterKeyFile)` sem `os.Stat` prévio. KEK em arquivo 0644 é aceito. KEK = root de toda envelope encryption (Fase 7 #89, #91, #92). Sem symlink check: arquivo pode ser symlink para `/dev/stdin` ou similar.
- **Fix:** `os.Stat` + check `mode & 0o077 == 0`; `O_NOFOLLOW` no `OpenFile`; mesma correção em `rotate_kek.go`.

### H4. `setup` (CLI) aceita password arbitrariamente fraca
- **Arquivo:** `cmd/server/setup.go:19-25, 48`
- **CWE:** CWE-521 (Weak Password Requirements)
- **DREAD:** D=8 R=10 E=7 A=7 D=6 → **7.6**
- **Confirmado por:** crypto, web, auth
- **Descrição:** `MEZ_SETUP_PASSWORD=x` é aceito e vira platform super-admin. HTTP form tem `minlength="8"`; CLI não tem equivalente.
- **Fix:** Função compartilhada `ValidatePassword` em `internal/core/admin/password.go`: min 12, max 64, breach check (HIBP k-anonymity ou local list). Chamar de CLI e HTTP.

### H5. `RunAsPlatform` audit é best-effort, não atômico
- **Arquivos:** `internal/usecase/backup/service.go:148-167, 169-207`; `internal/usecase/backup/export.go:351-356`, `restore.go:290-295`, `reset.go:184-189`
- **CWE:** CWE-778 (Insufficient Logging)
- **DREAD:** D=6 R=6 E=6 A=8 D=5 → **6.2**
- **Confirmado por:** auth, crypto
- **Descrição:** `s.recordAudit` (line 148) usa `Record` (best-effort, não na tx). Crash entre audit insert e mutation ⇒ audit fantasma ou audit faltando. `Service.runAsPlatform` (line 169) é definido mas **nunca é chamado** (grep confirma).
- **Fix:** Wrap cada operação em `RunAsPlatform` da platform pool. Mover `recordAudit` para dentro do tx existente.

### H6. JWT secret sem check de length/entropy
- **Arquivo:** `pkg/config/config.go:38`; ausência em `ValidateServe`
- **CWE:** CWE-326 (Inadequate Encryption Strength)
- **DREAD:** D=5 R=5 E=4 A=5 D=6 → **5.0**
- **Confirmado por:** crypto
- **Descrição:** HS256 com secret de 4 chars é brute-forcável em horas. Sem rotação in-process.
- **Fix:** `ValidateServe` exige `len(APIJWTSecret) ≥ 32`. Adicionar `kid` header + tabela de KEKs anteriores para rotação zero-downtime.

### H7. CSRF: `/setup` POST sem validação (apenas leitura do token)
- **Arquivo:** `internal/transport/adminweb/handlers_setup.go:40-44, 66-145`
- **CWE:** CWE-352
- **DREAD:** D=3 R=4 E=6 A=3 D=3 → **3.8** (production: 7.2 pelo web agent)
- **Confirmado por:** web
- **Descrição:** O form tem `csrf_token` hidden e o handler lê o cookie `XSRF-TOKEN`, mas **nunca compara**. `/setup` está fora do middleware group do CSRF (`server.go:112-114`). Race: enquanto operator carrega o form, attacker POSTa `email=attacker@evil.com&password=evil` e pode ganhar a race (handlers_setup.go:88-98 protege o segundo writer, mas não o primeiro).
- **Fix:** Aplicar `middleware.CSRF(...)` ao `mux` do setup, ou Origin/Referer check no POST.

### H8. `Bus.Unsubscribe*` usa `reflect.ValueOf(h).Pointer()` — identidade errada
- **Arquivo:** `internal/adapter/broker/bus.go:184-210`
- **CWE:** CWE-1284 (Improper Validation of Specified Quantity) — predicado quebrado
- **DREAD:** D=3 R=3 E=3 A=2 D=3 → **2.8** (alto pelo concurrency agent: 14/5=2.8 raw)
- **Confirmado por:** concurrency
- **Descrição:** `reflect.ValueOf(funcVal).Pointer()` retorna o *code pointer*, não a identidade do closure. Dois closures sobre variáveis capturadas diferentes compartilham o code pointer. `UnsubscribeInbound(h)` remove o handler errado. Em Fase 8 #97 (shutdown chama Unsubscribe), handler leak + producer continua disparando-o.
- **Fix:** Token opaco: `SubscribeInbound(handler) → handle`; `UnsubscribeInbound(handle)`.

### H9. `OutboxRepo.ClaimNext` sem transação (SKIP LOCKED inócuo)
- **Arquivo:** `internal/adapter/repository/postgres/outbox.go:104-114`
- **CWE:** CWE-662 (Improper Synchronization)
- **DREAD:** D=3 R=2 E=3 A=2 D=3 → **2.6**
- **Confirmado por:** concurrency
- **Descrição:** `platformPool.Query(... FOR UPDATE SKIP LOCKED)` numa única statement — locks liberados ao fim do statement. `AcquireClaimLock` (line 278) está exportado mas nunca é chamado. Em scale-out, dois relays pegam a mesma row. Single-process passa despercebido.
- **Fix:** `ClaimNext` deve abrir `BeginTx`, retornar o tx, e o caller deve drenar dentro do tx; OU `UPDATE ... RETURNING` atômico.

### H10. `Bus.Publish*` TOCTOU entre `isDrained()` e `select`
- **Arquivo:** `internal/adapter/broker/bus.go:85-150`
- **CWE:** CWE-367 (TOCTOU)
- **DREAD:** D=2 R=2 E=3 A=3 D=3 → **2.6**
- **Confirmado por:** concurrency
- **Descrição:** Entre `isDrained()=false` e o `select`, o `Drain` pode completar. Consumer saiu; channel ainda aberto. Event publicado vai para o buffer ou é dropado (mas o log diz "reconciler covers" — mentira após drain).
- **Fix:** Adicionar `done` channel; `select { case ch <- evt: ...; case <-b.drainCh: return; default: ...drop... }`.

### H11. Labstack Echo pulled por dead code
- **Arquivo:** `go.mod:12`; `api/openapi.gen.go:11`
- **CWE:** Dead dependency; "CVE-2024-57277 / 2025-31345 / 2025-46727" históricos do Echo
- **DREAD:** D=2 R=2 E=2 A=3 D=2 → **2.2**
- **Confirmado por:** concurrency
- **Descrição:** `oapi-codegen -generate types,server` gera server Echo-flavored. `api/` package no root não é importado por ninguém. Bloat no go.sum; `govulncheck` não flagga (código morto), mas desenvolvedor confuso.
- **Fix:** Deletar `api/openapi.gen.go` OU regenerar com `-generate types,chi-server`.

### H12. No TLS termination / no HTTP→HTTPS redirect
- **Arquivo:** `cmd/server/wire.go:345-351`; `internal/transport/http/server/server.go:96-107`
- **CWE:** CWE-319 (Cleartext Transmission)
- **DREAD:** D=8 R=8 E=7 A=7 D=7 → **7.4**
- **Confirmado por:** web
- **Descrição:** `ListenAndServe` cleartext, sem redirect middleware, sem detecção de `X-Forwarded-Proto`. Combinado com C3 (cookie sem `Secure`), toda sessão admin é trivialmente sniffável em rede não-proxied.
- **Fix:** Plumar `MEZ_TLS_ENABLED` + cert paths; ou documentar e exigir TLS-only no reverse proxy + detecção de forwarded-proto para HSTS dinâmico.

### H13. Security headers sempre invocados com `secure=false`
- **Arquivo:** `internal/transport/adminweb/server.go:109`
- **CWE:** CWE-693 (Protection Mechanism Failure)
- **DREAD:** D=7 R=7 E=6 A=8 D=6 → **6.8**
- **Confirmado por:** web
- **Descrição:** `r.Use(middleware.SecurityHeaders(false))` — HSTS nunca emitido. CSP fraca (`default-src 'self'; style-src 'self' 'unsafe-inline'`; sem `script-src`, `frame-ancestors`, `object-src`).
- **Fix:** Plumar `secure` da config. CSP completa: `script-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'`. Nonce para inline.

### H14. `ReadHeaderTimeout` não setado (slow-loris)
- **Arquivo:** `cmd/server/wire.go:345-351`
- **CWE:** CWE-400
- **DREAD:** D=6 R=6 E=5 A=7 D=6 → **6.0**
- **Confirmado por:** web
- **Descrição:** `ReadTimeout: 15s, WriteTimeout: 15s, IdleTimeout: 60s` mas `ReadHeaderTimeout: 0` — slow-loris nos headers (1 byte a cada 14s) mantém a conexão viva indefinidamente.
- **Fix:** `ReadHeaderTimeout: 5*time.Second; MaxHeaderBytes: 1<<20`.

### H15. `actorEmail` nunca setado em audit de login/logout
- **Arquivo:** `internal/usecase/auth/login.go:219-227`
- **CWE:** CWE-778 · CWE-213
- **DREAD:** D=5 R=10 E=5 A=9 D=7 → **7.2**
- **Confirmado por:** auth
- **Descrição:** `LoginService.recordAudit` recebe `email` mas nunca faz `entry.ActorEmail = email`. Toda linha `auth.login.success`/`failure`/`logout` fica com `actor_email = NULL`. O `AuditRepo.List` (`audit_repo.go:64-104`) nem faz SELECT em `actor_email`. O ponto do denormalized column (audit trail após ON DELETE SET NULL) é derrotado.
- **Fix:** Setar `entry.ActorEmail = email`; incluir `actor_email` no SELECT de `List`; deserializar no `AuditEntry`.

---

## Findings — MEDIUM (DREAD 4.0-5.4)

### M1. `audit_log` JSONB metadata sem size cap; `X-Admin-Email` forjável
- **Arquivo:** `internal/transport/http/api/handlers_backup.go:53, 69`; `internal/usecase/backup/service.go:149-167`
- **CWE:** CWE-20 (Input Validation) · CWE-400 (DoS)
- **DREAD:** D=5 R=5 E=6 A=7 D=6 → **5.8**
- **Confirmado por:** injection
- **Descrição:** `recordAudit` aceita `Metadata map[string]any` sem cap. Atacante admin inunda audit_log com blobs multi-MB → disk exhaustion + slow queries. Audit write é best-effort (não em tx).
- **Fix:** Cap `ActorEmail`/`IP`/metadata serializado em 16 KiB. Validar formato de email. `actor` deve vir do JWT/session, não de header.

### M2. Per-IP rate limit defeated by `chimiddleware.RealIP`
- **Arquivos:** `internal/transport/http/server/server.go:62`; `internal/transport/adminweb/middleware/ratelimit/ratelimit.go:96-111`
- **CWE:** CWE-348 (Use of Less Trusted Source)
- **DREAD:** D=5 R=6 E=6 A=7 D=5 → **5.8**
- **Confirmado por:** web
- **Descrição:** `chimiddleware.RealIP` reescreve `r.RemoteAddr` a partir de `X-Forwarded-For` *antes* do rate limiter. Atrás de proxy permissivo, atacante envia `X-Forwarded-For: 1.2.3.4` → bucket novo a cada request. Lockout (per-email) é a defesa real, mas defense-in-depth enfraquecido.
- **Fix:** (a) Remover `RealIP` da chain global e aplicar só em rotas que precisam. (b) Salvar `r.RemoteAddr` original em context antes do rewrite, e rate limiter lê esse valor.

### M3. API error responses leak internal error strings
- **Arquivo:** `internal/transport/http/api/handlers.go:174, 224, 274, 321, 390, 451`
- **CWE:** CWE-209 (Information Exposure Through Error Messages)
- **DREAD:** D=5 R=8 E=7 A=6 D=4 → **6.0**
- **Confirmado por:** web
- **Descrição:** `writeError(w, 500, "send failed: "+err.Error())` reflete pgx/whatsmeow errors (URLs, SQL fragments, hostnames) para o cliente.
- **Fix:** Log full error server-side; retornar mensagem estável + correlation ID opaco.

### M4. Session store TOCTOU entre expiry check e delete
- **Arquivo:** `internal/adapter/cache/memory/session.go:130-144, 169-183`
- **CWE:** CWE-367 (TOCTOU)
- **DREAD:** D=2 R=3 E=2 A=2 D=3 → **2.4**
- **Confirmado por:** concurrency
- **Descrição:** `Get` faz `RLock → check → RUnlock → if expired: Lock → delete → Unlock`. Entre RUnlock e Lock, `Save` (refresh on `Resolve`) pode reinserir uma entry fresh, que é apagada pelo reader expirado. Próximo request: re-login forçado.
- **Fix:** Single `Lock` para o check-and-act.

### M5. `dekCache.Get` retorna slice que aliasa storage interno
- **Arquivo:** `internal/usecase/secrets/cache.go:66-86, 90-102`
- **CWE:** CWE-362 (Race), secret-handling weakness
- **DREAD:** D=3 R=2 E=2 A=2 D=2 → **2.2** (race) + secret longevity
- **Confirmado por:** concurrency
- **Descrição:** Get retorna `e.dek` direto. `Invalidate`/`Put` mutam (zero) in-place. Hoje `Keyring.ResolveCredentials` não usa o DEK retornado (usa `cc.WrappedDEK` direto), então impacto é zero. Mas API é racy. Adicionalmente: TTL é *read-driven*; entry nunca lida nunca é zerada até process restart.
- **Fix:** (a) Retornar *copy* de `dek` no Get. (b) Background reaper que expira entries ativamente.

### M6. WebSocket writePump não bound ao request lifecycle
- **Arquivo:** `internal/transport/websocket/handler.go:53-74`
- **CWE:** CWE-400 (Resource Exhaustion)
- **DREAD:** D=2 R=2 E=2 A=2 D=2 → **2.0** (5-min leak sob load)
- **Confirmado por:** concurrency
- **Descrição:** `context.WithTimeout(context.Background(), 5*time.Minute)` não é cancelado quando request/conn termina. `app.HTTPServer.Shutdown` não espera writePumps. 5 min de leak sob carga.
- **Fix:** Passar `r.Context()` (cancelado no conn close) para writePump. Ou WaitGroup per-connection.

### M7. `RunAsPlatform` sempre grava `ActionPlatformAccess`; ação real no JSON metadata
- **Arquivo:** `internal/adapter/repository/postgres/admin/db.go:84-104`
- **CWE:** CWE-778
- **DREAD:** D=4 R=10 E=4 A=8 D=6 → **6.4**
- **Confirmado por:** auth
- **Descrição:** Toda cross-tenant op vira `action = 'platform:access'`; a real fica em `metadata.requested_action`. Audit log não é buscável por ação real. Typed `Action` enum é derrotado.
- **Fix:** Usar a real action (e.g., `tenant:status`) na coluna; flag platform em `{"platform": true}` no metadata. Index em `action`.

### M8. Audit log query sem tenant filter default
- **Arquivo:** `internal/transport/adminweb/handlers_audit.go:10-25`; `internal/adapter/repository/postgres/admin/audit_repo.go:64-104`
- **CWE:** CWE-200
- **DREAD:** D=6 R=10 E=7 A=8 D=6 → **7.4**
- **Confirmado por:** auth
- **Descrição:** `handleAuditList` passa `AuditFilter{Limit:100}` sem scope. Combinado com C4, qualquer user lê audit entries de todos os tenants (incluindo platform ops, tentativas de login com IP/UA, rotações KEK).
- **Fix:** Default filter = caller's tenant; "all tenants" toggle só para platform-admins; hash IP/UA para non-platform viewers.

### M9. Argon2 `Verify` ignora params encoded no hash
- **Arquivo:** `internal/adapter/auth/argon2/argon2.go:81-90, 92-108`
- **CWE:** CWE-757
- **DREAD:** D=4 R=5 E=4 A=9 D=6 → **5.6**
- **Confirmado por:** auth
- **Descrição:** `parseEncoded` lê `m=…,t=…,p=…` mas só retorna salt+hash. `Verify` usa os params do hasher local, não os do hash. Aumentar params no futuro: todos os users existentes ficam trancados (verify não bate). Forward migration impossível sem "password reset week".
- **Fix:** `parseEncoded` retorna `(m, t, p, keyBytes, salt, hash, err)`; `Verify` usa params do hash. Re-hash on next successful login.

### M10. Lockout off-by-one (5 vs 6)
- **Arquivo:** `internal/usecase/auth/lockout/lockout.go:60-77, 82-101`
- **CWE:** CWE-307
- **DREAD:** D=4 R=10 E=6 A=7 D=5 → **6.4**
- **Confirmado por:** auth
- **Descrição:** Docstring diz "5 fails / 15min"; código é `count > maxFails` (com `maxFails=5`, lock no 6º). Atacante ganha 1 tentativa extra vs expectativa.
- **Fix:** `count >= maxFails`. Considerar tighter HTTP rate + backoff exponencial.

### M11. `audit_log` actor vazio + role snapshot ausente
- **Arquivos:** `internal/transport/adminweb/handlers_backup.go:51-58, 114-122`; `handlers_reset.go:43-50`
- **CWE:** CWE-778
- **DREAD:** D=3 R=10 E=3 A=7 D=5 → **5.6**
- **Confirmado por:** auth
- **Descrição:** `actor.ID = principal.UserID` é setado, mas `Principal.Permissions`/`Roles` são `nil` (vazio no session middleware). Audit log não registra *o que o actor estava autorizado a fazer*; só o que fez.
- **Fix:** Persistir snapshot: `actor_roles TEXT[]`, `actor_permissions TEXT[]` em cada audit row. Sobrevive demotion/deletion.

### M12. No password-reset flow
- **CWE:** CWE-640
- **DREAD:** D=5 R=0 E=0 A=7 D=5 → **3.4**
- **Confirmado por:** auth
- **Descrição:** Sem `password_reset_tokens`, sem `/forgot-password`. Admin que esquece password precisa de re-run setup (no-op se admin já existe) ou DB direto.
- **Fix:** Tabela `password_reset_tokens(id, user_id, token_hash, expires_at, used_at)`. Token ≥128 bits de `crypto/rand`, single-use, TTL 15min, rate-limited endpoint, audit.

### M13. `chi.Logger` loga URL full (com query) — OIDC code/state em logs
- **Arquivo:** `internal/transport/http/server/server.go:63`
- **CWE:** CWE-532
- **DREAD:** D=3 R=8 E=6 A=5 D=4 → **5.2**
- **Confirmado por:** web
- **Descrição:** Chi default Logger inclui full query. `?code=...&state=...` no callback OIDC vai para o log aggregator. Blast radius depende de quem lê os logs.
- **Fix:** Custom Logger formatter que redact `code`/`state`/`password`/`csrf_token`; loga só keys, não values.

### M14. `chimiddleware.Timeout(60s)` global em /api
- **Arquivo:** `internal/transport/http/server/server.go:65`
- **CWE:** CWE-400
- **DREAD:** D=4 R=4 E=4 A=4 D=3 → **3.8**
- **Confirmado por:** web
- **Descrição:** 60s wrap em /api. Cliente autenticado malicioso/slow segura 1 conexão por goroutine até o timeout. Shorter timeouts (15-30s) em state-changing endpoints.
- **Fix:** Mover timeout por route; reduzir para 30s em /api, 5s em /webhooks.

### M15. Role ID gerado de `time.Now().UnixNano()` — previsível e collision-prone
- **Arquivo:** `internal/adapter/repository/postgres/admin/role_repo.go:102`
- **CWE:** CWE-330 · CWE-340
- **DREAD:** D=5 R=3 E=4 A=6 D=5 → **4.6**
- **Confirmado por:** crypto
- **Descrição:** Role ID = `role_<unixnano-hex>`. Previsível (attacker sabe roughly quando role foi criada) e collisivo em inserts concorrentes (mesmo nanosecond).
- **Fix:** `role_<uuid.NewString()>` (em linha com `user_repo.go:138`).

### M16. WS writePump / per-connection goroutines não canceláveis no shutdown
- **(já em M6)**, e concurrency agent reforça

---

## Findings — LOW (DREAD 1.0-3.9)

### L1. Dockerfile roda como root
- **Arquivo:** `deployments/Dockerfile:13-29`
- **CWE:** CWE-250
- **DREAD:** 2.0
- **Confirmado por:** web
- **Fix:** `RUN adduser -D -u 10001 mez && USER mez`.

### L2. `docker-compose.yml` shippa dev credentials em plaintext
- **Arquivo:** `deployments/docker-compose.yml:5-7, 23-24, 43-55`
- **CWE:** CWE-798
- **DREAD:** 2.2
- **Confirmado por:** web
- **Descrição:** Hardcoded `mez_dev_pass`, `mezgo_dev_pass`, master key `dGVzdC1tYXN0ZXIta2V5...`, dev session secret.
- **Fix:** Remover defaults. Fail-fast em `Load()` se env vars vazias ou iguais a known-dev.

### L3. MinIO console (:9001) e Postgres (:5432) expostos em 0.0.0.0
- **Arquivo:** `deployments/docker-compose.yml:8-9, 25-27`
- **CWE:** CWE-668
- **DREAD:** 1.5
- **Fix:** Bind a `127.0.0.1`; documentar que prod deve mover para rede privada.

### L4. CSRF `XSRF-TOKEN` cookie sem `__Host-` prefix, sem `Secure`
- **Arquivo:** `internal/transport/adminweb/middleware/csrf.go:46-53`
- **CWE:** CWE-1004
- **DREAD:** 1.5
- **Fix:** `__Host-xsrf` + `Secure=true` (requer o fix do C3 primeiro).

### L5. CSRF `Exempt` match é exact-string, não prefix
- **Arquivo:** `internal/transport/adminweb/middleware/csrf.go:30-41`
- **CWE:** CWE-352
- **DREAD:** 1.5
- **Fix:** Documentar como exact-path; nunca eximir state-changing verbs.

### L6. `RealIP` aplicado a /webhooks
- **Arquivo:** `internal/transport/http/server/server.go:62`
- **CWE:** CWE-348
- **DREAD:** 1.5
- **Fix:** Restringir `RealIP` a rotas que precisam; não aplicar em `/webhooks/*`.

### L7. `errOrEmpty` expõe channel error no JSON
- **Arquivo:** `internal/transport/http/api/handlers.go:454-459`
- **CWE:** CWE-209
- **DREAD:** 1.5
- **Fix:** Retornar enum fixo (`"ok"`/`"down"`/`"degraded"`).

### L8. `SessionSecret` exigido mas não usado como signing key
- **Arquivos:** `pkg/config/config.go:27, 99-104`; `internal/transport/adminweb/middleware/session.go:22-46`
- **CWE:** CWE-1188
- **DREAD:** 1.6
- **Confirmado por:** crypto
- **Descrição:** Session ID é 256-bit unguessable, validado por server-side store. Cookie não é HMAC-signed. `SessionSecret` é exigido por config mas ignorado.
- **Fix:** (a) Assinar cookie com HMAC-SHA256(SessionSecret, sessionID); ou (b) remover o requirement e documentar.

### L9. Argon2id defaults no mínimo OWASP 2024
- **Arquivo:** `internal/adapter/auth/argon2/argon2.go:27-35`
- **CWE:** CWE-916
- **DREAD:** 1.6
- **Fix:** Bump para `Memory: 64*1024, Iterations: 3, Parallelism: 4` (ou `Memory: 128*1024, t=2, p=2`).

### L10. `Reconciler.ReconcileAll` / `Relay.process` sem `recover` direto
- **Arquivos:** `internal/usecase/outbox/relay.go:128-199`; `reconcile/reconciler.go:77-145`
- **CWE:** CWE-248
- **DREAD:** 1.6
- **Confirmado por:** concurrency
- **Descrição:** `Runner.Run` (lifecycle/runner.go:291) recupera no nível da goroutine, então OK em produção. Mas `ReconcileAll` é chamado direto (test path) e em caso de panic em `process`/`assign`, o resto do batch é abortado.
- **Fix:** Inline `recover()` em `drain` e `ReconcileAll` para defense-in-depth.

### L11. `WhoisMeow.Dispatcher.Start` não chamado em produção
- **Arquivo:** `internal/adapter/provider/whatsmeow/dispatcher.go:46-53`
- **CWE:** CWE-400
- **DREAD:** 1.2
- **Descrição:** Possível dead code em prod path (events nunca entregues), OU start só em testes. Verificar se `*whatsmeow.Client` real chama `HandleRaw` síncrono.
- **Fix:** Chamar `d.Start` no Manager, ou documentar por que não.

### L12. `google/uuid` v1 vs v4 check
- **DREAD:** 1.2
- **Confirmado por:** concurrency
- **Fix:** `grep "uuid.NewV[0-9]"` para garantir que só v4 é usado (random, sem MAC leak).

### L13. Hub.Broadcast silent drop on closed subscriber
- **Arquivo:** `internal/transport/websocket/hub.go:105-120`
- **CWE:** CWE-662
- **DREAD:** 1.0
- **Confirmado por:** concurrency
- **Descrição:** `select { case s.send <- msg: default: drop }` em channel fechado não panic (retorna zero), mas event é silenciosamente perdido.
- **Fix:** Log "subscriber closed during broadcast".

### L14. `runner.go` phase slice read sem lock
- **Arquivo:** `pkg/lifecycle/runner.go:98-217`
- **CWE:** CWE-662
- **DREAD:** 1.6
- **Confirmado por:** concurrency
- **Fix:** Snapshot phases sob lock antes de iterar.

### L15. `whatsmeow.Manager.GetOrCreate` segura lock durante `Disconnect`
- **Arquivo:** `internal/adapter/provider/whatsmeow/manager.go:106-111`
- **CWE:** CWE-662
- **DREAD:** 1.4
- **Fix:** Drop lock antes de `Disconnect`; ou `Disconnect` em goroutine separada.

### L16. Context key para tenant é string-typed
- **Arquivos:** `internal/transport/http/api/handlers.go:34, 474-487`
- **CWE:** CWE-1007
- **DREAD:** 1.4
- **Fix:** Usar struct{} ou int constant como context key; não exportar `ContextWithTenant`.

### L17. S3 `publicBase` não valida scheme/host
- **Arquivo:** `internal/adapter/storage/s3/s3.go:94`
- **CWE:** CWE-20
- **DREAD:** 1.6
- **Confirmado por:** injection
- **Fix:** Validar com `net/url`; rejeitar scheme não-vazio.

### L18. IntToStr custom helper em telegram handler
- **Arquivo:** `internal/adapter/webhook/telegram/handler.go:177-196`
- **CWE:** CWE-20
- **DREAD:** 1.7
- **Fix:** Usar `strconv.FormatInt`.

---

## Findings — INFO (não-fix, mas documentar)

- **Permissions-Policy ausente** — `secheaders.go:1-19`. Adicionar `camera=(), microphone=(), geolocation=(), payment=()`.
- **Referrer-Policy: same-origin** — `secheaders.go:10`. Considerar `no-referrer` (admin panel) ou `strict-origin-when-cross-origin` (geral).
- **SameSite=Lax no session cookie** — login CSRF trade-off documentado. Considerar `Strict`.
- **Logout invalida server-side session** — `handlers_auth.go:94-108`. Bom.
- **Lockout per-email reseta on success** — `lockout.go`. Bom.
- **`math/rand`** — zero usos (confirmado por grep). Toda entropia via `crypto/rand`.
- **CSRF token via `crypto/rand`** — `csrf.go:87`. Bom.
- **AES-GCM nonce por `crypto/rand` por chamada** — `pkg/crypto/envelope.go:56-59, 105-108`. Bom.
- **HMAC body buffering** — webhooks computam HMAC sobre bytes exatos (sem re-encode). Bom.
- **Constant-time compare** — `hmac.Equal` e `subtle.ConstantTimeCompare` consistentemente. Bom.
- **Body size limits** — 2 MiB Meta, 1 MiB Telegram. Bom.
- **DEK zeroization** — `defer zero(dek)` consistente. Aceitável (limitação do Go).
- **Argon2id** — `argon2.IDKey` (id variant). Bom.
- **No replace directives em go.mod** — sem bypass de supply chain. Bom.
- **go-jose v4** (post-CVE-2024-28144). Bom.
- **pgx, viper, coreos/go-oidc, oapi-codegen, prometheus, zerolog, oauth2, golang-migrate, golang-migrate/migrate, gorilla/websocket 1.5.3** — sem CVEs ativos conhecidos.

---

## Resumo executivo — tabela DREAD-sorted

| # | Sev | Finding | Arquivo | DREAD |
|---|-----|---------|---------|-------|
| 1 | CRITICAL | Admin handlers: auth sem authz | `adminweb/handlers_*.go` | 9.0 |
| 2 | CRITICAL | IDOR API REST (sem `RunInTenantTx`) | `api/handlers.go:89-452` | 9.0 |
| 3 | CRITICAL | Cookie `__Host-` sem `Secure` | `handlers_auth.go:81-89` | 9.0 |
| 4 | CRITICAL | Actor backup controlável via header | `api/handlers_backup.go:52-61` | 8.6 |
| 5 | CRITICAL | Privilege escalation via role editor | `adminweb/handlers_roles.go:47-73` | 8.6 |
| 6 | CRITICAL | WebSocket `CheckOrigin: return true` | `websocket/hub.go:260-267` | 8.6 |
| 7 | CRITICAL | JWT sem `exp` + secret default público | `bearer.go:70-116`, `server.go:82-88` | 8.4 |
| 8 | CRITICAL | Webhook body em cleartext no log | `meta/handler.go:138` | 6.6 (PII weight) |
| 9 | CRITICAL | Backup restore aceita `_table` arbitrário | `restore.go:316-381` | 7.6 |
| 10 | CRITICAL | S3 keys sem validar `tenantID` | `backup/export.go:284, reset.go:166` | 7.2 |
| 11 | HIGH | OIDC `next` open redirect | `handlers_auth.go:110-166` | 8.0 |
| 12 | HIGH | No TLS termination / redirect | `wire.go:345-351` | 7.4 |
| 13 | HIGH | `actorEmail` não setado em login audit | `login.go:219-227` | 7.2 |
| 14 | HIGH | Master key file sem 0600 check | `config.go:82-93` | 6.2 |
| 15 | HIGH | Setup CLI password sem validação | `setup.go:19-25` | 7.6 |
| 16 | HIGH | `RunAsPlatform` audit best-effort | `service.go:148-207` | 6.2 |
| 17 | HIGH | Security headers `secure=false` | `adminweb/server.go:109` | 6.8 |
| 18 | HIGH | `ReadHeaderTimeout` 0 (slow-loris) | `wire.go:345-351` | 6.0 |
| 19 | HIGH | JWT secret sem length check | `config.go:38` | 5.0 |
| 20 | HIGH | OIDC `nonce` não validado | `oidc/oidc.go:46-47` | 5.8 |
| 21 | HIGH | CSRF `/setup` sem validação | `handlers_setup.go:66-145` | 3.8 (web: 7.2) |
| 22 | HIGH | `Bus.Unsubscribe*` por `reflect.Pointer()` | `bus.go:184-210` | 2.8 (concurrency: 14) |
| 23 | HIGH | `OutboxRepo.ClaimNext` sem tx | `outbox.go:104-114` | 2.6 (concurrency: 13) |
| 24 | HIGH | `Bus.Publish*` TOCTOU com Drain | `bus.go:85-150` | 2.6 (concurrency: 13) |
| 25 | HIGH | Labstack Echo em dead code | `go.mod:12`, `api/openapi.gen.go` | 2.2 (concurrency: 11) |
| 26 | MED | Audit log sem tenant filter | `handlers_audit.go:10-25` | 7.4 |
| 27 | MED | Setup password CLI sem validação | `setup.go:19-25` | 7.6 (overlap com 15) |
| 28 | MED | `RunAsPlatform` action mask | `admin/db.go:84-104` | 6.4 |
| 29 | MED | API error responses leak | `api/handlers.go:174,224,…` | 6.0 |
| 30 | MED | Lockout off-by-one | `lockout.go:60-77` | 6.4 |
| 31 | MED | Audit JSONB metadata sem cap | `api/handlers_backup.go:69` | 5.8 |
| 32 | MED | `RealIP` defeta rate limit | `server.go:62`, `ratelimit.go:96-111` | 5.8 |
| 33 | MED | OIDC nonce não validado | `oidc/oidc.go:46-47` | 5.8 (overlap com 20) |
| 34 | MED | Argon2 Verify ignora encoded params | `argon2.go:81-90` | 5.6 |
| 35 | MED | Audit `actorEmail` vazio | `login.go:219-227` | 7.2 (overlap com 13) |
| 36 | MED | `actor.ID` vazio / role snapshot ausente | `handlers_backup.go:51-58` | 5.6 |
| 37 | MED | `chi.Logger` loga URL full | `server.go:63` | 5.2 |
| 38 | MED | Role ID via `time.Now().UnixNano()` | `role_repo.go:102` | 4.6 |
| 39 | MED | `chimiddleware.Timeout(60s)` global | `server.go:65` | 3.8 |
| 40 | MED | No password-reset flow | projeto-wide | 3.4 |
| 41 | LOW | `dekCache.Get` retorna alias | `cache.go:66-86` | 2.2 |
| 42 | LOW | Session store TOCTOU | `session.go:130-183` | 2.4 |
| 43 | LOW | WS writePump sem cancel context | `handler.go:53-74` | 2.0 |
| 44 | LOW | Dockerfile roda como root | `Dockerfile:13-29` | 2.0 |
| 45 | LOW | docker-compose ship dev creds | `docker-compose.yml:5-55` | 2.2 |
| 46 | LOW | CSRF `XSRF-TOKEN` sem `__Host-` | `csrf.go:46-53` | 1.5 |
| 47 | LOW | `ErrNotConfigured`/`ErrCredentialsNotFound` duplicados | 3 lugares | 1.5 |
| 48 | LOW | `S3 publicBase` sem validação | `s3.go:94` | 1.6 |
| 49 | LOW | CSRF `Exempt` exact-string | `csrf.go:30-41` | 1.5 |
| 50 | LOW | `RealIP` em /webhooks | `server.go:62` | 1.5 |
| 51 | LOW | `errOrEmpty` expõe error | `api/handlers.go:454-459` | 1.5 |
| 52 | LOW | `SessionSecret` exigido mas não usado | `config.go:27,99` | 1.6 |
| 53 | LOW | Argon2id defaults borderline | `argon2.go:27-35` | 1.6 |
| 54 | LOW | `Reconciler/Relay` sem recover inline | `relay.go:128`, `reconciler.go:77` | 1.6 |
| 55 | LOW | WhoisMeow `Dispatcher.Start` | `dispatcher.go:46-53` | 1.2 |
| 56 | LOW | `google/uuid` v1 vs v4 check | projeto-wide | 1.2 |
| 57 | LOW | `Hub.Broadcast` silent drop | `hub.go:105-120` | 1.0 |
| 58 | LOW | `runner.go` phase slice read | `runner.go:98-217` | 1.6 |
| 59 | LOW | `whatsmeow.Manager.GetOrCreate` lock+IO | `manager.go:106-111` | 1.4 |
| 60 | LOW | Context key string-typed | `api/handlers.go:34` | 1.4 |
| 61 | LOW | `intToStr` custom helper | `telegram/handler.go:177` | 1.7 |

---

## Recomendação de sequência (pré-1.0)

### Dia 1-2: production blockers (1-2 devs, ~2 dias)
1. C1 — WebSocket `CheckOrigin` (15 min)
2. C2 — JWT `exp` + remover dev fallback + `ValidateServe` exige `APIJWTSecret ≥ 32` (1h)
3. C3 — Cookie `__Host-` com `Secure` plumbed via config (2h)
4. C8 — Webhook body PII no log (30 min; adicionar `zerolog.Hook` global)
5. C5 — IDOR API REST (decidir: wrap em `RunInTenantTx` ou `WHERE tenant_id` + reject; ~4h)
6. C4 — Wire `Principal.Permissions` no session middleware + `Evaluate` em cada handler (~1 dia)
7. C6 — Actor do backup do JWT, não do header (2h)
8. C7 — Role editor: `Evaluate(PermUpdateRoles, ScopePlatform)` + reject platform-only perms em Tenant role (4h)

### Dia 3-4: defense in depth
9. C9 — Allowlist `backupableTables` com colunas (4h)
10. C10 — `tenantID` UUID validation + per-tenant authz no reset (4h)
11. H1 — Open redirect `next` validation (30 min)
12. H3 — Master key file 0600 + O_NOFOLLOW (2h)
13. H4 — `ValidatePassword` compartilhada (4h)
14. H12 — TLS termination (config + ListenAndServeTLS, ou documentar proxy-only) (4h)
15. H14 — `ReadHeaderTimeout: 5s` (5 min)
16. H8, H9, H10 — Concurrency (1 dia)

### Dia 5: hardening
17. H5 — `RunAsPlatform` atomic no backup
18. H13 — CSP completa + nonces
19. M2 — `RealIP` scope (admin only)
20. M3 — Error messages sem leak
21. M7, M8 — Audit log com real action + tenant filter
22. M11 — Role snapshot no audit
23. L8, L9 — Session cookie HMAC + Argon2 bump

### Dia 6-7: long tail
24. Resolver L1-L18
25. Rodar `govulncheck ./...` no CI
26. Adicionar testes de regressão: RLS fail-closed (existente), CSWSH, IDOR, role escalation
27. Threat model review (STRIDE completo) antes de 1.0

---

*Última atualização: junho/2026. Audit pré-1.0. 5 sub-agentes paralelos (injection, crypto+secrets, web+headers, auth+authz, concurrency+deps) — todos os achados foram confirmados em pelo menos 1 agente; CRITICAL/HIGH com cross-agent consensus estão marcados.*
