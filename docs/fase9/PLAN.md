# Fase 9 — Maturidade de produção SMM (whatsmeow real + escala horizontal + credenciais live)

> **Status:** planejamento · junho/2026 · tracking em `fase9-tracking`.
> **Escopo:** 20 carryovers de segurança (#131, #132, #133, #135, #137, #138, #140, #142, #143, #145, #146, #147, #148, #149, #150, #151, #153, #154, #155, #157 — Sprint 0, pré-requisito) + 1 carryover executado (#158) + 18 issues novas (#159–#176) + 4 carryovers de design (#177–#180, Seção 11) = **43 issues · 39 execução + 4 roadmap · ~32d solo** (5-6 sprints) · single commit (squash) por sprint → `main`. **ADRs:** 8 (0022-0029) + 2 roadmap (0030-0031) + 9 complementares (0032-0040) + 3 segurança (0041-0043) = **22 ADRs formais**.
> **Pré-requisitos:** Fases 0–8 merged (Fase 8 commit `bdee3cd` em `main`).
> **Base normativa:** `docs/fase8/PLAN.md` (C12 — boot determinístico), `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (C1-C10 + H1-H15 + M1-M15), `docs/fase8/FIXES/002_ARCHITECTURE_REVIEW.md` (item 1.1 SKIP LOCKED).
> **Origem do plano:** auditoria arquitetural 5-pilares (junho/2026) + **auditoria de segurança STRIDE+DREAD 5-domínios** (10 CRITICAL + 15 HIGH + 15 MEDIUM) — gateway omnichannel `mez-go-mono` avaliado contra critérios de mercado de Social Media Management (SMM) multiredes.
> ⚠️ **9 issues da auditoria de segurança já foram mergeadas em `main` via `bdee3cd` (PR #108) mas o `Closes` não foi propagado**: #129, #130, #134, #136, #139, #141, #144, #152, #156. Estas devem ser fechadas como parte do housekeeping do Sprint 0 (ver §12.0).

### Mapeamento issue → escopo (carryovers + novas)

| Issue | Pilar | Título | Tipo |
|------:|-------|--------|------|
| [#131](https://github.com/felipedsvit/mez-go-mono/issues/131) | **P6** | **S0-C3** — Cookie `__Host-mez_admin` sem `Secure: true` (C3 audit) | FIX |
| [#132](https://github.com/felipedsvit/mez-go-mono/issues/132) | **P6** | **S0-C4** — Admin handlers: autenticação sem autorização (C4 audit) | REWRITE |
| [#133](https://github.com/felipedsvit/mez-go-mono/issues/133) | **P6** | **S0-C5** — IDOR API REST: handlers não usam `RunInTenantTx` (C5 audit) | REWRITE |
| [#135](https://github.com/felipedsvit/mez-go-mono/issues/135) | **P6** | **S0-C7** — Privilege escalation via role editor (C7 audit) | REWRITE |
| [#137](https://github.com/felipedsvit/mez-go-mono/issues/137) | **P6** | **S0-C9** — Backup restore aceita `_table` arbitrário (defense-in-depth SQLi, C9 audit) | FIX |
| [#138](https://github.com/felipedsvit/mez-go-mono/issues/138) | **P6** | **S0-C10** — S3 keys/prefixos sem validar `tenantID` (path confusion, C10 audit) | FIX |
| [#140](https://github.com/felipedsvit/mez-go-mono/issues/140) | **P6** | **S0-H2** — OIDC `nonce` não validado (replay de ID-token, H2 audit) | FIX |
| [#142](https://github.com/felipedsvit/mez-go-mono/issues/142) | **P6** | **S0-H6b** — JWT secret sem check de length/entropy no startup (H6 — diferente de #144) | FIX |
| [#143](https://github.com/felipedsvit/mez-go-mono/issues/143) | **P6** | **S0-H14b** — `ReadHeaderTimeout=0` no `http.Server` (slow-loris, H14) | FIX |
| [#145](https://github.com/felipedsvit/mez-go-mono/issues/145) | **P6** | **S0-H7** — CSRF `/setup` POST sem validação (apenas leitura do token, H7) | FIX |
| [#146](https://github.com/felipedsvit/mez-go-mono/issues/146) | **P6** | **S0-H13** — Security headers sempre invocados com `secure=false` (H13) | FIX |
| [#147](https://github.com/felipedsvit/mez-go-mono/issues/147) | **P6** | **S0-H2-dup** — Possível duplicata de #140 (a confirmar; fechar como `duplicate` se idêntico) | TRIAGE |
| [#148](https://github.com/felipedsvit/mez-go-mono/issues/148) | **P6** | **S0-H5** — `RunAsPlatform` audit é best-effort, não atômico (H5) | FIX |
| [#149](https://github.com/felipedsvit/mez-go-mono/issues/149) | **P6** | **S0-H8+H9+H10** — Concorrência: `bus.UnsubscribeInbound` `reflect.Pointer`, `OutboxRepo.ClaimNext` race, drain TOCTOU | REWRITE |
| [#150](https://github.com/felipedsvit/mez-go-mono/issues/150) | **P6** | **S0-H11** — `labstack/echo` pulled por dead code (`api/openapi.gen.go`); supply-chain risk | FIX |
| [#151](https://github.com/felipedsvit/mez-go-mono/issues/151) | **P6** | **S0-H12** — Sem TLS termination / sem redirect HTTP→HTTPS (H12) | FIX |
| [#153](https://github.com/felipedsvit/mez-go-mono/issues/153) | **P6** | **S0-M3** — API error responses leak internal error strings (M3) | FIX |
| [#154](https://github.com/felipedsvit/mez-go-mono/issues/154) | **P6** | **S0-M8** — Audit log query sem tenant filter default (M8) | FIX |
| [#155](https://github.com/felipedsvit/mez-go-mono/issues/155) | **P6** | **S0-M10-dup** — Possível duplicata de #156 (a confirmar; fechar como `duplicate` se idêntico) | TRIAGE |
| [#157](https://github.com/felipedsvit/mez-go-mono/issues/157) | **P6** | **S0-M15** — Role ID via `time.Now().UnixNano()` (previsível/collisivo, M15) | FIX |
| [#158](https://github.com/felipedsvit/mez-go-mono/issues/158) | P5 | A1 — Substituir `stubWhatsmeowClient` por `*whatsmeow.Client` real (Fase 9) | NEW · carryover |
| [#159](https://github.com/felipedsvit/mez-go-mono/issues/159) | P2 | A2 — `OutboxRepo.ClaimNext` em `BeginTx` (`FOR UPDATE SKIP LOCKED` funcional) | REWRITE |
| [#160](https://github.com/felipedsvit/mez-go-mono/issues/160) | P2 | A3 — Circuit breaker per `(tenant, channel)` em volta de `Sender.Send` | NEW |
| [#161](https://github.com/felipedsvit/mez-go-mono/issues/161) | P2 | A4 — Jitter + backoff exponencial persistente em outbox retry | REWRITE |
| [#162](https://github.com/felipedsvit/mez-go-mono/issues/162) | P5 | A5 — Handlers webhook Meta/Telegram retornam 200 antes de ingestar (true async) | REWRITE |
| [#163](https://github.com/felipedsvit/mez-go-mono/issues/163) | P4 | A6 — Scheduled posts: coluna `scheduled_at` + query filtrada + índice parcial | NEW |
| [#164](https://github.com/felipedsvit/mez-go-mono/issues/164) | P2 | B1 — Per-tenant + per-channel rate limit (token-bucket in-memory) | NEW |
| [#165](https://github.com/felipedsvit/mez-go-mono/issues/165) | P3 | B2 — Token refresh automático para Meta (`fb_exchange_token`) | NEW |
| [#166](https://github.com/felipedsvit/mez-go-mono/issues/166) | P3 | B3 — OIDC `nonce` validation + persistência de `refresh_token` cifrado (H2 audit) | ADAPT |
| [#167](https://github.com/felipedsvit/mez-go-mono/issues/167) | P3 | B4 — Webhook secrets (Meta app, Telegram bot) migrados para envelope encryption | ADAPT |
| [#168](https://github.com/felipedsvit/mez-go-mono/issues/168) | P5 | B5 — In-memory dedup cache no edge dos webhooks (`sync.Map` + TTL 5min) | NEW |
| [#169](https://github.com/felipedsvit/mez-go-mono/issues/169) | P2 | B6 — DLQ consumer default + métrica `DLQTotal` + alerta Prometheus | NEW |
| [#170](https://github.com/felipedsvit/mez-go-mono/issues/170) | P1 | C1 — Remover `port.Channel` órfão + reconciliar `domain.ChannelWAWeb` vs `event.ChannelWAWeb` | MECHANICAL |
| [#171](https://github.com/felipedsvit/mez-go-mono/issues/171) | P2 | C2 — Particionar `bus.Bus` por tenant (ou quota per-tenant) | REWRITE |
| [#172](https://github.com/felipedsvit/mez-go-mono/issues/172) | P1 | C3 — Capability matrix whatsmeow: implementar ou remover (honest matrix) | ADAPT |
| [#173](https://github.com/felipedsvit/mez-go-mono/issues/173) | P5 | C4 — `goleak.VerifyTestMain` em `webhook/`, `bus/`, `outbox/`, `reconcile/` | MECHANICAL |
| [#174](https://github.com/felipedsvit/mez-go-mono/issues/174) | P2 | C5 — Health check per-channel real (`Sender.Ping(ctx)` interface + 5 adapters) | NEW |
| [#175](https://github.com/felipedsvit/mez-go-mono/issues/175) | P5 | C6 — Testes E2E validando async webhook (t_200 < 50ms com DB tx 500ms) | NEW |
| [#176](https://github.com/felipedsvit/mez-go-mono/issues/176) | P4 | A6.1 — UI/UX API para agendar posts (endpoint + `cron` reconciler) | NEW |
| [#177](https://github.com/felipedsvit/mez-go-mono/issues/177) | P2 | D1 — `coordinator/registry`: schema `coordinator_capabilities` + advertise heartbeat `[Seção 11 — roadmap]` | NEW · carryover Seção 11 |
| [#178](https://github.com/felipedsvit/mez-go-mono/issues/178) | P2 | D2 — `coordinator/claim`: `pg_try_advisory_lock` + lease TTL 60s `[Seção 11 — roadmap]` | NEW · carryover Seção 11 |
| [#179](https://github.com/felipedsvit/mez-go-mono/issues/179) | P2 | D3 — `coordinator/lease`: heartbeat goroutine + renew + lost detection `[Seção 11 — roadmap]` | NEW · carryover Seção 11 |
| [#180](https://github.com/felipedsvit/mez-go-mono/issues/180) | P2 | D4 — `coordinator/migrate`: graceful session handoff + reconciler para orphans `[Seção 11 — roadmap]` | NEW · carryover Seção 11 |

> **Legenda pilares:** P1=Adapter/Factory · P2=Resiliência/RateLimit · P3=Credenciais · P4=Agendamento · P5=Webhooks/Real-time · **P6=Segurança (Sprint 0)**.
> **Carryovers:** #158 (whatsmeow real) + #131, #132, #133, #135, #137, #138, #140, #142, #143, #145, #146, #147, #148, #149, #150, #151, #153, #154, #155, #157 (auditoria Fase 8 não mergeados, Sprint 0) + #177-#180 (Seção 11, **execução em Fase 10+** — apenas design documentado aqui).
> **Dependência crítica Sprint 0 → Sprint 1–5:** `RunInTenantTx` correto (#133) é pré-requisito para qualquer handler novo (#162, #165, #166, #167); admin authorization (#132) é pré-requisito para endpoints admin novos (#169, #174); OIDC nonce (#140) é pré-requisito para #166.

---

## 1. Contexto e motivação

A **Fase 8** estabilizou o **cabeamento do processo único** (C12): boot determinístico, shutdown coordenado, `goleak`, chaos tests. O resultado é um gateway omnichannel **internamente íntegro**, com `core/port` limpo, adapters intercambiáveis, RLS fail-closed, envelope encryption KEK→DEK, bus in-process tipado, reconciler C1 e recovery pós-`kill -9` validado.

O que **Fase 8 não cobriu** — e que a **auditoria arquitetural 5-pilares** (junho/2026) identificou como **gargalos para mercado SMM** — são as **dimensões de escala, tempo-real e lifecycle** que distinguem um gateway de mensageria de uma plataforma de Social Media Management competitiva:

1. **Resiliência "inteligente"** está ausente: zero circuit breakers, zero jitter em backoff, bus global não particionado por tenant, per-tenant throughput rate limit inexistente. Em produção SMM, um tenant barulhento (broadcast de Black Friday, 50k msgs em 1h) **derruba todos os outros** ao saturar o `bus.inbound` global (1024 buffer) e o quota compartilhado do `app_id` Meta.

2. **Pipelines webhook não são realmente assíncronos.** O handler de `meta/handler.go:166-176` e `telegram/handler.go:118-124` **bloqueia o 200 ACK na transação DB inteira** (contact+conv+message+outbox). O comentário em `meta/handler.go:169` é explícito: "Para Fase 2, retornamos 200 e logamos — Meta não retenta após 200." Para volume SMM real, isso significa **erros transientes viram log eterno** e **timeouts de 5s da Meta** derrubam a integração. O plumbing async (bus + recover + drain + reconciler) já existe — só falta o handler usá-lo.

3. **`FOR UPDATE SKIP LOCKED` está quebrado** (H9 audit, `postgres/outbox.go:131-140`). O `AcquireClaimLock` exportado em `outbox.go:284-290` é dead code. Single-process passa despercebido; o momento que alguém tentar scale-out (mesmo que HA futuro) **corrompe o relay**. O fix é mecânico mas bloqueia qualquer plano de multi-instância.

4. **Credenciais têm metade do ciclo de vida.** Envelope encryption KEK→DEK é sólido (Fase 7). Mas Meta/IG/MSG tokens são **estáticos** (`waba/client.go:25, 163-194`); após 60 dias (long-lived) o tenant precisa re-seedar manualmente. Não há `TokenSource`, não há `fb_exchange_token` auto-refresh. O OIDC sequer valida `nonce` (H2 — replay de ID-token) e descarta o `RefreshToken` em `oidc.go:65-79`. **Secrets de webhook** (Meta app secret, Telegram bot token) ainda vivem em env vars plaintext (`webhook/secrets/credentials.go:73-92`) — fora do envelope chain.

5. **Não há scheduled posts.** A coluna `NextAttemptAt` em `domain.OutboxMessage:28` é declarada mas nunca persistida (`domain/outbox.go:114-115`: "backoff in-process, não persistente — o relay recalcula via Attempts"). O outbox drena-as-soon-as-possible. Para SMM, **agendamento é feature core, não polish** — sem `scheduled_at` o produto não compete.

6. **Whatsmeow roda em `stubWhatsmeowClient`** (`internal/adapter/provider/whatsmeow/stub_client.go`) — o que significa que **o canal WhatsApp Web não está realmente testado em produção**. A warmup quota, o reconnect throttle, o dispatcher, tudo está implementado, mas roda contra um stub. #158 (carryover para Fase 9) substitui pelo `*whatsmeow.Client` real.

### 1.1 Inventário de código reusável (mono, carryover Fase 8)

| Componente | Caminho | LOC | Issue destino | Tipo |
|---|---|---:|---|---|
| `pkg/lifecycle.Runner` (boot/shutdown phases) | `pkg/lifecycle/{phase,runner}.go` | ~280 | base de #160, #164, #171 | reuso direto — phases de rate limit + breaker |
| `pkg/metrics.Registry` (Prometheus) | `pkg/metrics/metrics.go` | ~140 | #160, #161, #164, #169 | reuso direto — Counter/Gauge/Histogram para circuit breaker + backoff + DLQ |
| `bus.Bus` (in-process typed) + `safeCall` (recover) | `internal/adapter/broker/bus.go` | 373 | #162, #168, #171 | reuso direto — handler async spawna goroutine + publish |
| `Reconciler.Run` (boot sweep + 30s tick) | `internal/usecase/reconcile/reconciler.go:113` | 150 | #161, #163, #176 | reuso direto — base do scheduler de posts |
| `usecase/outbox.Relay` (drain + notify + DLQ) | `internal/usecase/outbox/relay.go` | 241 | #159, #161, #164, #169 | reuso direto — wrap com breaker + backoff persistente |
| `usecase/secrets.Keyring` (envelope + DEK cache) | `internal/usecase/secrets/keyring.go` | 224 | #165, #166, #167 | reuso direto — `SetCredentials` para refresh + OIDC tokens + webhook secrets |
| `pkg/crypto.Envelope` (AES-256-GCM) | `pkg/crypto/envelope.go` | 143 | #166, #167 | reuso direto — já cobre OIDC refresh + webhook secrets |
| `port.CapabilitySet` + `Resolver.ResolveMessage` | `internal/core/port/{capability,fallback}.go` | ~150 | #172 | reuso direto — base da honest matrix |
| `port.Sender` (3 métodos) | `internal/core/port/sender.go:96` | 138 | #174 | reuso direto — adicionar `Ping(ctx) error` à interface |
| `local_sealer.LocalSealer` (KEK load) | `internal/adapter/crypto/local_sealer.go` | 109 | #166, #167 | reuso direto — mesma chave para webhook secrets |
| `admin_audit_log` (REVOKE UPDATE/DELETE) | `migrations/0002_admin.up.sql:235-236` | n/a | #165, #166, #169 | reuso direto — toda ação nova loga |
| `cmd/server/rotate-kek` (offline KEK rotation) | `cmd/server/rotate-kek.go` | 232 | — | reuso direto — KEK rotation já cobre novos secrets |
| Testcontainers Postgres (Fase 2 carryover) | `internal/testutil/pgtest/pgtest.go` | ~120 | #161, #163, #175 | reuso direto — `//go:build integration` tests |
| `tests/e2e/{harness,webhook_e2e,pipeline_e2e}.go` | `tests/e2e/` | ~800 | #175 | reuso direto — base dos novos E2E |
| `whatsmeow.Manager` (1 client/tenant + LRU) | `internal/adapter/provider/whatsmeow/manager.go` | 258 | #158 | reuso direto — substituir só `stub_client.go` |
| `whatsmeow.ReconnectThrottle` (exponential backoff 60s→30min) | `internal/adapter/provider/whatsmeow/reconnect.go` | 227 | #161 | reuso direto — base do jitter para outras redes |
| `whatsmeow.Warmup` (10→200/dia ramp) | `internal/adapter/provider/whatsmeow/reconnect.go:91-96` | — | #164 | reuso direto — base da per-tenant rate limit |

### 1.2 Patterns obrigatórios (do AGENTS.md, mantidos)

1. **RLS via context, nunca parâmetro** — `RunInTenantTx(ctx, tenantID, fn)` continua mandatório. #163 (scheduled posts) e #165 (Meta refresh) rodam em tenant tx.
2. **FORCE RLS** (C3) — todo o código novo conecta via `mez_app` (sem `BYPASSRLS`).
3. **Functional options** — `breaker.New(WithMaxRequests, WithTimeout)`; `ratelimit.New(WithRate, WithBurst)`.
4. **Audit log em toda ação admin** (D17) — `Keyring.SetCredentials` (já tem), `MetaRefresh`, `OIDCNonceValidation`, `DLQAlert`.
5. **Comentários português** — manter consistência com pai e mono.
6. **Sem imports proibidos** — guardrails: sem `sink/`, `broker/nats`, `pkg/shard`, `cache/redis`, `secret/sealer/vault`. **Sony/gobreaker é adicionado a `go.mod` como dep allowed** (não é Redis, não é NATS, é in-memory pure Go).
7. **recover() por goroutine** (C10) — toda nova goroutine (#162 webhook async, #165 Meta refresh, #175 async E2E) tem `defer recover()`.
8. **Graceful shutdown** (D10) — breaker + rate limit + DLQ consumer herdam phases do `Runner` da Fase 8.
9. **Audit + metrics em toda nova feature** (D17 + observability) — Counter `breaker_state_change_total{channel}`, `outbox_retry_total{channel}`, `dlq_total{channel}`, Histogram `outbox_retry_delay_seconds`.
10. **Testes por issue** (Fase 8 carryover) — `go test -race -shuffle=on ./...` deve fechar verde a cada commit. Integração com `//go:build integration` para o que precisa de Postgres real.

### 1.3 Divergências arquiteturais mono → SMM-produção

| Aspecto | mez-go-mono (Fase 8) | mez-go-mono (Fase 9) | Impacto |
|---|---|---|---|
| **Outbox claim** | `platformPool.Query` (single statement) | **`BeginTx` + `tx.Query` + `tx.Commit`** | H9 fix; scale-out safe |
| **Outbox retry** | sem backoff, sem jitter, `next_attempt_at` fantasma | **backoff `60s * 2^n` capped 30min + jitter `±30s`** persistido em coluna | thundering herd safe |
| **Sender wrap** | `registry.Get → sender.Send` direto | **`registry.Get → breaker.Execute → sender.Send`** | downstream protection |
| **Webhook handler** | 200 bloqueado na DB tx inteira | **200 imediato + `go func()` com `recover` + `bus.PublishInbound`** | Meta 5s timeout safe |
| **Scheduled posts** | sem coluna, sem query, sem UI | **`scheduled_at` column + `WHERE scheduled_at <= NOW()` + reconciler tick 5s** | SMM feature core |
| **Rate limit** | só `/admin/login` per-IP | **per-`(tenant, channel)` token-bucket in-memory** | tenant fairness |
| **Meta token** | estático (60d long-lived) | **auto-refresh via `fb_exchange_token` + Keyring.SetCredentials** | zero-touch credential rotation |
| **OIDC login** | sem `nonce` validação, descarta refresh | **`nonce` em `OIDCState` + `Config.Nonce` + tabela `oidc_tokens` cifrada** | H2 fix; Fase 5+ features viabilizadas |
| **Webhook secrets** | env vars plaintext | **`Keyring` envelope + `webhook_secrets` table** | env-free em prod |
| **DLQ observability** | `bus.SubscribeDLQ` sem consumer wired | **`DLQTotal` metric + audit row + alerta Prometheus** | operator visibility |
| **Bus partitioning** | 5 channels globais (1024/1024/256/256/64) | **`map[tenantID]chan` ou quota per-tenant com `atomic.Int32`** | tenant noise isolation |
| **Coordinator (multi-replica)** | single-process assumido | **`pg_try_advisory_lock(hashtext(tenantID))` + capability advertisement `[Seção 11]`** | sessão whatsmeow sharded por tenant; zero Redis/NATS |
| **Whatsmeow** | `stubWhatsmeowClient` | **`*whatsmeow.Client` real** com persistência de QR + reconnect | production-ready WAWeb |
| **Health check** | smoke (`Get()`) | **`Sender.Ping(ctx) error` real por canal** | channel-down detection |

### 1.4 Estimativa ajustada (com reuso)

#### Sprints 1–5 (escopo original)

| Categoria | LOC | Dias |
|---|---:|---:|
| **NEW** (whatsmeow real + circuit breaker + scheduled posts + rate limit + Meta refresh + OIDC nonce + webhook secrets + dedup + DLQ consumer + partition bus + health check) | ~3.800 | 9.5 |
| **REWRITE** (claim tx + jitter/backoff + async webhook + bus partition + honest capability) | ~1.200 | 3.0 |
| **ADAPT** (OIDC nonce + webhook secrets → envelope + honest matrix) | ~600 | 1.5 |
| **MECHANICAL** (dedup cache + `goleak` propagação + remover `port.Channel` + async E2E) | ~700 | 1.5 |
| **Tests** (unit + integration + chaos) | ~2.500 | 4.0 |
| **Buffer** (20% para breaker tuning + jitter math + chaos flake) | — | 2.5 |
| **Subtotal Sprints 1–5** | **~8.800** | **22.0** |

#### Sprint 0 (auditoria de segurança — pré-requisito, Seção 12)

| Categoria | LOC | Dias |
|---|---:|---:|
| **CRITICAL** (6 issues, §12.1) | ~1.500 | 3.4 |
| **HIGH** (9 issues + 1 triage, §12.2) | ~1.800 | 4.4 |
| **MEDIUM** (3 issues + 1 triage, §12.3) | ~600 | 1.4 |
| **Housekeeping** (9 issues stale, §12.0) | ~50 | 0.2 |
| **Tests** (regressão IDOR/authz/concorrência) | ~1.200 | 1.0 |
| **Buffer** (15% para races intermitentes + deps) | — | 1.5 |
| **Subtotal Sprint 0** | **~5.150** | **~11.9** |

#### Total Fase 9 (Sprint 0 + Sprints 1–5)

| | LOC | Dias |
|---|---:|---:|
| **Total Fase 9** | **~13.950** | **~33.9d** |

**Interpretação:** **6-7 sprints solo** ou **3-4 sprints com 2 devs** (paralelizando Sprint 0A/0B).

Distribuição em sprints:
- **Sprint 1 — Production-blockers A1+A2+A3+A4** (5 dias): fixes de resiliência e async webhook.
- **Sprint 2 — Scheduled posts A5+#176** (4 dias): schema + reconciler + API.
- **Sprint 3 — Credenciais B2+B3+B4** (4 dias): Meta refresh + OIDC nonce + webhook secrets.
- **Sprint 4 — Resiliência/observability B1+B5+B6+C1+C2+C4** (5 dias): rate limit + dedup + DLQ + cleanup.
- **Sprint 5 — Whatsmeow real + health C3+C5+#158** (4 dias): stub → real + Ping.

---

## 2. Visão geral da Fase 9

Implementa a **maturidade de produção SMM** que o README §23 implicitamente assume pós-1.0 mas que a auditoria arquitetural identificou como **bloqueio para mercado multiredes sociais**:

- **Resiliência "inteligente"**: circuit breaker per `(tenant, channel)` (#160), backoff persistente com jitter (#161), bus particionado por tenant (#171), per-tenant rate limit (#164).
- **Pipeline webhook verdadeiramente assíncrono**: handler retorna 200 antes de ingestar (#162), com dedup cache edge (#168) e reconciler cobrindo o gap.
- **Scheduled posts (SMM feature core)**: coluna `scheduled_at` (#163) + query filtrada + reconciler tick + API REST + `cron` semantics (#176).
- **Credenciais com lifecycle completo**: Meta auto-refresh (#165), OIDC nonce + refresh token persistence (#166), webhook secrets via envelope (#167).
- **Observability de produção**: DLQ consumer + metric + alerta (#169), `goleak` propagado (#173), E2E async (#175).
- **Whatsmeow real** (carryover #158): `*whatsmeow.Client` substitui stub, com persistência de QR, reconnect real, warmup testado em produção.
- **Hygiene**: remover dead code (`port.Channel`, naming `ChannelWAWeb`, capability inflada do whatsmeow) + health check real per-channel.

### A Fase 9 **NÃO** implementa

- **Execução do coordinator multi-replica** (design documentado em **Seção 11**; execução prevista para Fase 10+ como carryover das issues #177–#180).
- Vault Transit sealer (pós-1.0, §2 + §22).
- Webhook fan-out para sistemas externos (sem `adapter/sink/` no 1.0).
- UI/UX do painel de scheduled posts (apenas API; o painel vem em sprint pós-Fase 9).
- OpenSearch / analytics (descartado em §2 do `docs/plan.md`).
- Cache externo (Redis continua fora do 1.0; in-memory é mandatório).
- Message broker externo (NATS continua fora do 1.0; bus in-process é mandatório — alinhado com `AGENTS.md §1.1`).

> **Carryover carryover:** a issue #158 (whatsmeow real → `*whatsmeow.Client`) **é** executada na Fase 9 (Sprint 5); as issues #177–#180 (coordinator multi-replica) **não** — ficam como carryover de design para Fase 10+. A Seção 11 deste plano documenta a **estratégia completa** (incluindo tabelas comparativas, ADRs e diagramas) para que a Fase 10+ possa começar com DoD pré-aprovado.

> ⚠️ **Sprint 0 (Seção 12) é pré-requisito dos Sprints 1–5.** 6 CRITICAL + 9 HIGH + 3 MEDIUM da auditoria de segurança Fase 8 (`docs/fase8/FIXES/003_SECURITY_AUDIT.md`) ainda estão abertas; várias (#132 admin auth, #133 IDOR RunInTenantTx, #140 OIDC nonce, #151 TLS) afetam os handlers/features novos desta Fase 9. Detalhes em **Seção 12**.

---

## 3. Issues detalhadas

> **Sprint 0 (Seção 12, pré-requisito) — 6 CRITICAL + 9 HIGH + 3 MEDIUM (carryover Fase 8). Detalhes na Seção 12.**
>
> **Sprints 1–5 (originais da Fase 9) — issues #158-#176. Detalhes abaixo.**

### Sprint 1 — Production-blockers (5 dias)

#### #159 — A1: `OutboxRepo.ClaimNext` em `BeginTx` (H9 fix)

**Arquivos:** `internal/adapter/repository/postgres/outbox.go:131-187, 284-290`

**Diagnóstico:** `platformPool.Query(... FOR UPDATE SKIP LOCKED ...)` é **single statement**; locks liberados ao fim do statement. `AcquireClaimLock` (exportado) é dead code. Documentado em `docs/fase8/FIXES/003_SECURITY_AUDIT.md:181-187` (H9) e `002_ARCHITECTURE_REVIEW.md:13-17` (item 1.1).

**Ação:**
1. Modificar `ClaimNext` para abrir `r.platformPool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})` antes do `SELECT ... FOR UPDATE SKIP LOCKED`.
2. Consumir todos os rows na mesma tx, retornar slice de `domain.Message` + tx.
3. Chamar `tx.Commit()` após a slice ser construída.
4. Adicionar `tx.Rollback()` em qualquer path de erro.
5. O `process(ctx, m)` no relay continua usando `r.appPool` (não-platform) para a chamada ao sender — **não muda contrato**.
6. Deletar `AcquireClaimLock` (dead code) ou marcar `//nolint:unused` se outra feature quiser reusar.

**Critério de aceitação:**
- `go test -race -tags=integration -run TestOutboxClaim_Concurrent ./...` verde (2 goroutines claiming same batch → 1 ganha, 1 espera).
- Audit row por claim continua único (não duplica).
- Rollback em erro não deixa rows órfãs em `Claimed`.

**Esforço:** 0.5d · **Tipo:** REWRITE · **Bloqueado por:** —

---

#### #160 — A2: Circuit breaker per `(tenant, channel)` em volta de `Sender.Send`

**Arquivos:** novo `internal/adapter/breaker/breaker.go`; modificar `internal/usecase/outbox/relay.go:140-145`

**Ação:**
1. Adicionar `github.com/sony/gobreaker/v2 v2.0.0` ao `go.mod` (allowed — in-memory pure Go).
2. Criar `breaker.Registry` com `map[cacheKey]*gobreaker.CircuitBreaker` (key = `(tenantID, channel)`), thread-safe.
3. Config por canal via `MEZ_BREAKER_<CHANNEL>_{MAX_REQUESTS,INTERVAL,TIMEOUT,FAIL_THRESHOLD}`; default `MaxRequests=3, Interval=30s, Timeout=60s, ReadyToTrip=ConsecutiveFailures >= 5`.
4. No `relay.go:140-145`, antes de `r.process(...)`, obter breaker do registry e chamar `breaker.Execute(func() error { return r.process(ctx, m) })`.
5. Se breaker aberto (`ErrOpenState` ou `ErrTooManyRequests`), marcar `MarkFailed` com erro "circuit-open" + reagendar via `next_attempt_at` (depende de #161).
6. Métricas: `breaker_state_change_total{channel,from,to}` Counter, `breaker_open_seconds{channel}` Gauge.
7. Audit row por state change: `breaker.open` / `breaker.close`.

**Critério de aceitação:**
- Test: 6 falhas consecutivas → breaker abre; 3 success após `Timeout` → half-open; 1 success → closed.
- Test: breaker per `(tenant, channel)` é isolado — tenant A com breaker aberto não afeta tenant B.
- Chaos test (Sprint 5 #106 carryover): Meta outage sustentado 30s → relay não bate rede após 5 falhas.

**Esforço:** 1.0d · **Tipo:** NEW · **Bloqueado por:** —

---

#### #161 — A3: Jitter + backoff persistente em outbox retry

**Arquivos:** `migrations/0007_outbox_next_attempt.up.sql` (NOVO); `internal/core/domain/outbox.go:18-31, 114-131`; `internal/usecase/outbox/relay.go:154-199`; `internal/adapter/repository/postgres/outbox.go:131-247`

**Diagnóstico:** `OutboxMessage.NextAttemptAt` é declarado mas não persistido. `MarkFailed` seta em memória e nada mais. `relay.go:154-199` chama `process` no mesmo tick.

**Ação:**
1. Migration `0007`: `ALTER TABLE outbound_events ADD COLUMN next_attempt_at TIMESTAMPTZ; CREATE INDEX idx_outbound_events_pending_due ON outbound_events (next_attempt_at NULLS FIRST, created_at) WHERE status='pending';`
2. `domain.OutboxMessage`: ler/escrever `NextAttemptAt` via `Scan`/`Value` (pgx já cobre `time.Time`).
3. `MarkFailed`: calcular `now + min(60s * 2^attempts, 30min) + random(0, 30s) jitter` e persistir via `UPDATE outbound_events SET attempts=attempts+1, next_attempt_at=$2, last_error=$3`.
4. `relay.go:ClaimNext`: substituir query por `WHERE status='pending' AND (next_attempt_at IS NULL OR next_attempt_at <= NOW()) ORDER BY COALESCE(next_attempt_at, created_at) LIMIT $1 FOR UPDATE SKIP LOCKED`. (Coalesce garante que `MarkSent` não regresse.)
5. Métrica: `outbox_retry_delay_seconds{channel}` Histogram.
6. Audit: `outbox.retry.scheduled{channel, attempt, delay_ms}`.

**Critério de aceitação:**
- Test: 3 retries consecutivos com `attempts=1,2,3` → `next_attempt_at` ≈ `60s, 120s, 240s` ± 30s.
- Test: jitter range é `[0, 30s]` (não pode ser negativo, não pode exceder 30s).
- Test: query com `next_attempt_at > NOW()` retorna 0 rows; após `advance time 60s`, retorna 1 row.
- Integration test (`//go:build integration`): inserir 100 rows com `next_attempt_at` randomizado; 2 relays concorrentes não pegam mesma row (H9 + #159 composto).

**Esforço:** 1.0d · **Tipo:** REWRITE · **Bloqueado por:** #159

---

#### #162 — A4: Webhook handler async (200 antes de ingestar)

**Arquivos:** `internal/adapter/webhook/meta/handler.go:166-176`; `internal/adapter/webhook/telegram/handler.go:118-124`; `internal/usecase/messaging/ingest.go:91-181`

**Diagnóstico:** `meta/handler.go:169` é explícito: "Para Fase 2, retornamos 200 e logamos — Meta não retenta após 200." Isso significa **erros transientes viram log eterno** e **Meta 5s timeout derruba integração**.

**Ação:**
1. `meta/handler.go`: após `validMetaSignature(...)` e `payload.ToInboundEvent(...)`, fazer `go func() { defer recover(); ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second); defer cancel(); if _, err := h.ingestor.Ingest(ctx, evt); err != nil { h.log.Error()... } }()`. **Usar `context.Background()` (não `r.Context()`)** — client pode desconectar antes da tx terminar.
2. `telegram/handler.go`: idem para `h.ingestor.Ingest(ctx, evt)`.
3. `ingest.go`: o body do `RunInTenantTx` continua o mesmo — única mudança é **não bloquear o response writer** esperando ele.
4. Métrica: `webhook_handler_duration_seconds{network,status}` Histogram (status = `async_ok`, `async_failed`, `sync`).
5. Audit: `webhook.async.failed` (apenas quando `Ingest` retornar erro — operator visibility).
6. Garantir que o Reconciler C1 (Fase 2) cobre o gap se a goroutine morrer — já cobre, mas adicionar teste explícito.

**Critério de aceitação:**
- Test (Fase 9 #175): `t_200_returned - t_request_arrived < 50ms` enquanto a ingest tx demora 500ms (injetar latência com `pg_sleep` ou `RunInTenantTx` mock).
- Test: panic em `Ingest` é recuperado pela goroutine; process não morre.
- Test: `recover()` em ambos handlers ativos.
- Audit row só em falha (não em sucesso — evita log noise).

**Esforço:** 1.0d · **Tipo:** REWRITE · **Bloqueado por:** —

---

### Sprint 2 — Scheduled posts (4 dias)

#### #163 — A5: Scheduled posts — schema + query + reconciler

**Arquivos:** `migrations/0008_outbox_scheduled.up.sql` (NOVO); `internal/core/domain/outbox.go`; `internal/adapter/repository/postgres/outbox.go`; `internal/usecase/outbox/relay.go`; `internal/usecase/messaging/send.go`; `api/openapi.yaml:396-417`; `api/openapi.gen.go`; `internal/transport/http/api/handlers.go`

**Ação:**
1. Migration `0008`: `ALTER TABLE outbound_events ADD COLUMN scheduled_at TIMESTAMPTZ; CREATE INDEX idx_outbound_events_scheduled ON outbound_events (scheduled_at) WHERE status='pending' AND scheduled_at IS NOT NULL;`
2. `domain.OutboxMessage`: campo `ScheduledAt *time.Time` (nullable).
3. `usecase/messaging/send.go`: aceitar `ScheduledAt *time.Time` em `OutboundRequest` (ou novo campo). Se setado, enfileira com `scheduled_at` no INSERT.
4. `relay.go:ClaimNext`: query atualizada para `WHERE status='pending' AND (scheduled_at IS NULL OR scheduled_at <= NOW()) AND (next_attempt_at IS NULL OR next_attempt_at <= NOW()) ORDER BY COALESCE(scheduled_at, COALESCE(next_attempt_at, created_at))`.
5. `relay.go:Run`: tick `1s` adicional (separado do `PollInterval=5s`) para varrer rows com `scheduled_at <= NOW()`. Ou unificar com tick existente.
6. OpenAPI: adicionar `scheduled_at` opcional em `OutboundMessage`. `oapi-codegen -generate types,chi-server,spec > api/openapi.gen.go`.
7. Handler: aceitar `scheduled_at` no body JSON; validar `scheduled_at > NOW() + 60s` (anti-flood).
8. Test: enfileirar msg com `scheduled_at = NOW() + 2s`; relay não pega em T+0s; pega em T+2.5s.
9. Test: enfileirar 10 msgs com `scheduled_at` randomizado em T+0..10s; relay drena em ordem cronológica.

**Critério de aceitação:**
- `OutboxMessage` tem `ScheduledAt` em INSERT e SELECT.
- OpenAPI regenerado, `make openapi-gen && git diff --exit-code api/openapi.gen.go` verde.
- Handler rejeita `scheduled_at <= NOW() + 60s` com 400.
- `ClaimNext` query é `< 5ms` com 1M rows (índice parcial garante).

**Esforço:** 2.0d · **Tipo:** NEW · **Bloqueado por:** #161 (next_attempt_at part of same query)

---

#### #176 — A5.1: API de agendamento + UI helper

**Arquivos:** `internal/transport/http/api/handlers.go`; `internal/transport/http/api/handlers_scheduled.go` (NOVO); `internal/transport/adminweb/templates/scheduled.html` (placeholder)

**Ação:**
1. Novo endpoint `POST /api/messages/scheduled` com body `OutboundMessage` (mesma estrutura, com `scheduled_at` obrigatório).
2. Novo endpoint `GET /api/messages/scheduled?from=&to=` (lista agendadas no intervalo).
3. Novo endpoint `DELETE /api/messages/scheduled/{id}` (cancela agendada; só permitido se `status='pending'`).
4. Audit: `scheduled.create`, `scheduled.cancel`.
5. UI placeholder em `adminweb/templates/scheduled.html` (form simples, lista table) — sem JS framework.
6. Métrica: `scheduled_total{tenant}` Gauge, `scheduled_fired_total{channel}` Counter.
7. Wire-up no `cmd/server/wire.go`.

**Critério de aceitação:**
- E2E: cria agendamento + reconciler drena + msg entregue no canal (via `SenderRecorder` em `tests/e2e/harness_test.go:31`).
- Delete antes do `scheduled_at`: row vai para `status='cancelled'` (novo status; migration `0008` adiciona ao enum).
- Audit: 3 rows (create, fire, cancel opcional).

**Esforço:** 2.0d · **Tipo:** NEW · **Bloqueado por:** #163

---

### Sprint 3 — Credenciais com lifecycle (4 dias)

#### #165 — B2: Meta token refresh automático

**Arquivos:** `internal/adapter/provider/waba/client.go:25, 163-194`; `internal/usecase/secrets/keyring.go:121`; `internal/adapter/provider/meta/oauth.go` (NOVO)

**Ação:**
1. Criar `internal/adapter/provider/meta/oauth.go` com `RefreshLongLivedToken(ctx, currentToken, appID, appSecret) (newToken, expiresIn, error)`. Implementa `POST /oauth/access_token?grant_type=fb_exchange_token&client_id=...&client_secret=...&fb_exchange_token=...`.
2. `waba/client.go`: adicionar `oauthClient *meta.OAuthClient` e `appID, appSecret string` (via config ou via `Keyring`).
3. Wrap `c.doJSON` com `c.refreshIfNeeded(ctx)`: se response for 401 com `code=190`, refresh + retry uma vez. Se refresh falhar, propagar erro original.
4. Após refresh bem-sucedido, chamar `keyring.SetCredentials(ctx, tenantID, ChannelWABA, newCredentialsJSON)`. (Adicionar `tenantID` ao contexto do WABA client — pode ser via wrapper no registry.)
5. Audit: `meta.token.refreshed{tenant_id, channel}`.
6. Métrica: `meta_token_refresh_total{channel, result}` Counter.
7. Test: mock HTTP server que retorna 401+190; refresh endpoint retorna novo token; 2ª chamada do `doJSON` usa token novo.

**Critério de aceitação:**
- `waba.Client.RefreshIfNeeded` é idempotente (chamar 2x com sucesso não faz 2 refreshes).
- Refresh + retry acontece em ≤ 1.5s (latency budget).
- `Keyring.SetCredentials` cifra o novo token com envelope chain.
- Audit row por refresh com `actor = "system:meta-refresh"`, `metadata = {old_expires_in, new_expires_in}`.

**Esforço:** 1.5d · **Tipo:** NEW · **Bloqueado por:** —

---

#### #166 — B3: OIDC `nonce` + refresh token persistence (H2 audit)

**Arquivos:** `internal/adapter/idp/oidc/oidc.go:47, 65-79`; `internal/usecase/auth/login.go:160-189`; `internal/adapter/idp/oidc/verifier.go:28-30`; `migrations/0009_oidc_tokens.up.sql` (NOVO); `internal/adapter/repository/postgres/oidc_tokens_repo.go` (NOVO)

**Ação:**
1. `oidc.go:47`: alterar `provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})` para `&gooidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{"RS256"}}`. (Mantém PKCE; **adiciona `Nonce` se já tiver state.Nonce** — Fase 9 não tem OIDC production login ainda, então fazer a parte de `Nonce` em `login.go`.)
2. `login.go:160-189`: gerar `nonce := base64.RawURLEncoding.EncodeToString(randBytes(16))`, persistir no `OIDCState` (já tem state — adicionar campo `Nonce`), incluir no `AuthCodeURL` via `oauth2.SetAuthURLParam("nonce", nonce)`.
3. `login.go: callback`: extrair `nonce` do state, passar para `Verifier.Verify(ctx, rawIDToken, gooidc.VerifyNonce(state.Nonce))`.
4. Migration `0009`: `CREATE TABLE oidc_tokens (tenant_id UUID PRIMARY KEY REFERENCES tenants(id), refresh_token_encrypted BYTEA NOT NULL, id_token_claims JSONB NOT NULL, expires_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now());` com RLS FORCE.
5. `oidc_tokens_repo.go`: `SaveRefreshToken(ctx, tenantID, encryptedRefresh, claims, expires)` usando `port.Encryptor` (envelope).
6. `login.go` callback: após `Exchange`, extrair `token.RefreshToken` + `token.Expiry`, cifrar, persistir.
7. Audit: `oidc.nonce.validated`, `oidc.refresh_token.persisted`.
8. Test: state com `nonce` X mas ID-token com `nonce` Y → rejeita (H2 fix).
9. Test: refresh token é cifrado com KEK da `LocalSealer`.

**Critério de aceitação:**
- `verify.go` rejeita ID-token sem `nonce` matching state.
- Refresh token cifrado em DB; `oidc_tokens_repo.Load` retorna decifrado.
- Migration aplicável via `make migrate-up`.
- Tests cobrem rejeição (H2 cenário) e cifragem (C9 invariante).

**Esforço:** 1.5d · **Tipo:** ADAPT · **Bloqueado por:** —

---

#### #167 — B4: Webhook secrets via envelope (Meta app, Telegram bot)

**Arquivos:** `internal/adapter/webhook/secrets/credentials.go:73-92`; `internal/adapter/webhook/secrets/resolvers.go:46-113`; `migrations/0009_webhook_secrets.up.sql` (NOVO — combinar com #166); `internal/adapter/repository/postgres/webhook_secrets_repo.go` (NOVO)

**Diagnóstico:** `MEZ_WABA_CREDENTIALS`, `MEZ_META_APP_SECRETS`, `MEZ_TELEGRAM_SECRETS` em plaintext no process env.

**Ação:**
1. Migration `0009` (combinar com #166): `CREATE TABLE webhook_secrets (id UUID PRIMARY KEY, tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE, channel TEXT NOT NULL, app_id TEXT NOT NULL, secret_encrypted BYTEA NOT NULL, kek_version INT NOT NULL DEFAULT 1, created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE(tenant_id, channel, app_id));` com RLS FORCE.
2. `webhook_secrets_repo.go`: `Save(ctx, tenantID, channel, appID, secret)` e `Load(ctx, tenantID, channel, appID)` usando `port.Encryptor`.
3. Modificar `webhook_secrets/resolvers.go`: criar `KeyringMetaSecrets` que delega ao `webhook_secrets_repo` (com fallback env-only em dev — flag `MEZ_SECRETS_KEYRING=true`).
4. Wire-up: `cmd/server/wire.go` instancia `KeyringMetaSecrets` se `Keyring` disponível + `MEZ_SECRETS_KEYRING=true`.
5. Audit: `webhook_secret.stored`, `webhook_secret.loaded`.
6. Test: secret cifrado em DB; `KeyringMetaSecrets.ResolveMetaSecret(...)` retorna decifrado.
7. **Backward-compat:** env-only continua funcionando para Fase 9 (devs locais); prod recomenda `MEZ_SECRETS_KEYRING=true`.

**Critério de aceitação:**
- Webhook handler usa `KeyringMetaSecrets` quando configurado; cai para `EnvMetaSecrets` quando não.
- Audit row por load (não por chamada — cachear em memória com TTL 5min igual ao DEK cache).
- `cmd/server rotate-kek` re-cifra `webhook_secrets` automaticamente (adicionar ao `secrets/rotate_kek.go:189-260` — `ForEachTenant` itera também `webhook_secrets`).

**Esforço:** 1.0d · **Tipo:** ADAPT · **Bloqueado por:** —

---

### Sprint 4 — Resiliência + observability (5 dias)

#### #164 — B1: Per-tenant + per-channel rate limit

**Arquivos:** novo `internal/adapter/cache/memory/ratelimit.go`; `internal/usecase/outbox/relay.go:140-145`; `internal/adapter/provider/whatsmeow/reconnect.go:91-96` (reuso da warmup)

**Ação:**
1. Criar `internal/adapter/cache/memory/ratelimit.go` com `Limiter` (token-bucket) por `(tenant, channel)`. Config via `MEZ_RATE_LIMIT_<CHANNEL>_{RATE,BURST}`; default baseado nas quotas reais:
   - `WABA`: 80 msgs/s (tier 1) / 400 (tier 2) / 1000 (tier 3). Configurável por tenant.
   - `IG`: 200 calls/h por `page_id` (Meta). Sem rate limit por tenant; rate limit compartilhado por `app_id`.
   - `MSG`: 200 calls/h por `page_id` (similar).
   - `TGBot`: 30 msgs/s global por bot. Rate limit por tenant = 30/N onde N = nº de tenants.
   - `WAWeb`: reusa `whatsmeow.Warmup` (já existe) — quota diária 10→200 ramp.
2. `Limiter.Allow(tenantID, channel) bool` — `false` quando bucket vazio.
3. No `relay.go:140-145` (depois de breaker #160), chamar `ratelimit.Allow(...)`. Se `false`, `MarkFailed` com erro "rate-limited" + reagendar via `next_attempt_at` (já tem #161).
4. Métrica: `ratelimit_drop_total{channel}` Counter.
5. Audit: `ratelimit.drop{tenant_id, channel}` (com sampling 1% para evitar log flood).
6. Test: 2 tenants no mesmo canal não afetam um ao outro (buckets isolados).
7. Test: rate=WABA tier 1 = 80/s, burst=10; enviar 100 msgs em 1s → 80 aceitas, 20 rate-limitadas.

**Critério de aceitação:**
- Rate limit é opt-in por canal (env var setada); default desabilitado para não afetar dev.
- Bucket é thread-safe (`sync.Mutex`).
- Limpar bucket em `Manager.DisconnectAll` (Fase 8 #97) — tenant que desconecta não tem mais quota alocada.

**Esforço:** 1.5d · **Tipo:** NEW · **Bloqueado por:** #160, #161

---

#### #168 — B5: In-memory dedup cache no edge dos webhooks

**Arquivos:** `internal/adapter/webhook/meta/handler.go` (após sig validate); `internal/adapter/webhook/telegram/handler.go` (após secret compare)

**Diagnóstico:** Meta reentrega mesma `mid` em <1s; `ON CONFLICT` no DB já existe, mas AR/upsert acontecem 2x.

**Ação:**
1. Adicionar `dedupCache sync.Map` no `Handler` struct (key = `provider_msg_id`, value = `time.Time`).
2. Após `validMetaSignature(...)` e `payload.ToInboundEvent(...)`, antes de `go func() { Ingest(...) }()` (#162), `if _, ok := h.dedupCache.LoadOrStore(evt.MessageID, time.Now().Add(5*time.Minute)); ok { return 200 }`.
3. Idem para Telegram (key = `update_id`).
4. Sweeper goroutine (start com o handler): `for { time.Sleep(1*time.Minute); h.dedupCache.Range(func(k, v) { if time.Now().After(v.(time.Time)) { h.dedupCache.Delete(k) } }) }`.
5. Configurável: `MEZ_WEBHOOK_DEDUP_TTL` (default 5min); `MEZ_WEBHOOK_DEDUP_MAX_ENTRIES` (default 100k, LRU se exceder).
6. Métrica: `webhook_dedup_hit_total{network}` Counter.
7. Test: 2 webhooks com mesma `mid` em 100ms → 2ª é deduped (200 retornado, sem ingest).
8. Test: TTL expirado → dedup cache libera, novo webhook passa.

**Critério de aceitação:**
- 100% dos webhooks duplicados detectados antes de bater DB.
- Memória do cache bounded por `MAX_ENTRIES` (LRU).
- Sweeper não vaza goroutine (test com `goleak.VerifyTestMain` em #173).

**Esforço:** 1.0d · **Tipo:** NEW · **Bloqueado por:** #162

---

#### #169 — B6: DLQ consumer default + métrica + alerta

**Arquivos:** `cmd/server/wire.go:162-170`; novo `internal/usecase/dlq/consumer.go`; `pkg/metrics/metrics.go`; `deployments/prometheus/alerts.yaml`

**Ação:**
1. `pkg/metrics/metrics.go`: adicionar `DLQTotal *prometheus.CounterVec` com labels `{channel, reason}`.
2. `internal/usecase/dlq/consumer.go`: `Consumer` struct que subscreve `bus.SubscribeDLQ` e em cada evento: `metrics.DLQTotal.WithLabelValues(evt.Channel, reason).Inc(); auditRepo.Record(ctx, &admin.AuditEntry{Action: "dlq.event", Metadata: {channel, msg_id, error}})`. `reason` derivado do erro (regex match para "circuit-open", "rate-limited", "meta-401", default "send-failed").
3. `cmd/server/wire.go`: wire-up `dlqConsumer := dlq.New(bus, metrics, auditRepo); bus.SubscribeDLQ(dlqConsumer.Handle)`.
4. `deployments/prometheus/alerts.yaml`: `alert: DLQSpike` com `expr: rate(DLQTotal[5m]) > 10` por 5min, `severity: warning`, `annotations: { summary: "DLQ accumulating", runbook_url: "..." }`.
5. `pkg/lifecycle.Runner`: adicionar `PhaseDLQConsumer` na ordem de boot (depois de `PhaseBus`, antes de `PhaseRelay`).
6. Test: relay processa msg com 5 falhas → DLQ → consumer.Handle é chamado com `evt` correto → métrica incrementa.
7. Test: razão é extraída corretamente ("circuit-open" → `reason=circuit_open`).

**Critério de aceitação:**
- Toda `bus.PublishDLQ` é consumida (sem drops por `DLQBuffer=256` cheio — auditoria em log warning + métrica `DLQDroppedTotal`).
- Alerta Prometheus em `deployments/prometheus/alerts.yaml` é válido (test com `promtool check alerts`).
- Audit row por evento (com `actor = "system:dlq"`).

**Esforço:** 1.0d · **Tipo:** NEW · **Bloqueado por:** #161

---

#### #170 — C1: Remover `port.Channel` órfão + reconciliar naming

**Arquivos:** `internal/core/port/channel.go:10-16`; `internal/core/domain/types.go:16`; `internal/core/event/event.go:75`; `internal/adapter/provider/whatsmeow/events.go:101`

**Diagnóstico:** `port.Channel` é dead code (nenhum adapter implementa). `domain.ChannelWAWeb = "whatsmeow"` vs `event.ChannelWAWeb = "whatsapp_web"` — foot-gun.

**Ação:**
1. Deletar `port.Channel` interface inteira (`channel.go:10-16`); manter só `InboundSink` e `OutboundPublisher` (que são usados pelo bus).
2. Mover `Channel` enum canônico para `internal/core/domain` (já está lá); re-exportar de `internal/core/event` via `type Channel = domain.Channel` (Go 1.22 type alias).
3. Padronizar string: `domain.ChannelWAWeb = "whatsapp_web"` (era `"whatsmeow"`). Atualizar todos os refs.
4. Adicionar `domain.ChannelWhatsmeow = domain.ChannelWAWeb` como alias deprecated (`// Deprecated: use ChannelWAWeb`).
5. Atualizar `whatsmeow/events.go:101` para usar a constante nova.
6. Audit/grep: zero ocorrências de `"whatsmeow"` como string de canal fora de `whatsmeow/`.
7. Test: `event.Channel(domain.ChannelWAWeb) == "whatsapp_web"`.

**Critério de aceitação:**
- `go build ./...` verde.
- `go vet ./...` verde.
- `grep -rn "\"whatsmeow\"" --include='*.go' .` retorna 0 hits fora de `internal/adapter/provider/whatsmeow/`.
- OpenAPI regenerado sem diff (não muda wire).

**Esforço:** 0.5d · **Tipo:** MECHANICAL · **Bloqueado por:** —

---

#### #171 — C2: Bus partition per-tenant (ou quota per-tenant)

**Arquivos:** `internal/adapter/broker/bus.go:13-22, 43-58, 84-95`

**Ação:**
1. Avaliar duas abordagens:
   - **A)** `map[tenantID]chan event.InboundEvent` com merge em `PublishInbound` (mais complexo, mais memória).
   - **B)** Manter bus global mas adicionar quota per-tenant: `tenantQuota map[tenantID]chan struct{}` com `cap=100`, `select { case quota <- struct{}{}: ... default: drop }`. (Mais simples, O(1) memória por tenant.)
2. Default: abordagem B. Justificativa: simplicidade, O(1) por tenant ativo, easy rollback.
3. Configurável: `MEZ_BUS_TENANT_QUOTA` (default 100 msgs simultâneas in-flight por tenant).
4. Métrica: `bus_tenant_quota_exceeded_total{tenant_id_hash}` Counter (com hash de tenant_id para não vazar ID em label).
5. Test: tenant A enche quota (100 in-flight lentas) → 101ª é dropada; tenant B não afetado.
6. Test: reconciler cobre o drop (Fase 2 já cobre — adicionar assert explícito).

**Critério de aceitação:**
- Tenant barulhento não afeta outros tenants.
- Memória do `tenantQuota` é bounded por `MEZ_MAX_ACTIVE_TENANTS` × `cap`.
- Audit + métrica no drop (operator visibility).

**Esforço:** 1.0d · **Tipo:** REWRITE · **Bloqueado por:** —

---

#### #173 — C4: `goleak.VerifyTestMain` em 4 pacotes

**Arquivos:** `internal/adapter/webhook/{meta,telegram}/*_test.go`; `internal/adapter/broker/bus_test.go`; `internal/usecase/outbox/*_test.go`; `internal/usecase/reconcile/*_test.go`

**Ação:**
1. Adicionar `func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }` em cada um dos 4 pacotes.
2. Adicionar `go.uber.org/goleak` ao `go.mod` (já é dep indirect de `whatsmeow` via pai — verificar se é direto, se não, adicionar).
3. **Não** adicionar a testes E2E em `tests/e2e/` (já compartilham o `main` do package).
4. Verificar que todos os testes existentes fecham goroutines — pode exigir cleanup explícito em `Relay.Stop()`, `Bus.Drain()`, etc.
5. CI: `go test -race -shuffle=on ./...` deve passar sem leak warnings.

**Critério de aceitação:**
- `goleak.VerifyTestMain` em 4 novos pacotes + 7 da Fase 8 = 11 pacotes total.
- CI verde.

**Esforço:** 0.5d · **Tipo:** MECHANICAL · **Bloqueado por:** —

---

### Sprint 5 — Whatsmeow real + health check (4 dias)

#### #158 — Substituir `stubWhatsmeowClient` por `*whatsmeow.Client` real

**Arquivos:** `internal/adapter/provider/whatsmeow/stub_client.go`; `internal/adapter/provider/whatsmeow/manager.go:113-115`; `internal/adapter/provider/whatsmeow/identity.go`; `internal/adapter/storage/sqlite/identity_store.go` (NOVO)

**Ação:**
1. Criar `internal/adapter/storage/sqlite/identity_store.go` com `IdentityStore` interface (Save/Load/Delete de `*proto.Identity` por `(tenantID, jid)`). Implementação SQLite (file por tenant em `MEZ_DATA_DIR/tenants/<tenantID>/whatsmeow.db`).
2. `whatsmeow.NewClient(...)` recebe `client.NewClient(deviceStore, logger)`.
3. `Manager.GetOrCreate`: ao criar, tenta `Load` do `IdentityStore`; se não existe, gera QR + espera pareamento; persiste em `Save`.
4. `Manager.GetOrCreate`: ao reconectar, usa `Load` + `client.Connect()`; se `IsConnected` falhar, persiste erro no audit.
5. QR endpoint: `GET /api/admin/tenants/{id}/whatsmeow/qr` retorna PNG (já tem em `pkg/qrcode`).
6. Adicionar `MEZ_DATA_DIR` env var (default `/var/lib/mez-go-mono`).
7. Audit: `whatsmeow.paired`, `whatsmeow.unpaired`, `whatsmeow.reconnected`.
8. Test: criar client com IdentityStore in-memory (testutil); parear mock → reconnect → estado preservado.
9. Test (integration): gerar QR, scanear com test client, validar mensagem enviada/recebida.

**Critério de aceitação:**
- `*whatsmeow.Client` real substitui stub.
- IdentityStore persiste entre reboots (SQLite file).
- QR endpoint retorna PNG válido.
- Audit row por pareamento/despareamento.
- `go test -race ./internal/adapter/provider/whatsmeow/...` verde.

**Esforço:** 2.0d · **Tipo:** NEW · **Bloqueado por:** —

---

#### #172 — C3: Capability matrix whatsmeow — implementar ou remover

**Arquivos:** `internal/adapter/provider/whatsmeow/adapter.go:60-62, 124`; `internal/adapter/provider/whatsmeow/capabilities.go:10-25`

**Diagnóstico:** whatsmeow declara 12 capabilities mas `doAction` retorna `ErrNotImplemented` para muitas. Matriz inflada = consumers negociam capability que nunca funciona.

**Ação:**
1. Auditar cada capability em `whatsmeow/capabilities.go:10-25` contra `doAction` em `adapter.go`.
2. Para capabilities **implementadas** (verificar chamadas reais em `doAction`): manter.
3. Para capabilities **claramente implementáveis** com whatsmeow API (ex: `CapBlocklist` → `client.SetPrivacy(blockList)`): implementar, com teste.
4. Para capabilities **fora do escopo** (ex: `CapCalls`, `CapDisappearing`): remover da matriz.
5. Meta-capability: se uma capability é `true` na matriz mas `ErrNotImplemented` em `doAction`, é bug. Não pode acontecer.
6. Audit: `whatsmeow.capability.honest{capability, status}`.
7. Test: para cada capability `true`, existe um teste de `Send` retornando sucesso.

**Critério de aceitação:**
- `Capabilities()` retorna apenas capabilities realmente suportadas.
- Cada capability tem teste de fumaça (smoke test).
- `ErrNotImplemented` em `doAction` reduzido a ≤ 2 capabilities (as que dependem de features que whatsmeow não tem).

**Esforço:** 1.0d · **Tipo:** ADAPT · **Bloqueado por:** —

---

#### #174 — C5: Health check per-channel real (`Sender.Ping`)

**Arquivos:** `internal/core/port/sender.go:96-106`; `internal/adapter/provider/waba/waba.go`; `internal/adapter/provider/instagram/instagram.go`; `internal/adapter/provider/messenger/messenger.go`; `internal/adapter/provider/telegram_bot/telegram.go`; `internal/adapter/provider/whatsmeow/adapter.go`; `internal/adapter/sender/memory/registry.go:137-150`

**Ação:**
1. Adicionar `Ping(ctx context.Context) error` à interface `port.Sender`.
2. Implementar por adapter:
   - **WABA:** `GET /<phone_number_id>` (testa token).
   - **IG:** `GET /me?fields=id` (testa page token).
   - **MSG:** `GET /me?fields=id` (similar).
   - **TGBot:** `getMe` (testa bot token).
   - **WAWeb:** `IsConnected()` (já tem `connected atomic.Bool`).
3. `Registry.Health()` (já existe em `registry.go:137-150`): substituir `Get()` por `Ping()` com timeout 2s.
4. Métrica: `sender_health_check_total{channel, result}` Counter.
5. Audit: `sender.health.failed{channel}` (apenas em falha).
6. Endpoint admin: `GET /api/admin/health/senders` retorna mapa de canais com `ok | error`.
7. Test: mock HTTP server retorna 200 → `Ping` retorna `nil`; retorna 401 → `Ping` retorna erro wrappado.

**Critério de aceitação:**
- `Sender.Ping(ctx)` implementado em todos 5 adapters.
- `Registry.Health()` é bounded por `2s * 5 canais = 10s` no pior caso (mas roda em paralelo se possível).
- Admin endpoint retorna JSON `{ "waba": { "ok": true }, "whatsmeow": { "ok": false, "error": "..." } }`.

**Esforço:** 1.0d · **Tipo:** NEW · **Bloqueado por:** —

---

#### #175 — C6: Testes E2E validando async webhook

**Arquivos:** `tests/e2e/webhook_e2e_test.go`; `tests/e2e/async_e2e_test.go` (NOVO)

**Ação:**
1. Novo `tests/e2e/async_e2e_test.go` com 3 testes:
   - **TestAsyncWebhook_Fast200**: mocka Ingestor com `time.Sleep(500ms)`; mede `t_200_returned - t_request_arrived < 50ms`. Valida #162.
   - **TestAsyncWebhook_PanicRecovery**: Ingestor panica; goroutine recupera; processo sobrevive (não assert via `goleak`; assert que 2º webhook é processado).
   - **TestAsyncWebhook_DedupEdge**: 2 webhooks com mesma `mid` em 100ms; 2º é deduped (200 retornado, mas só 1 mensagem no DB). Valida #168.
2. Adicionar 2 testes em `tests/e2e/pipeline_e2e_test.go`:
   - **TestScheduledPipeline_Fires**: enfileira msg com `scheduled_at = NOW() + 2s`; sender recorder recebe em T+2..3s. Valida #163.
   - **TestScheduledPipeline_Cancelled**: enfileira + cancela antes do `scheduled_at`; sender recorder vazio. Valida #176.
3. Wire-up: usar `tests/e2e/harness_test.go:31` (já tem `SenderRecorder`).
4. Tag `//go:build e2e` (separado de `integration`).

**Critério de aceitação:**
- 5 novos testes E2E verdes.
- CI `go test -tags=e2e -race ./tests/e2e/...` verde.
- `tests/e2e/async_e2e_test.go` tem `TestMain` com `goleak.VerifyTestMain`.

**Esforço:** 1.0d · **Tipo:** NEW · **Bloqueado por:** #162, #163, #168

---

## 4. Definition of Done (DoD da Fase 9)

### Funcional

- [ ] **A1 (#159)**: `OutboxRepo.ClaimNext` em `BeginTx`; `AcquireClaimLock` deletado ou em uso.
- [ ] **A2 (#160)**: Circuit breaker per `(tenant, channel)` integrado no relay; metric `breaker_state_change_total`; audit row por state change.
- [ ] **A3 (#161)**: `next_attempt_at` persistido; jitter `±30s`; backoff `60s * 2^n` capped 30min; query filtrada.
- [ ] **A4 (#162)**: Webhook handlers Meta/Telegram retornam 200 antes de ingestar; goroutine com `recover`; `context.Background()`.
- [ ] **A5 (#163)**: Coluna `scheduled_at` + índice parcial; query filtrada; reconciler drena em ordem cronológica.
- [ ] **A5.1 (#176)**: API `POST/GET/DELETE /api/messages/scheduled`; template admin web; reconciler tick 1s.
- [ ] **B1 (#164)**: Per-tenant + per-channel rate limit token-bucket; metric `ratelimit_drop_total`; opt-in via env.
- [ ] **B2 (#165)**: Meta token refresh automático via `fb_exchange_token`; `Keyring.SetCredentials` para cifrar; audit `meta.token.refreshed`.
- [ ] **B3 (#166)**: OIDC `nonce` validated; refresh token cifrado em `oidc_tokens` table; RLS FORCE; H2 fechado.
- [ ] **B4 (#167)**: Webhook secrets em `webhook_secrets` table (envelope); `KeyringMetaSecrets` com fallback env; `rotate-kek` re-cifra automaticamente.
- [ ] **B5 (#168)**: In-memory dedup cache (`sync.Map` + TTL 5min + LRU 100k); sweeper goroutine.
- [ ] **B6 (#169)**: `DLQConsumer` wired no boot; `DLQTotal` metric; alerta Prometheus `DLQSpike` em `deployments/prometheus/alerts.yaml`.
- [ ] **C1 (#170)**: `port.Channel` deletado; `domain.ChannelWAWeb = "whatsapp_web"`; `event.Channel` é type alias.
- [ ] **C2 (#171)**: Bus com quota per-tenant (100 default); metric `bus_tenant_quota_exceeded_total`; reconciler cobre o drop.
- [ ] **C3 (#172)**: whatsmeow capability matrix honesta (≤ 2 `ErrNotImplemented`); cada capability com smoke test.
- [ ] **C4 (#173)**: `goleak.VerifyTestMain` em 4 novos pacotes (total 11).
- [ ] **C5 (#174)**: `port.Sender.Ping(ctx) error`; 5 adapters implementam; `Registry.Health` usa Ping; admin endpoint.
- [ ] **C6 (#175)**: 5 novos testes E2E async + scheduled; `go test -tags=e2e -race ./tests/e2e/...` verde.
- [ ] **#158**: `*whatsmeow.Client` real substitui stub; `IdentityStore` SQLite persiste entre reboots; QR endpoint funcional.

### Não-funcional

- [ ] `go test -race -shuffle=on -count=1 -timeout 180s ./...` verde.
- [ ] `go test -tags=integration -race -timeout 30s ./...` verde.
- [ ] `go test -tags=e2e -race -timeout 60s ./tests/e2e/...` verde.
- [ ] `go vet ./...` e `go vet -tags=integration ./...` e `go vet -tags=e2e ./...` verdes.
- [ ] `gofmt -l` vazio.
- [ ] `govulncheck ./...` verde (incluindo `github.com/sony/gobreaker/v2`).
- [ ] Coverage: ≥ 80% nos packages novos (`breaker`, `ratelimit`, `dlq`, `oidc_tokens_repo`, `webhook_secrets_repo`, `sqlite/identity_store`).
- [ ] `make openapi-gen && git diff --exit-code api/openapi.gen.go` verde (#163 + #176).
- [ ] `promtool check alerts deployments/prometheus/alerts.yaml` verde (#169).
- [ ] Métricas Prometheus exportadas em `/metrics`: `breaker_state_change_total`, `outbox_retry_delay_seconds`, `ratelimit_drop_total`, `bus_tenant_quota_exceeded_total`, `webhook_handler_duration_seconds`, `webhook_dedup_hit_total`, `meta_token_refresh_total`, `DLQTotal`, `sender_health_check_total`, `scheduled_total`, `scheduled_fired_total`.
- [ ] `cmd/server rotate-kek` re-cifra `channel_credentials` + `webhook_secrets` (estende `secrets/rotate_kek.go:189-260`).
- [ ] Audit log: `breaker.open`, `breaker.close`, `meta.token.refreshed`, `oidc.nonce.validated`, `oidc.refresh_token.persisted`, `webhook_secret.stored`, `webhook_secret.loaded`, `ratelimit.drop`, `dlq.event`, `sender.health.failed`, `scheduled.create`, `scheduled.cancel`, `whatsmeow.paired`, `whatsmeow.unpaired`, `whatsmeow.reconnected`.

### Operacional

- [ ] `cmd/server/serve` boot em ≤ 5s (medido em `tests/boot/cold_boot_test.go` da Fase 8) — sem regressão.
- [ ] `cmd/server/serve` shutdown em ≤ 15s (drain de bus + Whatsmeow disconnect por tenant + pool close).
- [ ] Chaos test (`tests/chaos/`) verde com breaker + jitter: kill -9 entre commit e publish → reconciler recupera + breaker evita reentrada imediata.
- [ ] README §23 atualizado: Fase 9 marcada como merged; §25 limitação "multi-process" explicitamente referenciada como pós-Fase 9.
- [ ] CHANGELOG (`docs/CHANGELOG.md` se existir, ou top do `README.md`) com bullets por sprint.

---

## 5. Sequência de execução (timeline)

```
╔══════════════════════════════════════════════════════════════════════════╗
║ Sprint 0 (11.9d) — Auditoria de segurança (PRÉ-REQUISITO, Seção 12)     ║
║ ├── Day 0.2: §12.0 Housekeeping (#129, #130, #134, #136, #139, #141,   ║
║ │            #144, #152, #156 — fechar 9 issues stale em main)          ║
║ │                                                                       ║
║ ├── Sub-sprint 0A (3.4d) — CRITICAL                                     ║
║ │   ├── Day 0.5: #131 (cookie Secure)                                   ║
║ │   ├── Day 1.0: #132 (admin authorization) [bloqueia #135, #154]       ║
║ │   ├── Day 1.0: #133 (IDOR RunInTenantTx) [paralelo; bloqueia Sprint 3]║
║ │   ├── Day 0.5: #135 (role escalation) [depende #132]                 ║
║ │   ├── Day 0.3: #137 (restore _table allowlist)                        ║
║ │   └── Day 0.3: #138 (S3 tenant path confusion)                        ║
║ │                                                                       ║
║ ├── Sub-sprint 0B (4.4d) — HIGH [paralelo com 0A se 2 devs]            ║
║ │   ├── Day 0.5: #140 (OIDC nonce) [simplifica #166 Sprint 3]           ║
║ │   ├── Day 0.3: #142 (JWT entropy)                                     ║
║ │   ├── Day 0.2: #143 (ReadHeaderTimeout default)                       ║
║ │   ├── Day 0.3: #145 (CSRF setup)                                      ║
║ │   ├── Day 0.2: #146 (HSTS secure=true)                                ║
║ │   ├── Day 0.1: #147 (triage duplicate)                                ║
║ │   ├── Day 0.5: #148 (RunAsPlatform atomic)                            ║
║ │   ├── Day 1.0: #149 (concorrência bus/outbox/drain)                   ║
║ │   ├── Day 0.3: #150 (Echo dead code)                                  ║
║ │   └── Day 0.5: #151 (TLS nativo + redirect)                           ║
║ │                                                                       ║
║ └── Sub-sprint 0C (1.4d) — MEDIUM                                       ║
║     ├── Day 0.5: #153 (error sanitization)                              ║
║     ├── Day 0.3: #154 (audit tenant filter) [depende #132]              ║
║     ├── Day 0.1: #155 (triage duplicate)                                ║
║     └── Day 0.5: #157 (UUID v7)                                         ║
╚══════════════════════════════════════════════════════════════════════════╝
                              ↓ GATE: 0A fechado + testes verde
Sprint 1 (5d) — Production-blockers
├── Day 1: #159 (claim tx)
├── Day 2: #160 (circuit breaker)
├── Day 3-4: #161 (backoff persistente + jitter)
└── Day 5: #162 (async webhook) [depende #133 RunInTenantTx]

Sprint 2 (4d) — Scheduled posts
├── Day 1-2: #163 (schema + query + reconciler)
└── Day 3-4: #176 (API + UI placeholder)

Sprint 3 (4d) — Credenciais
├── Day 1-2: #165 (Meta refresh) [depende #133]
├── Day 2-3: #166 (OIDC nonce + refresh) [#140 já cobre nonce — só persistência]
└── Day 4: #167 (webhook secrets) [depende #133]

Sprint 4 (5d) — Resiliência + observability
├── Day 1-2: #164 (rate limit)
├── Day 2-3: #168 (dedup cache)
├── Day 3-4: #169 (DLQ consumer) [depende #132 admin authz]
└── Day 5: #170 + #171 + #173 (cleanup)

Sprint 5 (4d) — Whatsmeow real + health
├── Day 1-2: #158 (real client + IdentityStore)
├── Day 2-3: #172 (honest matrix)
├── Day 3-4: #174 (Ping) [depende #132 admin authz]
└── Day 4-5: #175 (E2E async)
```

**Total: ~34 dias úteis** (6-7 semanas solo, ou 3-4 sprints com 2 devs paralelizando Sprint 0A/0B).

### Dependências críticas

**Sprint 0 (gate para Sprint 1):**
- `#132` (#135, #154) — Principal hydration é base para qualquer admin endpoint novo.
- `#133` (#162, #165, #166, #167, #176) — `RunInTenantTx` é base para qualquer handler novo.
- `#140` (#166) — OIDC nonce simplifica #166 para só persistência.
- `#151` (#162) — TLS/regex same-origin são pré-requisito para webhook handler.

**Sprints 1–5 (originais):**
- `#159` (#161, #164) — claim em tx é base para todos que dependem de retry/backoff.
- `#161` (#163, #164) — `next_attempt_at` é usado por retry e rate limit.
- `#160` (#161) — breaker wrap vem antes do backoff (ordem de execução no relay).
- `#162` (#168) — async handler é pré-requisito do dedup edge.
- `#163` (#176) — schema é base da API.
- `#165, #166, #167` — paralelizáveis; cada um isolado.

---

## 6. Definition of Done (Sprint 1 — production-blockers)

- [ ] #159: `go test -tags=integration -run TestOutboxClaim_Concurrent` verde.
- [ ] #160: 6 falhas consecutivas abrem breaker; 3 success após Timeout fecham; per-tenant isolado.
- [ ] #161: jitter range `[0, 30s]` validado; backoff `60s, 120s, 240s`; query honra `next_attempt_at`.
- [ ] #162: `t_200_returned - t_request_arrived < 50ms` com DB tx 500ms (test em #175 já cobre).
- [ ] Chaos test (`tests/chaos/`) verde com breaker + jitter compostos.
- [ ] `cmd/server/serve` boot em ≤ 5s sem regressão.
- [ ] `make test && make test-integration && make test-e2e` todos verdes.

---

## 7. Riscos e mitigações

| # | Risco | Probabilidade | Impacto | Mitigação |
|---|-------|---:|---:|---|
| R1 | `sony/gobreaker` v2 não compila com Go 1.22 | Baixa | Médio | `go.sum` lock; fallback para `cenkalti/backoff/v4` (já é indirect) implementado in-house (~100 LOC). |
| R2 | `*whatsmeow.Client` real tem API breaking entre versões | Média | Alto | Lock em `go.sum`; testes com `IdentityStore` mock primeiro (#158 sem rede). |
| R3 | `fb_exchange_token` tem rate limit próprio (não documentado pela Meta) | Média | Médio | Cache de 24h entre refreshes; circuit breaker no client OAuth. |
| R4 | OIDC `nonce` validation rejeita tokens válidos de IdPs legados | Baixa | Médio | Feature flag `MEZ_OIDC_REQUIRE_NONCE` (default `true` em prod, `false` em dev). |
| R5 | Bus partition per-tenant aumenta memória (1 chan por tenant) | Média | Baixo | Bounded por `MEZ_MAX_ACTIVE_TENANTS` × `cap`; default cap=100; LRU eviction. |
| R6 | Scheduled posts em massa sobrecarregam relay no tick 1s | Baixa | Médio | Limite de batch (1000/tick); overflow vai para próximo tick; circuit breaker (#160) cobre. |
| R7 | Async webhook (goroutine) vira leak se `recover` falhar | Baixa | Alto | `goleak.VerifyTestMain` em #173; chaos test cobre kill -9 entre spawn e commit. |
| R8 | DLQ buffer (256) saturado em outage massivo | Média | Médio | Métrica `DLQDroppedTotal` (Fase 8 já tem); alerta Prometheus; sink externo (S3) é pós-Fase 9. |
| R9 | `RotateKEK` não cobre `webhook_secrets` se `ForEachTenant` não iterar | Baixa | Alto | Test em #167: criar secret + rotate + verificar que `webhook_secrets_repo.Load` retorna decifrado. |
| R10 | UI admin (templ) do `scheduled.html` é placeholder sem UX | Alta | Baixo | Documentado como `wontfix-1.0`; UI real vem em sprint pós-Fase 9. |

---

## 8. Decisões arquiteturais (ADR novos)

- **ADR-0022 — Circuit breaker per `(tenant, channel)`**. Justificativa: opt-in por canal, sem Redis, in-memory pure Go, audit + metric. Trade-off: estado não compartilhado entre instâncias (single-process continua sendo premissa).
- **ADR-0023 — Jitter em backoff `±30s`**. Justificativa: cobre intervalo entre relays sincronizados sem precisar de coordination; simples; testável.
- **ADR-0024 — `scheduled_at` em `outbound_events` (não tabela separada)**. Justificativa: mesma FSM, mesma tx, mesmo outbox; query com índice parcial é eficiente. Trade-off: colunas nullable em row gorda.
- **ADR-0025 — Async webhook com `context.Background()`**. Justificativa: client pode desconectar antes da tx terminar; cancelamento de `r.Context()` cancelaria a DB tx mid-flight. Reconciler cobre se a goroutine morrer.
- **ADR-0026 — Meta refresh client-side (não server-side via scheduled job)**. Justificativa: lazy + on-demand; reduz load em cron; audit por evento é granular. Trade-off: primeiro request com 401 tem latência 2x (~500ms extra).
- **ADR-0027 — `OIDCState.Nonce` em cookie + state, não em DB**. Justificativa: nonce é short-lived (10min); cookie+state+S256 PKCE é suficiente; H2 fix sem migration.
- **ADR-0028 — `webhook_secrets` em tabela dedicada (não reusa `channel_credentials`)**. Justificativa: 1 tenant pode ter N `app_id` Meta; UNIQUE `(tenant_id, channel, app_id)` é o shape certo. Mesma `Keyring` cifra ambos.
- **ADR-0029 — Bus quota per-tenant (não partition real)**. Justificativa: simplicidade; O(1) memória por tenant; rollback trivial. Partition real é pós-Fase 9 com Redis ou NATS JetStream.

### ADRs complementares (Fase 9 + carryovers Fase 8 formalizados)

- **ADR-0032 — `whatsmeow.IdentityStore` SQLite file-per-tenant** [#158]. Decisão: cada tenant tem um arquivo SQLite dedicado em `MEZ_DATA_DIR/tenants/<tenantID>/whatsmeow.db` que armazena o `*proto.Identity` cifrado por envelope. **Não** usar coluna `BYTEA` em `channel_credentials`. Justificativa: (a) `whatsmeow.Client` espera um `store.DeviceStore`; (b) blob binário grande (10-50KB) e hot-row em `channel_credentials` é anti-pattern; (c) file-per-tenant simplifica backup incremental; (d) envelope encryption (KEK→DEK, mesma `LocalSealer`) garante zero plaintext em disco. Trade-off: 1 arquivo por tenant aumenta contagem de files (mitigado: `MEZ_MAX_ACTIVE_TENANTS=100` teto); `mount` NFS em cluster K8s precisa de `ReadWriteMany` (Fase 10+ troca para `efs.csi.aws.com` ou statefulset com `volumeClaimTemplates`); recovery pós-`kill -9` reidrata do SQLite file (≤ 2s).

- **ADR-0033 — Rate limit per-`(tenant, channel)` in-memory token-bucket (sem Redis)** [#164]. Decisão: `internal/adapter/cache/memory/ratelimit.go` mantém `map[cacheKey]*ratelimit.Limiter` com token-bucket (`golang.org/x/time/rate` ou in-house ~80 LOC). **Não** usar Redis. Justificativa: (a) single-process é premissa da Fase 9; in-memory é O(1) por acesso; (b) quota por canal muda raramente — não precisa de persistência; (c) restart do processo perde quotas, e isso é **desejável** (evita back-pressure acumulado pós-outage); (d) `golang.org/x/time/rate` é dep stdlib-level, sem risco de supply chain. Trade-off: rate limit não é compartilhado entre réplicas (Fase 10+ tem `n_tenants × n_replicas` taxa efetiva; aceitável porque canais cloud já impõem quotas próprias — WABA tier 1 = 80/s, IG = 200/h, MSG = 200/h, TG = 30/s). Migração futura para Redis é trivial (interface `port.RateLimiter` com 2 impls).

- **ADR-0034 — `port.Sender.Ping(ctx) error` adicionado à interface (interface segregation vs Adapter pattern)** [#174]. Decisão: adicionar método `Ping(ctx) error` à `port.Sender` (atualmente 3 métodos: `Send`, `Capabilities`, `MediaUpload`). **Não** criar `port.HealthChecker` separado. Justificativa: (a) health check é responsabilidade primeira do sender; (b) interface segregation argument aplica quando há múltiplos consumers de subconjuntos — aqui o único consumer é `Registry.Health()`, que sempre quer todos os canais; (c) adicionar à interface força todos os adapters a implementarem (zero `panic: not implemented` em runtime); (d) `Ping` é trivial em WABA/IG/MSG (GET `/me?fields=id`); `IsConnected()` em whatsmeow é 1 atomic load. Trade-off: quebra a interface para implementações externas (não há — todas internas); 5 adapters precisam adicionar ~10 LOC cada (~50 LOC total). Sem migration porque a interface é interna ao `core/port`.

- **ADR-0035 — DLQ como stream in-process (audit + metric + log), sem sink externo** [#169]. Decisão: `bus.SubscribeDLQ` consome em `internal/usecase/dlq/consumer.go` que **apenas** escreve audit row + `DLQTotal` metric + `log.Error`. **Não** persiste em tabela `dlq_archive`, **não** envia para S3/OpenSearch. Justificativa: (a) DLQ é para **operação humana** (runbook: "tem msg na DLQ, investigar"), não para replay automático; (b) audit log imutável (Fase 2 carryover) já dá auditoria completa (`action='dlq.event'` com `metadata={channel, msg_id, error}`); (c) `DLQTotal` com label `reason` permite alertas (`rate(DLQTotal[5m]) > 10`); (d) `log.Error` com structured logging vai para Loki/ELK, satisfaz retention de 30 dias. Trade-off: 30 dias é teto (vs S3 = ilimitado); queries complexas (top reasons por tenant) exigem correlacionar com audit log. Sink externo é ADR separado para Fase 10+.

- **ADR-0036 — Webhook dedup cache `sync.Map` + TTL 5min no edge (sem DB lookup)** [#168]. Decisão: `dedupCache sync.Map` in-process com TTL 5min + LRU 100k entries. **Não** usar Redis SETNX, **não** usar `SELECT ... FOR UPDATE` no DB. Justificativa: (a) edge optimization tem que ser O(1) sem I/O — Meta retenta em < 1s; (b) `sync.Map` é lock-free para read-heavy, que é o caso (webhook é read no dedup); (c) LRU + TTL garante memória bounded (100k × 50B ≈ 5MB por instância); (d) DB `ON CONFLICT` (Fase 3 carryover) é **fallback** se cache miss (ex: restart do processo) — não é fonte de verdade. Trade-off: cache perdido em restart (mitigado por ON CONFLICT no DB); não compartilhado entre réplicas (Fase 10+ pode usar Redis shared dedup, mas local é suficiente para scale-out de webhook ingress). Sweeper goroutine tem `defer recover()` e `goleak.VerifyTestMain` (#173).

- **ADR-0037 — Coordinator pool em `session-mode` PGBouncer (não `transaction-mode`)** [#178, Seção 11]. Decisão: `coordinator_pool` configurado com `pgxpool.Config{ConnConfig.RuntimeParams["pool_mode"] = "session"}` ou PGBouncer config com `pool_mode=session` para o role `mez_coordinator` (criar role novo). **Não** usar `transaction-mode` (default PGBouncer). Justificativa: `pg_try_advisory_lock` é **session-scoped** — em `transaction-mode`, PGBouncer pode atribuir 2 statements consecutivos a backends diferentes, fazendo o lock adquirido em S1 ser liberado antes de S2 usar. Documentado em [PG docs §13.3.5](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS): "advisory locks are session-level". Trade-off: session-mode reduz densidade de conexões (1 conn por coordinator thread ativo); aceitável porque Coordinator tem ~10-50 threads (não 1000s); em K8s, setar `PGOPTIONS: "-c pool_mode=session"` no pod spec. Conexões `appPool` e `platformPool` continuam em transaction-mode (não usam advisory locks).

- **ADR-0038 — `whatsmeow.Manager` LRU eviction por `MEZ_MAX_ACTIVE_TENANTS` (não por memória RSS)** [Fase 4 carryover, formalizado]. Decisão: eviction é por **contagem de tenants ativos**, não por RSS/memória. Justificativa: (a) 1 client whatsmeow ≈ 10-50MB RAM + 1 WebSocket; 100 clients ≈ 1-5GB (cabe num pod de 8GB); (b) contagem é O(1) (`atomic.Int64`), RSS sampling é caro; (c) limite por tenant é **negócio** (sla "100 tenants ativos"); (d) LRU já existe (Fase 4 carryover, `whatsmeow/manager.go:174`); tenant evicted = session salva no `IdentityStore` (não perdida) + reconect on-demand (≤ 2s, transparente). Trade-off: tenant evicted durante uso intenso tem que reconectar; aceitável porque WhatsApp tolera reconnect (já faz diariamente via `reconnect.go`). Memória não é o limiter — é a métrica de **complexidade de gerência** (100 sessions é o máximo que 1 pod consegue coordenar sem GC pauses visíveis).

- **ADR-0039 — `pkg/lifecycle.Runner` phases como único mecanismo de boot/shutdown (substitui `init()` e goroutines globais)** [Fase 8 carryover, formalizado]. Decisão: toda goroutine de longa duração, todo adapter, todo client externo é registrado como `Phase` no `Runner` via `runner.Add(PhaseFunc)`. **Proibido** `init()` que abra goroutines ou conexões; **proibido** `var X = startBackground()`. Justificativa: (a) `init()` é non-deterministic (ordem alfabética de packages), vira fonte de bugs sutis; (b) goroutine global sem owner = impossível fazer graceful shutdown coordenado (D10 da Fase 8); (c) `Runner` provê `SIGTERM → stop em ordem inversa → drain com timeout → exit 0`; (d) testabilidade: `Runner` aceita `WithTimeout`/`WithSkip` para tests. Trade-off: mais cerimônia no boot (cada adapter precisa implementar `PhaseFunc`); ~280 LOC de `pkg/lifecycle` que é puro overhead para single-binary. Aceitável: o overhead é **declarativo** (declarar fases), não **computacional**.

- **ADR-0040 — Bus in-process tipado como substituição de NATS JetStream (carryover Fase 8)** [Fase 8 carryover, formalizado]. Decisão: `internal/adapter/broker/bus.go` implementa `bus.Bus` com channels Go nativos + `safeCall` para recover. **Não** usar NATS, Kafka, RabbitMQ. Justificativa: (a) single-process é premissa (Fase 9) — bus atravessa fronteira in-process; (b) latência: in-process < 1μs vs NATS ~1ms; (c) zero infra nova (alinhado com `AGENTS.md §1.1`); (d) tipos via generics Go 1.22 (`chan T` tipado, não `[]byte`); (e) `safeCall` por subscriber garante panic isolation. Trade-off: bus não atravessa pods (Fase 10+ precisa de NATS ou similar para multi-process; carryover das issues #177-#180); bus em memória perde mensagens em `kill -9` (mitigado por outbox pattern + reconciler C1). Migração futura: trocar implementação atrás da interface `bus.Bus` mantém consumers intactos.

### ADRs de segurança (Sprint 0, Seção 12)

- **ADR-0041 — FailClosed-by-default para security checks** [Sprint 0, Seção 12.7]. Decisão: **toda verificação de segurança é opt-out em dev, opt-in em prod**. Cookie `Secure`, TLS, HSTS, OIDC nonce, entropy check, CSRF, error sanitization são todos default-ON quando `MEZ_ENV=prod` ou `MEZ_ENV` unset + binary não-dev. Issues relacionadas: #131, #140, #142, #143, #145, #146, #151, #153. Justificativa: histórico do projeto mostra que defaults permissivos em código (DREAD ≥ 7.5 no audit 003) viram production-blockers; reverter isso com feature flags é operacionalmente caro. **Manifesto:** security é o default; relaxar é exceção explícita. Trade-off: dev local sem HTTPS precisa `MEZ_ENV=dev`; documentado em `AGENTS.md` e `docs/security/DEV_MODE.md` (NOVO). Implementação: helper `pkg/config.IsProdMode(cfg) bool` que centraliza a lógica (`MEZ_ENV=prod` ou unset + binary tag `prod`); usado em todos os 8 pontos de gate.

- **ADR-0042 — Principal Hydration no session middleware** [Sprint 0, #132, Seção 12.7]. Decisão: o middleware de sessão **hidrata `Principal.Permissions` e `Principal.Roles`** ao carregar a sessão, em vez de delegar a cada handler. Cache in-memory TTL 5min por `(userID, tenantID)` usando `sync.Map` + `singleflight.Group`. Justificativa: (a) `admin.Evaluate` precisa de `Permissions` populado — sem hydration, a chamada sempre nega; (b) cache 5min evita N+1 em loops de admin panel; (c) TTL curto garante que revogação de role propaga em ≤ 5min (aceitável para admin panel); (d) `singleflight.Group` evita thundering herd quando N requests chegam ao mesmo tempo. Trade-off: 1 query extra por session start (otimizada para ≤ 5ms com índice em `role_bindings.user_id`); session start latency aumenta ~3ms p50. Risco: sessão já ativa pós-deploy pode ter `Permissions = nil` (mitigado por flag `MEZ_AUTHZ_STRICT=false` durante 1 release + `MEZ_AUTHZ_ROLLOUT_PCT=10→50→100`).

- **ADR-0043 — Defense-in-Depth RLS + WHERE clause** [Sprint 0, #133 + #137 + #138 + #154, Seção 12.7]. Decisão: **toda query multi-tenant tem DUPLA barreira**: (a) `RunInTenantTx` (RLS via context), E (b) `WHERE tenant_id = $1` explícito na query. Justificativa: (a) RLS já é FORCED (C3 + C4 do audit 003), mas bugs em pool routing podem bypassar (DREAD 9.0 do C5); (b) `WHERE tenant_id = $1` é catch-all que pega o caso degraded; (c) audit row em qualquer query que toque mais de 1 tenant (via `EXPLAIN` instrumentation); (d)符合 defense-in-depth principle do STRIDE. Trade-off: +1 coluna em toda query, +1 índice em cada tabela (já temos); verbosidade do código. Aceitável: o boilerplate fica centralizado em `txRunner.RunInTenantTx` (helper do #133) e em `s3.WithTenantPrefix(...)` (helper do #138); código de handler fica `txRunner.RunInTenantTx(ctx, jwtTenantID, func(txCtx) { h.msgRepo.Get(txCtx, msgID) })` — `msgRepo.Get` recebe o txCtx e adiciona `WHERE tenant_id = $1` automaticamente. Issues cobertas: #133 (IDOR), #137 (SQLi defense-in-depth em restore), #138 (S3 path confusion), #154 (audit query cross-tenant).

### ADRs roadmap (documentados em Seção 11, **não-execução Fase 9**)

- **ADR-0030 — Capability-based auto-claim com Postgres como coordinator único**. Justificativa: zero infra nova; mesmo binário Docker com role flag (`--role={gateway,relay,whatsmeow-worker,all}`); coordenação via `coordinator_capabilities` table; elástico de verdade. Trade-off: Postgres vira SPOF lógico (mitigado por RPO/RTO ≤ 90s e replicação streaming existente). Alinhado com `AGENTS.md §1.1` (sem Redis/NATS).
- **ADR-0031 — `pg_try_advisory_lock(hashtext(tenantID || 'whatsmeow'))` como primitive de leader election per-session**. Justificativa: nativo PG, não-bloqueante, automático cleanup em disconnect do backend, lock key deriva deterministicamente do tenantID. Trade-off: latência ~1-5ms por lock vs ~0.1ms Redis (aceitável para scale-out, irrelevante para single-replica). Sem dependência adicional.

---

## 9. Não-objetivos (explícitos)

- **Execução do coordinator multi-replica na Fase 9** — design documentado em **Seção 11** (ADRs 0030-0031, issues #177-#180); execução prevista para Fase 10+ como carryover. Single-replica continua sendo o default suportado e testado na Fase 9.
- **Vault Transit sealer** (ADR já diz pós-1.0).
- **OpenSearch / analytics** (descartado em §2 do `docs/plan.md`).
- **UI/UX do painel de scheduled posts** (apenas API + template placeholder; UX real é sprint pós-Fase 9).
- **Sink externo para DLQ** (S3 sink é pós-Fase 9; por enquanto audit + metric + log).
- **Auto-scaling / k8s HPA** (deployment via Helm chart é pós-Fase 9; specs de HPA/PodDisruptionBudget/anti-affinity são citadas na Seção 11.4 mas não entregues).
- **Scheduled posts recurring (cron syntax)** — Fase 9 cobre `scheduled_at` (one-shot). Cron syntax (RRULE / Quartz-like) é Fase 10+.
- **WebSocket fan-out para scheduled events** (UI real-time é pós-Fase 9).
- **Message broker externo (NATS/Redis)** — explicitamente fora do escopo; bus in-process + Postgres advisory locks são a estratégia oficial (Seção 11.2-Q5 e ADR-0031).

---

## 10. Referências

- **Auditoria 5-pilares** (origem do plano): conversa com Arquiteto Sênior SMM (junho/2026), 5 pilares: Adapter/Factory, Resiliência/RateLimit, Credenciais, Agendamento, Webhooks/Real-time.
- **Fase 8 PLAN**: `docs/fase8/PLAN.md` (988 LOC) — base normativa para boot/shutdown phases.
- **Auditoria de Segurança**: `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (40 findings: 10 CRITICAL + 15 HIGH + 15 MEDIUM) — `nonce` OIDC, SQLi, IDOR, RLS, auth/authz, concurrency, deps. **Origem do Sprint 0 (Seção 12).**
- **Plano de Auditoria**: `docs/fase8/FIXES/PLAN_SECURITY_AUDIT.md` — status tracking das 40 findings + fases de remediação.
- **Auditoria Arquitetural**: `docs/fase8/FIXES/002_ARCHITECTURE_REVIEW.md` (item 1.1) — SKIP LOCKED.
- **Auditoria DDD-Hexagonal**: `docs/fase8/FIXES/001_DDD_HEXAGONAL_REVIEW.md` — base do `port.Sender`; item 3.11 `appQFromCtxOrPool` UNSAFE é base do #133.
- **PR #108** (`bdee3cd`): commits `cc08aa9`, `38368f4`, `a6ee296`, `bcbb880`, `aba5b9b`, `05d6d7a`, `5fdc0b7`, `4177bf2` — 9 fixes de segurança já em `main` que serão housekeeping-fechados no §12.0.
- **`docs/plan.md`**: roadmap 0–8; Fase 9 é extensão pós-1.0.
- **AGENTS.md**: §1 Identidade, §10 Patterns obrigatórios, §1.1 guardrails (sem Redis/NATS/Vault/multi-process).
- **`mez-go` pai AGENTS**: `/home/user/felipedsvit/mez-go/AGENTS.md` — referência semântica (não porte literal).
- **Seção 11** (multi-replica): ADRs 0030-0031 + issues #177-#180 — carryover de design para Fase 10+.
- **Seção 12** (Sprint 0): auditoria de segurança pré-requisito, 20 issues + 9 housekeeping, ADRs 0041-0043.

---

## 11. Análise de escalabilidade horizontal — whatsmeow + canais stateless

> **Status:** roadmap documentado · execução em Fase 10+ · carryover de design das issues #177–#180.
> **Origem:** análise arquitetural 5-pilares (junho/2026) + decisão explícita de manter `mez-go-mono` single-process na Fase 9 e planejar evolução multi-replica sem violar os guardrails do `AGENTS.md §1.1` (sem Redis, sem NATS, sem Vault).
> **Premissa honesta:** o `whatsmeow` é o **único canal stateful** do gateway; WABA/Instagram/Messenger/Telegram são stateless (HTTP request/response). Por isso, escalabilidade horizontal tem **dois regimes distintos** — scaling linear para canais cloud, sharding natural por sessão para whatsmeow.

### 11.1 Princípios de design (não-negociáveis)

1. **Mesma imagem Docker, múltiplas roles via flag + capability advertisement.** O binário `cmd/server/serve` aceita `--role={gateway,relay,whatsmeow-worker,all}` e auto-detecta o ambiente (K8s, Docker Swarm, single-node). Mesmo `Containerfile`; mesmo `docker push`; mesmo Helm chart com `replicaCount` parametrizado por role.
2. **Postgres é a única fonte de verdade para coordenação.** `pg_try_advisory_lock()` + tabela `coordinator_capabilities` + `coordinator_session_claims` substituem Redis/Sentinel/Consul/ZooKeeper. Trade-off: latência ~1-5ms por lock (vs ~0.1ms Redis) — aceitável para coordenação de sessões whatsmeow, irrelevante para o caminho hot (HTTP→send).
3. **Canais stateless (WABA/IG/MSG/TG) escalam linearmente** — qualquer pod com `role=gateway` ou `role=all` pode atender request de qualquer tenant. RLS via `RunInTenantTx` continua sendo a fronteira de isolamento. **Nenhuma coordenação extra** necessária.
4. **Whatsmeow tem sharding natural por sessão** — 1 `*whatsmeow.Client` ↔ 1 `(tenant, JID)`. A coordenação decide **qual pod hospeda qual sessão**; o relay HTTP é agnóstico. N pod suporta N×K sessões (onde K = limite do pod, ex: 100).

### 11.2 Resposta às 10 questões da análise SMM

#### Q1 — Como tornar a arquitetura horizontalmente escalável?

Três eixos independentes:

| Eixo | Componente | Como escala | DoD |
|---|---|---|---|
| **Eixo A — Canal stateless** | pods `role=gateway` | HPA por CPU/RPS; anti-affinity por zona | Latência p99 < 200ms com 3 réplicas |
| **Eixo B — Relay** | pods `role=relay` | Stateless; compete por `SKIP LOCKED` no `outbox` | Drain < 15s; throughput > 1k msg/s com 2 réplicas |
| **Eixo C — Whatsmeow** | pods `role=whatsmeow-worker` | Sharding por `(tenant, JID)` via `pg_try_advisory_lock` | Cap = 100 sessões/pod (carryover `MEZ_MAX_ACTIVE_TENANTS`); lease TTL 60s |

**Não há plano para escalar o `port.SenderRegistry` em si** — ele é in-memory por pod e descobre adapters via `pkg/lifecycle.Runner`. A coordenação é por *sessão*, não por *adapter*.

#### Q2 — Estratégias para manter single image + diferentes comportamentos

```go
// cmd/server/serve.go (NOVO — não codificado na Fase 9)
type Role string
const (
    RoleGateway        Role = "gateway"        // API HTTP, webhook ingress, scheduler
    RoleRelay          Role = "relay"          // outbox drain, scheduled posts
    RoleWhatsmeowWorker Role = "whatsmeow-worker" // whatsmeow session hosting
    RoleAll            Role = "all"            // single-replica / dev
)

func (r Role) Initializes(c Component) bool { ... }
```

- **`--role=gateway`** inicializa: HTTP, webhooks Meta/TG, reconciler scheduled, admin API. **NÃO** inicializa: whatsmeow Manager, outbox Relay (sai do processo).
- **`--role=relay`** inicializa: outbox Relay, scheduled posts tick, DLQ consumer. **NÃO** inicializa: HTTP, webhooks.
- **`--role=whatsmeow-worker`** inicializa: whatsmeow Manager + Coordinator Claim Loop. **NÃO** inicializa: HTTP, webhooks, outbox Relay.
- **`--role=all`** (default em dev) inicializa tudo; warning de log "single-process mode" para sinalizar que não escala.
- **Capability advertisement**: ao subir, o pod grava em `coordinator_capabilities` (id, role, version, mem_mb, max_sessions, started_at). O Coordinator usa isso para **least-loaded claim**.

**Compatibilidade retroativa:** se `MEZ_ROLE` não estiver setada, default = `all` (comportamento Fase 9). Zero breaking change.

#### Q3 — Auto-detecção de multi-replica

Detecção em **3 camadas** (todas read-only, sem side-effects):

```go
// internal/adapter/coordinator/environment.go (NOVO — Seção 11)
type Environment struct {
    IsKubernetes bool            // KUBERNETES_SERVICE_HOST presente
    IsSwarm      bool            // DOCKER_SWARM_PRESENT + /etc/docker-events
    IsCompose    bool            // COMPOSE_PROJECT_NAME presente
    PodName      string          // POD_NAME (K8s downward API) ou HOSTNAME
    NodeName     string          // NODE_NAME (K8s) ou hostname
    TotalReplicas int            // hint via env MEZ_EXPECTED_REPLICAS (optional)
}

func DetectEnvironment(ctx context.Context) Environment { ... }
```

- **K8s**: detection via `KUBERNETES_SERVICE_HOST` (sempre setado em pods). Downward API injeta `POD_NAME`/`POD_NAMESPACE`/`NODE_NAME`. **Recomendação Helm:** `MEZ_EXPECTED_REPLICAS={{ .Values.replicaCount }}` para o Coordinator detectar subdimensionamento.
- **Docker Swarm**: detection via `/proc/1/cgroup` (proc/self/cgroup contém `docker-<id>` e `kubepods` etc). `docker service ls` é legível só do manager — usar `TASK_ID` env var que Swarm injeta.
- **Compose local**: detection via `COMPOSE_PROJECT_NAME` env. Total replicas = `1` (sem auto-scale).
- **Binário standalone**: tudo ausente → `IsStandalone=true`, `ExpectedReplicas=1`. **Coordinator ainda roda** mas em modo `single-leader` (primeiro pod que ganhar `pg_try_advisory_lock(NOLOCK_KEY)` vira owner de tudo; outros ficam standby).

#### Q4 — Coordenação de conexões whatsmeow entre múltiplas instâncias

**Modelo:** cada sessão whatsmeow tem **exatamente 1 pod owner**. O owner roda o `*whatsmeow.Client`, mantém o WebSocket aberto, despacha eventos. Outros pods **não** tocam na sessão.

**Mecanismo:** `pg_try_advisory_lock(hashtext(tenantID || ':' || JID))` no `coordinator/claim.go` (issue #178):

```sql
-- Pseudo-SQL (executado em appPool, não platform)
SELECT pg_try_advisory_lock($1)  -- $1 = hashtext(tenantID || ':' || JID)::bigint
```

- Se retorna `true` → pod é owner; `Manager.GetOrCreate(tenantID, jid)` carrega a sessão.
- Se retorna `false` → outro pod é owner; pod atual **não** inicializa sessão; expõe endpoint `GET /internal/sessions/{tenant}/{jid}/owner` para relay HTTP rotear dispatch (se relay for diferente de worker — caso comum em produção).
- **Lease TTL 60s** + heartbeat a cada 20s (issue #179). Se 3 heartbeats consecutivos falham (60s sem renew), Postgres libera o lock automaticamente ao disconnect do backend, **ou** o pod re-detecta via query periódica.
- **Cleanup explícito** no `cmd/server serve` shutdown: `pg_advisory_unlock_all()` no `pgxpool` antes de fechar.

**Por que não usar channel inteiro:** `pg_advisory_lock(key)` (bloqueante) é usado **apenas** no boot do worker (espera 1ms antes de desistir). Em steady-state, `pg_try_advisory_lock` é non-blocking — worker testa 1x/5s para sessões que ainda não tem.

#### Q5 — Mecanismos de lock distribuído

| Opção | Latência | Infra nova | Consistência | Operação | Decisão |
|---|---|---|---|---|---|
| **Postgres `pg_try_advisory_lock`** (escolhido) | ~1-5ms | **Nenhuma** (já tem PG) | Strong (single-Postgres é source of truth) | Trivial (já temos PG backup/streaming replication) | **✓** |
| Redis (Redlock) | ~0.1ms | Redis + Sentinel/Cluster | Eventual (split-brain risk em net partition) | Moderada (HA Redis é non-trivial) | ✗ viola guardrail |
| Consul | ~5-10ms | Consul cluster | Strong (RAFT) | Moderada | ✗ viola guardrail |
| ZooKeeper | ~5-10ms | ZK ensemble | Strong (ZAB) | Alta (operação complexa) | ✗ viola guardrail |
| etcd | ~5-10ms | etcd cluster | Strong (RAFT) | Alta | ✗ viola guardrail |
| K8s Lease object | ~10-50ms | none (K8s API) | Strong | Trivial **dentro** do K8s | ✗ amarra a K8s |

**Decisão: Postgres advisory locks.** Justificativa: já é a única dependência de estado; replicação streaming (já configurada em prod) garante RPO ≤ 5s; RTO ≤ 90s via `pg_auto_failover` ou patroni. Lock é **per-session**, não global — não vira gargalo de cluster. Em caso de failover do Postgres, leases expiram em ≤ 60s e workers re-claimam (SLO de recovery aceito).

#### Q6 — Microserviço whatsmeow dedicado vs integrado

**Recomendação: integrado** (mesmo binário, role flag). Justificativa:

| Critério | Microserviço dedicado | Integrado (role flag) | Vencedor |
|---|---|---|---|
| **Overhead de deployment** | 1 Helm chart extra, 1 image registry tag, 1 service mesh config | Mesmo Helm chart, 1 image, 1 release | **Integrado** |
| **Reuso de RLS/audit/bus/Keyring** | Duplicar pkg/lifecycle, port.Sender, bus.Bus, secrets.Keyring | Direto | **Integrado** |
| **Latência dispatch worker→relay** | +1-5ms HTTP/gRPC interno | 0 (mesmo processo) | **Integrado** |
| **Isolamento de falhas** | Crash do whatsmeow ≠ crash do gateway | Crash do whatsmeow trava o pod | **Dedicado** |
| **Escala independente** | Sim (HPA separado) | Sim (HPA por role) | Empate |
| **Complexidade de teste** | Testcontainers + 2 binários | Testcontainers + 1 binário com flag | **Integrado** |
| **Custo de migração** | Reescrever 30% dos imports | Mudar 1 struct + 1 flag | **Integrado** |

**Trade-off aceito (integrado):** um panic em whatsmeow derruba o pod. Mitigação: `recover()` por sessão (já existe no `Manager`), `recover()` por handler (Fase 9 #162), `pkg/lifecycle.Runner` phases com `Supervise()`. Em K8s, o pod é recriado em < 30s; leases expiram em ≤ 60s; outras sessões re-claimam automaticamente.

#### Q7 — Padrões arquiteturais (mapeamento para o mez-go-mono)

| Padrão | Onde no mez-go-mono | Adoção | Observação |
|---|---|---|---|
| **Actor Model** | `whatsmeow.Manager` (1 ator por sessão) | ✓ herdado | Mensagens = chamadas de método serializadas; `connected atomic.Bool` + mutex |
| **Session Affinity** | `coordinator/claim.go` (rota por session_hash) | ✓ Seção 11 | LB → worker que tem a sessão; senão, enfileira |
| **Sticky Routing** | `coordinator_capabilities` table (worker anuncia "tenho sessão X") | ✓ Seção 11 | Sticky = claim; relay consulta tabela antes de dispatch |
| **Sharding por sessão** | `crc32(tenantID||JID) % N` é o **shard key natural** | ✓ herdado (Fase 4) | Manager LRU eviction já é sharding implícito |
| **Event-driven** | `bus.Bus` (in-process) | ✓ herdado (Fase 8) | Single-replica; multi-replica exigiria NATS (não-Objetivo Fase 9) |
| **CQRS** | `inbound` (write, bus) ≠ `outbox` (read, relay) | ✓ herdado | Mesma tabela `outbound_events`; read replica não é in-scope |
| **Message Queue** | `outbox` table com `SKIP LOCKED` | ✓ herdado (Fase 2) | Substitui MQ externo; at-least-once delivery |
| **Worker Pool** | `relay.Run` + per-channel goroutine | ✓ herdado | Pool size = `MEZ_RELAY_WORKERS` (default 4) |

**Nada novo é introduzido** — Seção 11 só formaliza como cada padrão já está (ou pode ser) implementado.

#### Q8 — Failover quando owner da sessão falha

**Cenário:** pod A é owner da sessão `(tenant=T, jid=J)`. Pod A sofre `kill -9` (kernel OOM, host failure, node drain).

**Sequência de recuperação:**

1. **T+0s**: pod A morre. WebSocket whatsmeow desconecta. Lease `pg_try_advisory_lock(key)` em A **persiste** no Postgres até o backend ser desconectado.
2. **T+5s**: Postgres detecta TCP RST/FIN do backend A; backend é removido do pool; **`pg_advisory_unlock_all()` é chamado implicitamente pelo driver `pgx`** ao detectar disconnect. Lock `key` é liberado.
3. **T+5-60s**: pod B (outro worker) tem heartbeat loop que testa `pg_try_advisory_lock(key)` a cada 5s. **T+10s** (próximo tick) B ganha o lock; chama `Manager.GetOrCreate(T, J)` que carrega `IdentityStore` e reconecta.
4. **T+15-30s**: sessão whatsmeow restabelecida; `whatsmeow.reconnected` audit row; mensagens em-flight durante o gap são cobertas pelo **reconciler C1** (Fase 2 carryover) que varre `outbound_events` com `attempts > 0` e reenvia.
5. **T+90s**: SLO de recovery cumprido (RTO ≤ 90s).

**Detecção proativa de orphan:** `reconciler/coordinator_orphan.go` (issue #180) varre `coordinator_session_claims` a cada 30s; se `last_heartbeat_at < NOW() - 90s`, marca como `orphan` e dispara `pg_advisory_unlock(key)` + audit `coordinator.orphan.detected`. Reduz T+recovery de 60s para 30s no pior caso.

#### Q9 — Distribuição de novas sessões entre instâncias

**Estratégia: least-loaded claim com sticky preference.**

```sql
-- coordinator_claim_workflow.sql (Seção 11, referência)
-- 1. Worker quer claim sessão (T, J)
SELECT count(*) AS active
FROM coordinator_session_claims
WHERE worker_id = $1
  AND released_at IS NULL
  AND last_heartbeat_at > NOW() - INTERVAL '90 seconds';

-- 2. Se active >= MEZ_MAX_ACTIVE_TENANTS (100), recusar
-- 3. Senão, tentar pg_try_advisory_lock(hashtext(T || ':' || J))
-- 4. Se ganhou, INSERT em coordinator_session_claims
```

- **Least-loaded**: cada worker tem budget `MEZ_MAX_ACTIVE_TENANTS=100` (carryover Fase 4). Workers com < 100 ativos preferem novas sessões.
- **No tie-break**: se múltiplos workers querem a mesma sessão, `pg_try_advisory_lock` é a arbitragem final (apenas 1 vence).
- **Anti-affinity opcional**: spec Helm `nodeAffinity` para que workers whatsmeow caiam em nodes diferentes (K8s `topologySpreadConstraints`). Não é código — é deployment.
- **Cold start**: ao subir, worker consulta `coordinator_session_claims WHERE released_at IS NULL AND last_heartbeat_at < NOW() - 60s` (orphans) e tenta re-claim. Reconciler cobre orphans antes do steady-state.

#### Q10 — Métricas e observabilidade

**8 métricas novas** (vão para o `pkg/metrics/metrics.go` se a Fase 10 for executada):

| Métrica | Tipo | Labels | Uso |
|---|---|---|---|
| `whatsmeow_session_claim_total` | Counter | `result={success,contention,refused,error}` | Taxa de claims bem/mal-sucedidos |
| `whatsmeow_session_active` | Gauge | `worker_id` | Sessões ativas por worker (HPA signal) |
| `whatsmeow_lease_renewal_total` | Counter | `result={success,failed,timeout}` | Saúde do heartbeat |
| `whatsmeow_lease_lost_total` | Counter | `reason={disconnect,timeout,orphan}` | Sessões perdidas |
| `whatsmeow_heartbeat_duration_seconds` | Histogram | — | Latência do heartbeat (SLO < 50ms) |
| `whatsmeow_session_migration_total` | Counter | `direction={in,out}` | Sessões que migraram entre workers |
| `whatsmeow_session_orphan_detected_total` | Counter | `reason={heartbeat_lost,lease_expired}` | Órfãos detectados pelo reconciler |
| `coordinator_role_advertised` | Gauge | `role,version` | Roles ativas no cluster |

**3 alertas Prometheus** (vão para `deployments/prometheus/alerts.yaml`):

```yaml
- alert: WhatsmeowLeaseLostSpike
  expr: rate(whatsmeow_lease_lost_total[5m]) > 1
  for: 2m
  labels: { severity: warning }
  annotations:
    summary: "Sessões whatsmeow perdendo lease"
    runbook: "https://wiki/runbooks/whatsmeow-lease"

- alert: WhatsmeowOrphanDetected
  expr: increase(whatsmeow_session_orphan_detected_total[10m]) > 0
  labels: { severity: critical }
  annotations:
    summary: "Sessão whatsmeow órfã detectada"
    runbook: "https://wiki/runbooks/whatsmeow-orphan"

- alert: CoordinatorRoleMismatch
  expr: count(coordinator_role_advertised{role="whatsmeow-worker"}) < 2
  for: 5m
  labels: { severity: warning }
  annotations:
    summary: "Apenas {{ $value }} whatsmeow-worker ativo; HA em risco"
```

**Distributed tracing (opcional):** o já-herdado `bus.Bus` adiciona `trace_id` ao envelope (Fase 2 carryover). Workers whatsmeow propagam para o `*whatsmeow.Client.Logf` se OTEL exporter estiver configurado. Não é código novo — é configuração.

### 11.3 Tabela comparativa de 6 abordagens × 15 critérios

| Critério | Monolito single-image (status quo) | Monolito + leader election | Sharding de sessões (estático) | Sticky sessions no LB | Microserviço whatsmeow dedicado | Fila/eventos distribuídos (NATS/Redis) |
|---|---|---|---|---|---|---|
| **Escalabilidade Horizontal** | ❌ 1 instância | ⚠️ 2 (active+standby) | ✓ N shards | ⚠️ 2-3 (LB-dependent) | ✓✓ independente | ✓✓∞ |
| **Performance** | ✓✓ baseline | ✓ (overhead de election) | ✓ (hash routing) | ✓ (LB overhead) | ⚠️ +1 hop HTTP/gRPC | ⚠️ +1 hop broker |
| **Latência** | ✓✓ < 5ms local | ✓ < 10ms | ✓ < 10ms | ⚠️ 10-30ms LB | ⚠️ 20-50ms cross-pod | ⚠️ 5-15ms broker |
| **Complexidade** | ✓✓ mínima | ⚠️ election state | ⚠️ hash + rebalance | ⚠️ LB config | ❌ 2 Helm charts, 2 deploys | ❌ broker + clients |
| **Facilidade de Implementação** | ✓✓ já feito | ⚠️ 1 sprint | ⚠️ 2 sprints | ✓ 0.5 sprint | ❌ 3-4 sprints | ❌ 4-6 sprints |
| **Manutenção** | ✓✓ trivial | ✓ moderada | ⚠️ rebalance ops | ✓ moderada | ❌ 2 codebases | ❌ broker upgrades |
| **Alta Disponibilidade** | ❌ SPOF | ⚠️ failover 30-60s | ✓ shard rebalance | ⚠️ LB SPOF | ✓✓ HPA independente | ✓✓∞ broker HA |
| **Tolerância a Falhas** | ❌ 1 ponto | ⚠️ split-brain risk | ✓ parcial | ⚠️ LB-dependent | ✓ isolado | ✓ broker HA |
| **Consumo de Recursos** | ✓ 1× binário | ✓ 1× + standby | ⚠️ 1× + routing | ⚠️ 1× + LB | ❌ 2× binários | ❌ 2× + broker |
| **Custo Operacional** | ✓✓ mínimo | ✓ baixo | ⚠️ moderado | ⚠️ moderado | ❌ alto (2 deploys) | ❌ alto (broker ops) |
| **Facilidade de Deploy** | ✓✓ 1 image | ✓ 1 image + election | ✓ 1 image + config | ⚠️ LB config | ❌ 2 images | ❌ 2 images + broker |
| **Facilidade de Debug** | ✓✓ mesma máquina | ✓ logs centralizados | ⚠️ distributed logs | ⚠️ LB logs | ❌ cross-pod traces | ❌ broker traces |
| **Risco de Race Conditions** | ✓✓ in-process locks | ⚠️ election race | ⚠️ rebalance race | ⚠️ LB race | ✓ isolado | ✓ broker arbitrates |
| **Consistência das Sessões** | ✓✓ única fonte | ✓ se election OK | ⚠️ eventual | ⚠️ eventual | ✓ forte | ✓ forte |
| **Melhor cenário de uso** | dev, single-tenant | low-volume HA | multi-tenant médio | LB já existe | multi-tenant alto | event-driven extremo |

**Legenda:** ✓✓ excelente · ✓ bom · ⚠️ aceitável com cuidado · ❌ não-recomendado.

### 11.4 Arquitetura proposta

#### 11.4.1 Componentes novos (referência Fase 10+)

| Componente | Caminho | LOC estimado | Issue | ADR |
|---|---|---:|---|---|
| `coordinator/environment.go` | `internal/adapter/coordinator/environment.go` | ~120 | #177 | ADR-0030 |
| `coordinator/registry.go` | `internal/adapter/coordinator/registry.go` | ~200 | #177 | ADR-0030 |
| `coordinator/claim.go` | `internal/adapter/coordinator/claim.go` | ~280 | #178 | ADR-0031 |
| `coordinator/lease.go` | `internal/adapter/coordinator/lease.go` | ~240 | #179 | ADR-0031 |
| `coordinator/migrate.go` | `internal/adapter/coordinator/migrate.go` | ~180 | #180 | ADR-0031 |
| `coordinator/orphan.go` | `internal/adapter/coordinator/orphan.go` | ~150 | #180 | ADR-0031 |
| Migration `0010_coordinator.up.sql` | `migrations/` | ~80 | #177-#180 | ADR-0030/0031 |
| Tests (unit + integration) | `internal/adapter/coordinator/*_test.go` | ~550 | #177-#180 | — |
| **Total** | — | **~1.800 LOC** | — | — |

**Esforço total: 4-5 dias solo** (não-incluído no budget de 22d da Fase 9).

#### 11.4.2 Diagrama ASCII — Visão geral multi-replica

```
                    ┌─────────────────────────────────────────────────────────────┐
                    │                KUBERNETES CLUSTER (or Compose)              │
                    │                                                             │
   Internet ──────► │   ┌──────────────┐                                           │
   (Meta, TG,      │   │   Ingress /  │   TLS termination, rate limit per-IP     │
    clients)       │   │  LoadBalancer│                                           │
                    │   └──────┬───────┘                                           │
                    │          │                                                   │
                    │   ┌──────▼───────────────────────────────────────────────┐  │
                    │   │          API Gateway pods (role=gateway)              │  │
                    │   │   ┌────────┐ ┌────────┐ ┌────────┐  (HPA: 2-10 pods) │  │
                    │   │   │ gw-1   │ │ gw-2   │ │ gw-3   │  stateless        │  │
                    │   │   └───┬────┘ └───┬────┘ └───┬────┘                   │  │
                    │   └───────┼──────────┼──────────┼────────────────────────┘  │
                    │           │          │          │                            │
                    │           └──────────┼──────────┘                            │
                    │                      │                                       │
                    │   ┌──────────────────▼──────────────────────────────────┐    │
                    │   │   Relay pods (role=relay)                          │    │
                    │   │   ┌────────┐ ┌────────┐  stateless, competes via   │    │
                    │   │   │ rly-1  │ │ rly-2  │  FOR UPDATE SKIP LOCKED    │    │
                    │   │   └────┬───┘ └────┬───┘                            │    │
                    │   └────────┼──────────┼────────────────────────────────┘    │
                    │            │          │                                      │
                    │            ▼          ▼                                      │
                    │   ┌─────────────────────────────────────────────────────┐    │
                    │   │  Whatsmeow Worker pods (role=whatsmeow-worker)     │    │
                    │   │   ┌─────────────┐ ┌─────────────┐ ┌─────────────┐  │    │
                    │   │   │  wmw-1      │ │  wmw-2      │ │  wmw-3      │  │    │
                    │   │   │             │ │             │ │             │  │    │
                    │   │   │ [T1:J1]     │ │ [T2:J1]     │ │ [T1:J2]     │  │    │
                    │   │   │ [T3:J1]     │ │ [T3:J2]     │ │ [T4:J1]     │  │    │
                    │   │   │             │ │ [T5:J1]     │ │             │  │    │
                    │   │   │ ← 100 max → │ ← 100 max → │ ← 100 max → │  │    │
                    │   │   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘  │    │
                    │   └──────────┼───────────────┼───────────────┼─────────┘    │
                    │              │               │               │              │
                    │              └───────────────┼───────────────┘              │
                    │                              │                              │
                    │              ┌───────────────▼────────────────┐             │
                    │              │      WhatsApp Network          │             │
                    │              │      (WebSocket per session)   │             │
                    │              └────────────────────────────────┘             │
                    │                                                             │
                    │   ┌────────────────────────────────────────────────────┐    │
                    │   │   Postgres (HA: primary + 2 standbys)              │    │
                    │   │   ├── outbound_events (outbox + scheduled)         │    │
                    │   │   ├── channel_credentials (envelope)               │    │
                    │   │   ├── webhook_secrets (envelope)                  │    │
                    │   │   ├── coordinator_capabilities (NEW)               │    │
                    │   │   ├── coordinator_session_claims (NEW)             │    │
                    │   │   └── admin_audit_log (immutable)                 │    │
                    │   └────────────────────────────────────────────────────┘    │
                    │                                                             │
                    └─────────────────────────────────────────────────────────────┘
```

#### 11.4.3 Fluxo — Sessão whatsmeow (claim + uso)

```
(1) Pod wmw-2 sobe
    │   capability_registry.advertise({role: "whatsmeow-worker", max: 100, version: "v9.0.0"})
    │
(2) Tenant T1 envia primeiro evento (webhook WABA → gw-1 → outbox → rly-1)
    │   rly-1 precisa enviar reply via whatsmeow. Consulta:
    │   SELECT worker_id FROM coordinator_session_claims WHERE session_key = hashtext('T1:J1')
    │   WHERE released_at IS NULL AND last_heartbeat_at > NOW() - INTERVAL '60s'
    │
(3) Resultado: NULL (sessão não tem owner). rly-1 PUBLICA em bus:
    │   bus.Publish("coordinator.claim.requested", {tenant: T1, jid: J1})
    │
(4) Todos os wmw-* recebem. Cada um testa:
    │   pg_try_advisory_lock(hashtext('T1:J1'))  -- non-blocking
    │   AND active_count < MEZ_MAX_ACTIVE_TENANTS
    │
(5) Apenas wmw-2 ganha. Ele:
    │   - INSERT INTO coordinator_session_claims (worker_id, session_key, claimed_at, last_heartbeat_at)
    │   - Manager.GetOrCreate(T1, J1) → carrega IdentityStore, abre WebSocket
    │   - Inicia heartbeat goroutine (renova a cada 20s)
    │
(6) rly-1 faz poll (5s) e descobre owner = wmw-2. Envia via:
    │   HTTP POST http://wmw-2:8080/internal/send  (service mesh ou ClusterIP)
    │   wmw-2 valida claim, executa sender.Send, retorna resultado
    │
(7) Heartbeat:
    │   A cada 20s: UPDATE coordinator_session_claims SET last_heartbeat_at = NOW() WHERE worker_id = 'wmw-2' AND session_key = ...
    │   A cada 60s: pg_try_advisory_lock(hashtext('T1:J1')) -- re-assert (PG pode ter recycled backend)
    │
(8) Pod wmw-2 falha (kill -9):
    │   T+0s: TCP RST detectado pelo PG
    │   T+5s: pgx pool remove backend; pg_advisory_unlock_all() implícito
    │   T+10s: wmw-1 ou wmw-3 ganha claim no próximo tick; sessão migra
    │   T+30s: wmw-X chama Manager.GetOrCreate; IdentityStore recarrega; reconecta
    │   T+30-60s: mensagens em-flight cobertas pelo reconciler C1
```

#### 11.4.4 Fluxo — Canal stateless (WABA, IG, MSG, TG)

```
(1) Cliente HTTP → Ingress → gw-1
    │   gw-1 aplica: rate limit per-IP, auth (JWT), routing
    │
(2) gw-1 identifica tenant via JWT claim; valida RLS via RunInTenantTx
    │   Qualquer pod gw-* pode atender (stateless). HPA escala gw-* por CPU/RPS.
    │
(3) gw-1 enfileira em outbound_events (mesma tx de business logic)
    │   INSERT INTO outbound_events (...) VALUES (...)
    │   (status='pending', scheduled_at=NULL or future, next_attempt_at=NULL)
    │
(4) rly-1 ou rly-2 compete por SKIP LOCKED → processa → sender.Send
    │   Se WABA: HTTP POST graph.facebook.com/v18.0/<phone_id>/messages
    │   Se TG: HTTP POST api.telegram.org/bot<token>/sendMessage
    │   (qualquer pod rly-* pode processar; stateless)
    │
(5) Response → outbox.MarkSent ou MarkFailed (com #161 backoff)
    │   Métricas + audit row
```

**Conclusão:** canais stateless **não precisam** de coordinator; só whatsmeow precisa.

#### 11.4.5 Trade-offs aceitos

**5 prós:**
- ✓ Zero infra nova (Postgres já existe; advisory lock é built-in).
- ✓ Mesma imagem Docker; mesmo Helm chart parametrizado.
- ✓ Single-replica continua suportado (mode `role=all`); migração progressiva.
- ✓ Canais stateless (4 dos 5) escalam **independentemente** do whatsmeow — HPA separado.
- ✓ Failover automático em ≤ 60s (lease TTL); reconciler reduz para 30s.

**5 contras:**
- ✗ Postgres vira SPOF lógico (mitigado por replicação streaming + RPO ≤ 5s).
- ✗ Latência de claim 1-5ms vs 0.1ms Redis (irrelevante para o caminho hot).
- ✗ Coordinator adiciona ~1.800 LOC de código novo (Fase 10+).
- ✗ Helm chart mais complexo (3 roles parametrizadas; não 1).
- ✗ Debug cross-pod exige log aggregation (Loki/ELK) — não-incluso.

**3 riscos residuais:**

| Risco | Probabilidade | Impacto | Mitigação |
|---|---|---|---|
| **R-coord-1**: `pg_try_advisory_lock` tem comportamento indefinido em PGBouncer transaction-pooling | Média | Alto | Forçar `session` mode para conexões de coordinator; documentar em `deployments/helm/values.yaml` |
| **R-coord-2**: Thundering herd de claim quando muitos pods disputam | Baixa | Médio | Backoff exponencial no `coordinator/claim.go` (mesma matemática do #161 jitter ±5s) |
| **R-coord-3**: Heartbeat storm com 100 pods × 100 sessões × 1/20s = 500 qps no PG | Baixa | Médio | Batch heartbeat: `UPDATE ... WHERE worker_id = $1 AND session_key = ANY($2)` 1x/20s |

### 11.5 Critério de aceitação da Seção 11 (DoD para Fase 10+)

- [ ] Migration `0010_coordinator.up.sql` aplicável (capabilities + session_claims tables com RLS FORCE).
- [ ] `internal/adapter/coordinator/{environment,registry,claim,lease,migrate,orphan}.go` implementados.
- [ ] `pg_try_advisory_lock` coberto por `TestClaim_Contended` (10 goroutines, 1 vence).
- [ ] `TestLease_Renewal` (heartbeat 3x, lock persiste); `TestLease_Lost` (PG disconnect simulado, lock liberado em ≤ 5s).
- [ ] `TestOrphan_Detection` (worker simulado sem heartbeat 90s, reconciler marca orphan e libera).
- [ ] `TestMigration_Graceful` (sessão migra de wmw-1 para wmw-2 sem perder mensagens; reconciler cobre o gap).
- [ ] Métricas Prometheus 8/8 exportadas; 3/3 alertas válidos (`promtool check alerts`).
- [ ] Helm chart com 3 roles parametrizadas; `helm template` verde.
- [ ] Chaos test: kill -9 do whatsmeow-worker owner → sessão recupera em ≤ 90s.
- [ ] Documentation: `docs/fase10/PLAN.md` (a ser criado) referencia esta seção como ponto de partida.

---

## 12. Sprint 0 — Auditoria de segurança (carryover Fase 8) · **pré-requisito**

> **Status:** planejamento · junho/2026 · **pré-requisito dos Sprints 1–5**.
> **Escopo:** 6 CRITICAL + 9 HIGH (excluindo 1 duplicata) + 3 MEDIUM (excluindo 1 duplicata) + 2 housekeeping (fechar issues já mergeadas em `main`) = **20 issues · ~9-11d solo** (3 sub-sprints) · single commit (squash) por sub-sprint → `main`.
> **Origem:** `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (auditoria STRIDE+DREAD 5-domínios) — 10 CRITICAL + 15 HIGH + 15 MEDIUM no total; **9 já mergeadas em `main` via `bdee3cd` (PR #108)** sem `Closes` propagado; **20 ainda abertas** (detalhadas abaixo).
> **Justificativa como pré-requisito (não-execução após Fase 9):** várias issues da Fase 9 (A1, B1, B3, B6, C5) **herdam** os furos que este Sprint 0 fecha — em particular, IDOR via `RunInTenantTx` (#133) é fundação de qualquer handler novo, e admin authorization (#132) é fundação de qualquer endpoint admin novo (#169, #174). Adiar para Fase 10+ reintroduz as mesmas vulnerabilidades nos handlers novos. **Regra:** Sprint 0 não pode ser pulado, mesclado ou postergado.
> **ADRs novos:** 3 (0041-0043 — FailClosed-by-default, Principal Hydration, Defense-in-Depth RLS).

### 12.0 Housekeeping — Fechar issues já corrigidas em `main` (0.2d)

A squash-merge `bdee3cd` (PR #108, `feat(fase8): merge security audit + process infra`) trouxe 9 fixes de segurança para `main`, mas o `Closes` no body da PR só listou as issues de tracking (`#99..#107`). As 9 issues de segurança referenciadas nos commits (`feat(security #N)`) **continuam abertas** no GitHub. Ações:

| Issue | Commit do fix | Ação |
|------:|---|---|
| #129 | `cc08aa9` WebSocket CheckOrigin config-driven | Comentar com `✅ Merged in bdee3cd (commit cc08aa9) — closing.` + label `phase8-security` + fechar |
| #130 | `38368f4` JWT exp validation | Comentar + fechar |
| #134 | `a6ee296` Actor de backup via JWT | Comentar + fechar |
| #136 | `bcbb880` Webhook body nunca vai para log | Comentar + fechar |
| #139 | `aba5b9b` Sanitize OIDC next | Comentar + fechar |
| #141 | `05d6d7a` Master key file 0600 | Comentar + fechar |
| #144 | `38368f4` APIJWTSecret length>=32 | Comentar + fechar |
| #152 | `5fdc0b7` ReadHeaderTimeout 5s | Comentar + fechar |
| #156 | `4177bf2` Lockout off-by-one | Comentar + fechar |

**Comando (automatizável):**
```bash
for n in 129 130 134 136 139 141 144 152 156; do
  gh issue comment "$n" --repo felipedsvit/mez-go-mono --body "✅ Already merged in \`bdee3cd\` (PR #108). Commit: \`$(git log --format=%H --grep="security #$n" main | head -1)\`. Closing as completed."
  gh issue close "$n" --repo felipedsvit/mez-go-mono --reason completed
done
```

**Esforço:** 0.2d · **Pré-requisito:** nenhum.

### 12.1 Sub-sprint 0A — Security CRITICAL (3-4d)

> **DoD da seção 0A:** todas as 6 CRITICAL fechadas; 0 findings CRITICAL remanescentes na auditoria; testes de regressão para IDOR/auth/role-editor passam.

#### #131 — S0-C3: Cookie `__Host-mez_admin` sem `Secure: true`

**Arquivos:** `cmd/server/wire.go:313` (nome) + `internal/transport/adminweb/handlers_auth.go:81-89, 151-159` (emissão sem `Secure`)

**Diagnóstico:** prefixo `__Host-` (RFC 6265bis) **exige** `Secure=true`. Browsers modernos ou rejeitam o cookie (auth quebra) ou aceitam em cleartext (sessão sniffável em WiFi de café). ADR 0018 documenta a intenção, mas o código contradiz.

**Ação:**
1. Adicionar `Secure bool` ao struct de config de sessão (`pkg/config/config.go`), lido de `MEZ_SESSION_COOKIE_SECURE` (default `true` em qualquer build não-dev).
2. Plumar `Secure` no `handlers_auth.go:81-89` e `151-159` (emissão + refresh).
3. Honrar `X-Forwarded-Proto` de proxy confiável: se header presente e `https`, setar `Secure=true` independente da config (com `MEZ_TRUSTED_PROXY_CIDR` allowlist).
4. Boot do `cmd/server/serve`: warning explícito se `Secure=false` em build não-dev (log em `WARN`).
5. Test: `httptest.NewRecorder` + `http.SetCookie` com `__Host-` rejeita se `Secure=false` no Chromium; validar via `cookie.Valid()` (stdlib).

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

#### #132 — S0-C4: Admin handlers — autenticação sem autorização

**Arquivos:** `internal/transport/adminweb/handlers_users.go:11-87`, `handlers_tenants.go:11-91`, `handlers_roles.go:11-73`, `handlers_audit.go:10-25`, `handlers_backup.go:28-167`, `handlers_reset.go:23-69`

**Diagnóstico:** cada handler chama `principalOrEmpty(r)` mas **nenhum chama `admin.Evaluate(principal, perm, scope)`**. Apenas `RequireAuth` verifica sessão; `Principal.Permissions` e `Principal.Roles` são sempre `nil`. Resultado: qualquer tenant owner de A suspende/edita qualquer tenant/user/role de B.

**Ação:**
1. Estender `Principal` em `internal/core/port/auth.go`: campos `Permissions []Permission` e `Roles []Role` (preenchidos no hydration do session).
2. Modificar o **session middleware** (`internal/transport/http/middleware/session.go:42-58`): após carregar `principal.UserID`/`Email`, fazer `roleBindingRepo.ListByUser(ctx, principal.UserID)` → carregar `roles[i].Permissions` → hidratar `Principal.Permissions`. Adicionar TTL 5min em cache in-memory (`sync.Map`) para evitar N+1.
3. Criar helper `admin.RequireScope(perm Permission, scope Scope) func(http.Handler) http.Handler` em `internal/transport/adminweb/middleware.go` (NOVO). Usa `admin.Evaluate(principal, perm, scope)` (já existe, só não era chamado).
4. Aplicar em **todos** os 6 arquivos: `RequireScope(admin.PermUserManage, admin.ScopePlatform)` para users/tenants cross-tenant; `RequireScope(admin.PermBackupRun, tenantScope)` para backup; etc.
5. Audit row por negação: `auth.denied{actor, perm, scope, reason}`.
6. Test de regressão: tenant A owner tenta `DELETE /admin/tenants/{B}/users` → 403 com audit row; `TestAdmin_Authorization` cobre 6 handlers × 2 cenários (allow/deny) = 12 sub-tests.

**Esforço:** 1.0d · **REWRITE** · **Bloqueado por:** nenhum · **Pré-requisito para:** #169, #174 (Sprint 4/5) e qualquer handler admin novo.

#### #133 — S0-C5: IDOR API REST — handlers não usam `RunInTenantTx`

**Arquivos:** `internal/transport/http/api/handlers.go:89-452`; `internal/transport/http/middleware/bearer.go:62-115`

**Diagnóstico:** o doc-comment em `handlers.go:16` diz "RLS via RunInTenantTx (claim tenant_id do token)" mas nenhum handler chama `txRunner.RunInTenantTx`. Eles chamam `h.convRepo.ListByTenant`, `h.msgRepo.Get`, `h.convRepo.Upsert` direto. O `appQFromCtx` fallback (`db.go:29-34`) usa o pool raw quando não há tx no ctx. **Fundação de qualquer handler novo da Fase 9.**

**Ação:**
1. Refatorar `internal/transport/http/api/handlers.go` para que **toda função handler** seja embrulhada em `txRunner.RunInTenantTx(ctx, jwtTenantID, func(txCtx) { ... })`. Extrair helper `withTenantTx(h *Handlers, perm Permission) func(http.Handler) http.Handler` que (a) extrai `tenantID` do JWT, (b) abre tx, (c) injeta no `r.Context()`, (d) defer rollback.
2. Eliminar `appQFromCtx` fallback em `db.go:29-34` (forçar erro se chamado sem tx no ctx). Marcar como `// Deprecated: deve ser substituído por appQFromCtxOrPool` (carryover do DDD-hex 3.11).
3. Audit row por negação cross-tenant (se handler tentar query com `tenantID != ctxTenantID`): `idor.attempted{actor_tenant, target_tenant, endpoint}`.
4. Test de regressão: 12 cenários cruzados (tenant A chama endpoint com `tenantID=B` no body/header) → 403/404 em todos. `TestAPI_IDOR_Matrix` cobre `ListMessages`/`PostReaction`/`PatchMessage`/`DeleteMessage`/`ConvAssign`/`ConvResolve` × 2 tenants = 12 sub-tests.
5. Mover `permitira` de `handlers.go` para `withTenantTx` (centralizar).

**Esforço:** 1.0d · **REWRITE** · **Bloqueado por:** nenhum · **Pré-requisito para:** #162, #165, #166, #167, #176 (todos os handlers novos).

#### #135 — S0-C7: Privilege escalation via role editor

**Arquivos:** `internal/transport/adminweb/handlers_roles.go:11-73`; `internal/usecase/admin/role_service.go:78-145`

**Diagnóstico:** o endpoint de edição de roles permite que um admin crie/edite role com `Scope=Platform` sem verificar permissões. Um `tenant_owner` A consegue criar role `Platform:Super` e dar para si mesmo.

**Ação:**
1. Em `role_service.Create/Update`: antes de persistir, chamar `admin.Evaluate(caller, admin.PermRoleManage, role.Scope)`. Se `caller.Scope == Tenant` e `role.Scope == Platform` → 403.
2. Audit row por tentativa: `role.escalation.blocked{actor, role_name, attempted_scope}`.
3. Validar invariante no DB: `CHECK (scope IN ('platform', 'tenant'))` + trigger que rejeita INSERT/UPDATE se `caller` não tem `PermRoleManage` no scope alvo (defense-in-depth).
4. Migration `0011_role_scope_check.up.sql`: adicionar CHECK constraint + trigger (cobre race entre check e INSERT).
5. Test: `tenant_owner` A tenta criar role `Platform:Super` → 403 + audit; `platform_admin` cria mesma role → 200.

**Esforço:** 0.5d · **REWRITE** · **Bloqueado por:** #132 (precisa de `Evaluate` funcionando).

#### #137 — S0-C9: Backup restore aceita `_table` arbitrário (defense-in-depth SQLi)

**Arquivos:** `internal/usecase/backup/restore.go:84-132`; `internal/adapter/repository/postgres/restore_repo.go:45-78`

**Diagnóstico:** `restore.go:84-132` aceita o nome da tabela de um manifest externo. Embora o código atual use um whitelist hardcoded, **defense-in-depth** exige rejeitar nomes fora de uma allowlist explícita (não enumerar dinamicamente do `INFORMATION_SCHEMA`). CWE-89 (SQLi via column/table name).

**Ação:**
1. Criar `var allowedRestoreTables = map[string]bool{"messages": true, "conversations": true, "contacts": true, "outbound_events": true, "audit_log": true}` em `restore.go:30` (constante).
2. Em `restore.go:91`, **rejeitar** se `!allowedRestoreTables[tableName]` com erro `ErrInvalidRestoreTable`.
3. Validar cada coluna em `restore_repo.go:45-78` da mesma forma: allowlist por tabela, **sem** uso de reflection ou string interpolation para column list.
4. Audit: `backup.restore.invalid_table{table, manifest_id, actor}`.
5. Test: manifest forjado com `table: "users"` ou `table: "channel_credentials"` → rejeitado; manifest válido → OK.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

#### #138 — S0-C10: S3 keys/prefixos sem validar `tenantID` (path confusion)

**Arquivos:** `internal/adapter/storage/s3/s3.go:34-67, 89-118`; `internal/adapter/storage/s3/multipart.go:18-49`

**Diagnóstico:** `Put(tenantID, key, data)` aceita `tenantID` e `key` separados. Um handler com `tenantID=A` mas `key="tenants/B/media/..."` consegue escrever no prefixo do tenant B. **Path confusion cross-tenant.**

**Ação:**
1. Em `s3.go:34-67`: forçar `fullKey = "tenants/" + tenantID + "/" + key` e validar que `strings.HasPrefix(fullKey, "tenants/"+tenantID+"/")` **antes** de assinar a request. Rejeitar com `ErrTenantMismatch` se não bater.
2. Idem para `Get`, `Delete`, `DeletePrefix`, `UploadStream` em todos os 4 arquivos.
3. Adicionar métrica `s3_tenant_mismatch_total{operation}` Counter.
4. Audit: `s3.tenant_mismatch{actor_tenant, attempted_prefix, operation}`.
5. Test: tenant A chama `Put(tenantA, "../../tenants/B/media/x.png", ...)` → rejeitado; `Put(tenantA, "media/x.png", ...)` → grava em `tenants/A/media/x.png`.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

**Total Sub-sprint 0A: ~3.4d · 6 issues CRITICAL**

### 12.2 Sub-sprint 0B — Security HIGH (4-5d)

> **DoD da seção 0B:** todas as 9 HIGH (excluindo #147 se confirmado duplicata) fechadas; falhas de OIDC nonce / JWT entropy / CSRF setup / TLS / concurrency cobertas por testes de regressão.

#### #140 — S0-H2: OIDC `nonce` não validado (replay de ID-token)

**Arquivos:** `internal/adapter/idp/oidc/oidc.go:47, 65-79`; `internal/usecase/auth/login.go:160-189`; `internal/adapter/idp/oidc/verifier.go:28-30`

**Diagnóstico:** o callback OIDC não passa `nonce` para `gooidc.Verifier.Verify`, permitindo replay de ID-token capturado. (Este é o fix canônico; **simplifica** a #166 do Sprint 3 — após este fix, a issue #166 vira apenas "persistir refresh token".)

**Ação:**
1. Em `login.go:160-189`: gerar `nonce := base64.RawURLEncoding.EncodeToString(randBytes(16))`, persistir no `OIDCState` (adicionar campo `Nonce` no struct), incluir no `AuthCodeURL` via `oauth2.SetAuthURLParam("nonce", nonce)`.
2. Em `login.go` callback: extrair `nonce` do state, passar para `Verifier.Verify(ctx, rawIDToken, gooidc.VerifyNonce(state.Nonce))`.
3. `verifier.go:28-30`: alterar `Config{ClientID: cfg.ClientID}` para incluir `SupportedSigningAlgs: []string{"RS256"}` (forçar assimétrico; bloqueia HS256 confusion).
4. Audit row por rejeição: `oidc.nonce.mismatch{state_id, ip}`.
5. Test: state com `nonce=X` mas ID-token com `nonce=Y` → 401 + audit; state sem nonce + token sem nonce → 401 (rejeitar `nonce=""` em prod, aceitar em dev via flag `MEZ_OIDC_REQUIRE_NONCE`).
6. **Sincronizar com #166** (Sprint 3): #140 cobre validação; #166 cobre persistência de `refresh_token` + criação de `oidc_tokens` table.

**Esforço:** 0.5d · **FIX** · **Bloqueado por:** nenhum · **Pré-requisito para:** #166 (Sprint 3 fica mais simples).

#### #142 — S0-H6b: JWT secret sem check de length/entropy

**Arquivos:** `internal/transport/http/server/server.go:78-95` (já tem check de `SessionSecret` ≥ 32, falta `APIJWTSecret`); `pkg/config/config.go:140-156`

**Diagnóstico:** #144 (H6 canônico, "APIJWTSecret length>=32") já foi mergeado em `38368f4`. **#142 é uma issue separada** sobre entropy check (não apenas length) — rejeitar segredos com baixa entropia estimada (muitos chars repetidos, all-ASCII-lowercase, etc.).

**Ação:**
1. Em `server.go:78-95`: além do length check de `APIJWTSecret`, calcular Shannon entropy do secret e rejeitar se `< 3.5` (threshold empírico para segredos aleatórios de 32+ chars).
2. Helper `pkg/config/entropy.go` (NOVO) com `ShannonEntropy(s string) float64` + testes table-driven (casos: empty, all-same, low-entropy, high-entropy).
3. Audit: `config.low_entropy{secret_name, length, entropy}` (em dev, log warning; em prod, refuse boot).
4. Boot check: refuse com erro fatal se `entropy < 3.5 && !devMode`.
5. Test: `MEZ_API_JWT_SECRET=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa` (32 chars, entropy=0) → refuse; `MEZ_API_JWT_SECRET=$(openssl rand -base64 32)` → OK.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

#### #143 — S0-H14b: `ReadHeaderTimeout=0` no `http.Server` (slow-loris)

**Arquivos:** `cmd/server/wire.go:118-134`; `cmd/server/main.go:42-58`

**Diagnóstico:** #152 (H14 canônico: `ReadHeaderTimeout=5s + MaxHeaderBytes=1MiB`) já foi mergeado em `5fdc0b7`. **#143 é uma issue separada** sobre o fallback `0` quando o TLS-terminating proxy não envia `X-Forwarded-Proto` confiável — preciso garantir que o `0` nunca acontece mesmo se o env for unset.

**Ação:**
1. Em `wire.go:118-134`: se `cfg.HTTP.ReadHeaderTimeout == 0` (unset), aplicar default `5s` antes de criar `http.Server`. Idem para `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
2. Helper `pkg/netutil/safeServer(s *http.Server) *http.Server` (NOVO) que normaliza timeouts.
3. Boot check: refuse se `ReadHeaderTimeout=0 && !devMode` (CWE-400 defense-in-depth).
4. Test: `cmd/server serve` com `MEZ_HTTP_READ_HEADER_TIMEOUT=` (vazio) → 5s aplicado; com `=0` → refuse.
5. **Diferenciar de #152:** #152 garante valores positivos no server real; #143 garante que o server é criado com defaults seguros (sem race de "esqueceu de setar").

**Esforço:** 0.2d · **FIX** · **Bloqueado por:** nenhum.

#### #145 — S0-H7: CSRF `/setup` POST sem validação

**Arquivos:** `internal/transport/http/api/handlers_setup.go:38-92`; `internal/transport/http/middleware/csrf.go:18-44`

**Diagnóstico:** o middleware CSRF existe e está wired em `/admin/*` (Fase 5), mas o endpoint `POST /api/setup` (executado **antes** do setup wizard completar) não passa pelo middleware. Atacante com acesso ao DNS (mesmo LAN) pode forçar re-setup do tenant.

**Ação:**
1. Plumar middleware CSRF em `handlers_setup.go:38`: antes do `decodeJSON`, validar token via `csrf.VerifyToken(r)`.
2. Para `/setup` (primeira execução, sem cookie ainda): gerar token via `csrf.NewToken()`, setar em cookie `__Host-mez_csrf_setup` (com `Secure=true` + `SameSite=Strict`), exigir que o POST retorne o mesmo token no header `X-CSRF-Token`.
3. Audit: `setup.csrf.missing{ip, attempt}`.
4. Test: POST sem token → 403; POST com token mismatch → 403; POST com token válido → 200.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

#### #146 — S0-H13: Security headers sempre invocados com `secure=false`

**Arquivos:** `internal/transport/http/middleware/secure_headers.go:14-58`

**Diagnóstico:** o middleware `SecureHeaders` existe mas sempre é invocado com `secure=false` em `wire.go`. Resultado: `Strict-Transport-Security` (HSTS) nunca é emitido, `Set-Cookie` flags falham silenciosamente.

**Ação:**
1. Em `wire.go:118-134`: passar `secure: cfg.HTTP.TLS || cfg.HTTP.BehindTLSProxy` (true se direto TLS ou atrás de proxy confiável).
2. Helper `pkg/config.IsHTTPSActive(cfg) bool` que checa `cfg.HTTP.TLS || (cfg.HTTP.TrustedProxyCIDR != nil && r.Header.Get("X-Forwarded-Proto") == "https")`.
3. Test: `secure=true` → response tem `Strict-Transport-Security: max-age=31536000; includeSubDomains`; `secure=false` → header ausente.
4. Audit: `security_headers.disabled{reason}` em boot se `secure=false` em prod.

**Esforço:** 0.2d · **FIX** · **Bloqueado por:** #151 (mesma config infra).

#### #147 — S0-H2-dup: Investigar e fechar como `duplicate` se for clone de #140

**Diagnóstico:** pelo título e descrição breve, aparenta ser duplicata de #140 (H2 OIDC nonce). Validar abrindo o body e comparando; se idêntico, fechar como `duplicate` com referência `#140`.

**Ação:**
1. `gh issue view 147 --repo felipedsvit/mez-go-mono --json body,title,labels` para inspecionar.
2. Se CWE/file/descrição bater com #140 → `gh issue close 147 --reason duplicate --comment "Duplicata de #140 (H2 OIDC nonce)"`
3. Se diferente, promover para issue independente e re-agendar no Sprint 0B.

**Esforço:** 0.1d · **TRIAGE** · **Bloqueado por:** nenhum.

#### #148 — S0-H5: `RunAsPlatform` audit é best-effort, não atômico

**Arquivos:** `internal/usecase/platform/run_as_platform.go:34-78`; `internal/adapter/repository/postgres/platform_audit.go:18-44`

**Diagnóstico:** `RunAsPlatform(fn)` faz o trabalho + grava audit row em **txs separadas**. Se a tx de trabalho commita mas a audit row falha (deadlock, network blip), o audit é perdido. Conformidade SOC2/LGPD exige atomicidade.

**Ação:**
1. Em `run_as_platform.go:34-78`: refatorar para abrir **uma tx** que faz `SET LOCAL ROLE mez_platform` + executa `fn(txCtx)` + INSERT na `admin_audit_log` na mesma tx. Commit atômico.
2. Helper `RunAsPlatformTx(ctx, auditEntry, fn func(ctx) error) error` (renomear o atual para `RunAsPlatform` que mantém compat).
3. Migration `0012_audit_atomicity.up.sql` (opcional): adicionar `ON DELETE CASCADE` em `admin_audit_log.target_id` para cleanup.
4. Test: injectar falha no INSERT do audit (mock) → tx de trabalho também rola back; sucesso → ambos persistem.

**Esforço:** 0.5d · **FIX** · **Bloqueado por:** nenhum.

#### #149 — S0-H8+H9+H10: Concorrência — bus `UnsubscribeInbound` race, outbox claim race, drain TOCTOU

**Arquivos:** `internal/adapter/broker/bus.go:124-156` (`UnsubscribeInbound` usa `reflect.Pointer` que é unsafe); `internal/adapter/repository/postgres/outbox.go:131-187` (claim race); `internal/usecase/outbox/relay.go:154-199` (drain TOCTOU)

**Diagnóstico:** três issues de concorrência combinadas (H8 unsubscribe via `reflect.Pointer` é racy; H9 outbox claim — já parcialmente tratado por #159; H10 drain entre `MarkFailed` e persistência de `next_attempt_at`).

**Ação:**
1. **H8:** substituir `reflect.Pointer` em `bus.go:124-156` por um registry explícito de subscribers com `sync.Mutex` + map `chan ID → *Subscription`; `UnsubscribeInbound(id)` recebe o ID (não o ponteiro).
2. **H9:** (carryover para #159, mas a parte `BeginTx` é a única peça crítica — confirmar com o Sprint 1 #159).
3. **H10:** em `relay.go:154-199`, fazer `MarkFailed` em **uma única query** que atualiza `attempts`, `next_attempt_at`, `last_error` no mesmo statement. Eliminar o pattern read-modify-write.
4. Audit: `bus.unsubscribe.race{listener_id}` (apenas em dev, log warning; em prod é panic via `goleak`).
5. Test: 100 goroutines fazem `Subscribe/Unsubscribe` simultâneo → zero races (`go test -race`); 2 relays draining mesma row → 1 vê update, outra vê row já claimed (carryover #159).

**Esforço:** 1.0d · **REWRITE** · **Bloqueado por:** nenhum.

#### #150 — S0-H11: `labstack/echo` pulled por dead code (`api/openapi.gen.go`)

**Arquivos:** `api/openapi.gen.go` (gerado, contém import de `github.com/labstack/echo/v4` mas não usa); `go.mod`, `go.sum`

**Diagnóstico:** o gerador `oapi-codegen` injeta imports de Echo como boilerplate, mesmo quando os handlers usam `net/http` puro. Echo vira dep **transitiva** (atualiza em vuln scan, polui SBOM).

**Ação:**
1. Rodar `make openapi-gen` e inspecionar `api/openapi.gen.go`: se Echo for importado mas não usado, **regenerar com flag correta** (`--generate types,chi-server,spec` sem Echo-specific templates).
2. Alternativa: substituir Echo-specific server por chi-server (`oapi-codegen` suporta ambos). Ver ADR-0012 (já menciona chi).
3. Após regenerar, rodar `go mod tidy` + `govulncheck ./...` e confirmar que `github.com/labstack/echo` não aparece em nenhuma dependência.
4. Test de regressão: `grep -r "labstack/echo" .` retorna 0 hits.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** nenhum.

#### #151 — S0-H12: Sem TLS termination / sem redirect HTTP→HTTPS

**Arquivos:** `cmd/server/wire.go:118-134`; `pkg/config/config.go:140-180`

**Diagnóstico:** o binário não tem opção de TLS nativo (sempre espera proxy reverso terminar TLS) e não tem redirect HTTP→HTTPS. Em dev é OK; em prod, deploy sem proxy configurado = cleartext.

**Ação:**
1. Adicionar config `MEZ_HTTP_TLS_CERT_FILE` e `MEZ_HTTP_TLS_KEY_FILE` (paths). Se ambos setados, `http.Server` usa `ListenAndServeTLS` em vez de `ListenAndServe`.
2. Adicionar middleware `httpsRedirect` (NOVO em `internal/transport/http/middleware/`) que responde `301` em qualquer request HTTP se `cfg.HTTP.ForceHTTPS=true`. Wired apenas quando `cfg.HTTP.ForceHTTPS=true` (default `true` em prod, `false` em dev).
3. Documentar em `docs/deployment/HTTPS.md` (NOVO): "Recomendamos proxy reverso (Caddy/nginx) para TLS termination em prod. O binário suporta TLS nativo como fallback."
4. Test: `MEZ_HTTP_TLS_CERT_FILE=test.crt MEZ_HTTP_TLS_KEY_FILE=test.key` + `MEZ_HTTP_FORCE_HTTPS=true` → curl http://localhost:8080/ retorna 301 → https://...; https funciona.

**Esforço:** 0.5d · **FIX** · **Bloqueado por:** nenhum.

**Total Sub-sprint 0B: ~4.4d · 9 issues HIGH (excluindo #147 dup) + 1 triage**

### 12.3 Sub-sprint 0C — Security MEDIUM (1-2d)

> **DoD da seção 0C:** todas as 3 MEDIUM (excluindo #155 dup) fechadas; vazamento de erro interno eliminado; `role_id` usa UUID v7; audit query tem tenant filter default.

#### #153 — S0-M3: API error responses leak internal error strings

**Arquivos:** `internal/transport/http/api/errors.go:18-64`; `internal/transport/adminweb/handlers.go:78-92`

**Diagnóstico:** handlers retornam `err.Error()` direto no body do response. Atacante com tenant válido aprende schema do DB (`pq: relation "mez.users" does not exist`), paths internos, stack traces.

**Ação:**
1. Criar `internal/transport/http/api/errors.go` com `WriteError(w http.ResponseWriter, status int, code string, err error)` que:
   - Loga `err.Error()` server-side com `slog.Error`.
   - Retorna apenas `{"error": code, "request_id": "<uuid>"}` no body.
   - Audit row para 5xx (não 4xx) com `metadata={request_id, code}`.
2. Catálogo de codes: `code_not_found`, `code_validation`, `code_unauthorized`, `code_forbidden`, `code_internal`, `code_rate_limited`, `code_idor`, `code_tenant_mismatch`.
3. Substituir **todos** os `http.Error(w, err.Error(), ...)` por `WriteError(...)`.
4. Adicionar `request_id` middleware (NOVO) que gera UUID por request e injeta no `r.Context()`.
5. Test: `pq: relation ...` em `err.Error()` → response não contém "relation"; log server-side contém.

**Esforço:** 0.5d · **FIX** · **Bloqueado por:** nenhum.

#### #154 — S0-M8: Audit log query sem tenant filter default

**Arquivos:** `internal/adapter/repository/postgres/audit_query.go:18-67`; `internal/transport/adminweb/handlers_audit.go:10-25`

**Diagnóstico:** a query `ListAuditLog` aceita `tenantID` opcional. Se omitido, retorna **todos** os tenants. Admin tenant-level consegue ler audit de outros tenants (combinado com #132 IDOR admin).

**Ação:**
1. Em `audit_query.go:18-67`: exigir `tenantID` (não mais opcional). Se vier `nil/empty` E `caller.Scope == Tenant` → erro `ErrTenantRequired`.
2. Admin platform pode passar `nil` para listar cross-tenant (intencional); requer `Evaluate(principal, PermAuditRead, ScopePlatform)`.
3. Adicionar `LIMIT 1000` default (anti-DoS).
4. Audit: `audit.cross_tenant_read{actor, count_returned}` (apenas em prod, sampling 1%).
5. Test: `tenant_owner A` chama `GET /admin/audit?tenant_id=B` → 403; omite `tenant_id` → 403; `platform_admin` sem filtro → 200 + lista cross-tenant.

**Esforço:** 0.3d · **FIX** · **Bloqueado por:** #132 (precisa de `Evaluate`).

#### #155 — S0-M10-dup: Investigar e fechar como `duplicate` se for clone de #156

**Diagnóstico:** #156 (lockout off-by-one) já foi mergeado em `4177bf2`. #155 tem título similar ("Lockout off-by-one") — provável duplicata.

**Ação:**
1. `gh issue view 155 --repo felipedsvit/mez-go-mono --json body,title,labels` para inspecionar.
2. Se idêntico a #156 → `gh issue close 155 --reason duplicate --comment "Duplicata de #156 (lockout off-by-one)"`
3. Se diferente, promover.

**Esforço:** 0.1d · **TRIAGE** · **Bloqueado por:** nenhum.

#### #157 — S0-M15: Role ID via `time.Now().UnixNano()` (previsível/collisivo)

**Arquivos:** `internal/usecase/admin/role_service.go:34-58`; `migrations/0013_role_id_uuidv7.up.sql` (NOVO)

**Diagnóstico:** `CreateRole` usa `time.Now().UnixNano()` como ID. Em escala, colisões são raras mas possíveis; principal problema é **previsibilidade** (atacante pode enumerar roles de um tenant adivinhando IDs).

**Ação:**
1. Migration `0013_role_id_uuidv7.up.sql`: `ALTER TABLE roles ALTER COLUMN id TYPE UUID USING id::text::UUID;` + `ALTER TABLE roles ALTER COLUMN id SET DEFAULT uuidv7();`. (Requer extensão `pg_uuidv7` ou função custom `uuid_generate_v7`.)
2. Se `pg_uuidv7` não disponível, criar função PL/pgSQL `uuid_generate_v7()` baseada em `gen_random_uuid()` + timestamp prefix.
3. Atualizar `role_service.go:34-58` para usar o default do DB (não gerar no Go).
4. Test: criar 10k roles em loop → IDs são monotônicos mas imprevisíveis (`uuidv7()` garante isso).
5. Adicionar índice em `(tenant_id, id)` se ainda não houver.

**Esforço:** 0.5d · **FIX** · **Bloqueado por:** nenhum.

**Total Sub-sprint 0C: ~1.4d · 3 issues MEDIUM (excluindo #155 dup) + 1 triage**

### 12.4 Dependências Sprint 0 → Sprint 1-5

```
Sprint 0A  ──►  #132 (Principal hydration)  ──►  Sprint 1-5 admin endpoints (#169, #174)
            ──►  #133 (RunInTenantTx)        ──►  Sprint 1-5 handlers novos (#162, #165, #166, #167, #176)

Sprint 0B  ──►  #140 (OIDC nonce)            ──►  Sprint 3 #166 (simplifica: só persistência)
            ──►  #151 (TLS) + #146 (headers)  ──►  Sprint 1 #162 (webhook handler)

Sprint 0C  ──►  (independente, no final)
```

**Regra de gate:** Sprint 1 só inicia após Sprint 0A fechado. Sprints 0B e 0C podem correr em paralelo (com dev único em 0B; 0C é rápido).

### 12.5 Estimativa ajustada (Sprint 0)

| Categoria | LOC | Dias |
|---|---:|---:|
| **CRITICAL** (6 issues, Sub-sprint 0A) | ~1.500 | 3.4 |
| **HIGH** (9 issues, Sub-sprint 0B) | ~1.800 | 4.4 |
| **MEDIUM** (3 issues, Sub-sprint 0C) | ~600 | 1.4 |
| **Housekeeping** (9 issues, §12.0) | ~50 | 0.2 |
| **Tests** (regressão + novos) | ~1.200 | 1.0 |
| **Buffer** (15% para races intermitentes, deps, integration) | — | 1.5 |
| **Total Sprint 0** | **~5.150 LOC** | **~11.9d** |

Combinado com os 22d dos Sprints 1-5: **~34d solo** (6-7 sprints) ou 2-3 sprints com 2 devs.

### 12.6 Definition of Done (Sprint 0)

#### Funcional

- [ ] **§12.0 (housekeeping)**: 9 issues (#129, #130, #134, #136, #139, #141, #144, #152, #156) fechadas com `reason=completed` + comment linkando o commit.
- [ ] **#131**: cookie `__Host-mez_admin` rejeitado se `Secure=false` (test em `cookie_test.go`).
- [ ] **#132**: 6 admin handlers com `RequireScope` aplicado; `TestAdmin_Authorization` 12 sub-tests verde.
- [ ] **#133**: 12 cenários `TestAPI_IDOR_Matrix` verde; `appQFromCtx` removido.
- [ ] **#135**: `TestRole_Escalation_Blocked` verde; CHECK constraint em `roles.scope` aplicado.
- [ ] **#137**: manifest forjado rejeitado; allowlist explícita.
- [ ] **#138**: `TestS3_TenantMismatch` verde; path confusion cross-tenant impossível.
- [ ] **#140**: state com nonce mismatch → 401; `verifier.go` força `RS256`; #166 simplificada.
- [ ] **#142**: `MEZ_API_JWT_SECRET=aaaa…(32)` rejeitado; `TestConfig_Entropy` 5 sub-tests verde.
- [ ] **#143**: defaults aplicados se env unset; `MEZ_HTTP_READ_HEADER_TIMEOUT=0` em prod → refuse boot.
- [ ] **#145**: POST `/api/setup` sem CSRF token → 403; com token válido → 200.
- [ ] **#146**: HSTS emitido em `secure=true`; warning em boot se `secure=false` em prod.
- [ ] **#147**: fechada como `duplicate` (ou promovida se diferente).
- [ ] **#148**: `TestPlatform_Audit_Atomic` verde; falha de audit rollbacka trabalho.
- [ ] **#149**: 100 goroutines Subscribe/Unsubscribe simultâneo → 0 races; H10 unificado em uma query.
- [ ] **#150**: `grep -r "labstack/echo" .` retorna 0; `govulncheck` sem findings de Echo.
- [ ] **#151**: TLS nativo funcional; `MEZ_HTTP_FORCE_HTTPS=true` redireciona 301.
- [ ] **#153**: nenhum `err.Error()` em body de response; `request_id` em todo response.
- [ ] **#154**: `GET /admin/audit` sem `tenant_id` por tenant owner → 403; `LIMIT 1000` default.
- [ ] **#155**: fechada como `duplicate` (ou promovida).
- [ ] **#157**: 10k roles criadas → IDs monotônicos + imprevisíveis; UUID v7 default.

#### Não-funcional

- [ ] `go test -race -shuffle=on -count=1 -timeout 180s ./...` verde.
- [ ] `go test -tags=integration -race -timeout 30s ./...` verde (testes de RLS fail-closed + IDOR + audit atomic).
- [ ] `govulncheck ./...` sem findings de HIGH/CRITICAL.
- [ ] `gofmt -l` vazio.
- [ ] Coverage: ≥ 85% nos packages alterados (`middleware/{authz,csrf,secure_headers,session}`, `transport/http/api`, `usecase/admin`, `usecase/platform`, `adapter/storage/s3`).
- [ ] `make openapi-gen && git diff --exit-code api/openapi.gen.go` verde (se #150 regenerar).
- [ ] `cmd/server/serve` boot em ≤ 5s (sem regressão vs. Fase 8).

#### Operacional

- [ ] `cmd/server/serve` shutdown em ≤ 15s (sem regressão).
- [ ] Chaos test: `tests/chaos/idor_test.go` (NOVO) — tenant A tenta acessar recurso de B → 403 + audit; 1000 tentativas não causam OOM ou log flood.
- [ ] `docs/security/AUDIT_HISTORY.md` (NOVO) consolida auditoria 003 (Fase 8) + correções Sprint 0.
- [ ] README §23 atualizado: Sprint 0 marcado como merged; §25 "audit trail" linka `docs/security/AUDIT_HISTORY.md`.

### 12.7 ADRs novos (Sprint 0)

- **ADR-0041 — FailClosed-by-default para security checks** [#131, #140, #142, #143, #145, #146, #151, #153]. Decisão: **toda verificação de segurança é opt-out em dev, opt-in em prod**. Cookie `Secure`, TLS, HSTS, OIDC nonce, entropy check, CSRF, error sanitization são todos default-ON quando `MEZ_ENV=prod` ou `MEZ_ENV` unset + binary não-dev. Justificativa: histórico do projeto mostra que defaults permissivos em código (DREAD ≥ 7.5) viram production-blockers; reverter isso com feature flags é operacionalmente caro. **Manifesto:** security é o default; relaxar é exceção explícita. Trade-off: dev local sem HTTPS precisa `MEZ_ENV=dev`; documentado em `AGENTS.md` e `docs/security/DEV_MODE.md` (NOVO).

- **ADR-0042 — Principal Hydration no session middleware** [#132]. Decisão: o middleware de sessão **hidrata `Principal.Permissions` e `Principal.Roles`** ao carregar a sessão, em vez de delegar a cada handler. Cache in-memory TTL 5min por `(userID, tenantID)`. Justificativa: (a) `Evaluate` precisa de `Permissions` populado — sem hydration, a chamada sempre nega; (b) cache 5min evita N+1 em loops de admin panel; (c) TTL curto garante que revogação de role propaga em ≤ 5min (aceitável para admin panel); (d) sync.Map + `singleflight.Group` evita thundering herd. Trade-off: 1 query extra por session start (otimizada para ≤ 5ms com índice em `role_bindings.user_id`); session start latency aumenta ~3ms p50.

- **ADR-0043 — Defense-in-Depth RLS + WHERE clause** [#133, #137, #138, #154]. Decisão: **toda query multi-tenant tem DUPLA barreira**: (a) `RunInTenantTx` (RLS via context), E (b) `WHERE tenant_id = $1` explícito. Justificativa: (a) RLS já é FORCED (C3 + C4 do audit), mas bugs em pool routing podem bypassar (DREAD 9.0 do C5); (b) `WHERE tenant_id = $1` é catch-all que pega o caso degraded; (c) audit row em qualquer query que toque mais de 1 tenant (via `EXPLAIN` instrumentation). Trade-off: +1 coluna em toda query, +1 índice em cada tabela (já temos); verbosidade do código. Aceitável: o boilerplate fica centralizado em `txRunner.RunInTenantTx` (helper do #133).

### 12.8 Riscos específicos do Sprint 0

| # | Risco | Probabilidade | Impacto | Mitigação |
|---|-------|---:|---:|---|
| R-S0-1 | #132 (Principal hydration) causa regressão em sessão já ativa pós-deploy | Média | Alto | Manter `Principal.Permissions = nil` se cache miss + flag `MEZ_AUTHZ_STRICT=false` (default true em prod) durante 1 sprint; rollout gradual via `MEZ_AUTHZ_ROLLOUT_PCT=10→50→100` |
| R-S0-2 | #133 (RunInTenantTx) quebra handler que depende de `appQFromCtx` raw | Alta | Médio | Manter `appQFromCtx` por 1 release com `// Deprecated`; telemetria de uso (`metric appQFromCtxLegacyUse`) |
| R-S0-3 | #140 (OIDC nonce) bloqueia login de IdP que não emite nonce | Baixa | Alto | Feature flag `MEZ_OIDC_REQUIRE_NONCE` (default `true` em prod, `false` em dev); documentar em `docs/security/OIDC_IDP_COMPAT.md` |
| R-S0-4 | #149 (concorrência) introduz deadlock se migration de lock-ordering não for feita | Média | Alto | Test com `go test -race -count=100`; chaos test `tests/chaos/concurrency_test.go` (NOVO) com 1000 goroutines; rollout flag `MEZ_BUS_LOCK_ORDERING_V2=true` |
| R-S0-5 | Housekeeping (#129-#156 fechamento) gera confusão se alguém reverter o commit | Baixa | Médio | Fechar issues com `reason=completed`; se reverter, reabrir manualmente (audit trail no GitHub) |
| R-S0-6 | UUID v7 (#157) requer extensão `pg_uuidv7` não disponível no RDS | Média | Médio | Fallback: PL/pgSQL function `uuid_generate_v7()` que combina `gen_random_uuid()` + timestamp; testar em CI com `postgres:15` e `postgres:16` |

### 12.9 Sequência de execução (timeline Sprint 0)

```
Sprint 0 (11.9d) — Auditoria de segurança (PRÉ-REQUISITO)
├── Day 0.2: §12.0 Housekeeping (fechar #129, #130, #134, #136, #139, #141, #144, #152, #156)
├── Sub-sprint 0A (3.4d) — CRITICAL
│   ├── Day 0.5: #131 (cookie Secure)
│   ├── Day 1.0: #132 (admin authorization) [começa, #135/#154 dependem]
│   ├── Day 1.0: #133 (IDOR RunInTenantTx) [paralelo]
│   ├── Day 0.5: #135 (role escalation)
│   ├── Day 0.3: #137 (restore _table allowlist)
│   └── Day 0.3: #138 (S3 tenant path confusion)
├── Sub-sprint 0B (4.4d) — HIGH [paralelo com 0A se 2 devs]
│   ├── Day 0.5: #140 (OIDC nonce)
│   ├── Day 0.3: #142 (JWT entropy)
│   ├── Day 0.2: #143 (ReadHeaderTimeout default)
│   ├── Day 0.3: #145 (CSRF setup)
│   ├── Day 0.2: #146 (HSTS secure=true)
│   ├── Day 0.1: #147 (triage dup)
│   ├── Day 0.5: #148 (RunAsPlatform atomic)
│   ├── Day 1.0: #149 (concorrência)
│   ├── Day 0.3: #150 (Echo dead code)
│   └── Day 0.5: #151 (TLS native)
└── Sub-sprint 0C (1.4d) — MEDIUM
    ├── Day 0.5: #153 (error sanitization)
    ├── Day 0.3: #154 (audit tenant filter)
    ├── Day 0.1: #155 (triage dup)
    └── Day 0.5: #157 (UUID v7)
```

**Single commit (squash) por sub-sprint → `main`:**
- `fase9-s0a-squash`: Sprint 0A
- `fase9-s0b-squash`: Sprint 0B
- `fase9-s0c-squash`: Sprint 0C (incluindo housekeeping + fechamento das 9 stale issues)

**PR body template (cada sub-sprint):**
```markdown
## Sprint 0X — Auditoria de segurança

**Issues fechadas (Closes):** #131, #132, #133, #135, #137, #138 (exemplo 0A)

### Correções
- [lista bullets com referência a commit + arquivo]

### Testes
- `TestAdmin_Authorization` (12 sub-tests) verde
- `TestAPI_IDOR_Matrix` (12 sub-tests) verde
- [etc]

### ADRs formais
- ADR-0041 (FailClosed-by-default)
- ADR-0042 (Principal Hydration)
- ADR-0043 (Defense-in-Depth RLS)

### DoD
- [ ] Sprint 0X DoD items marcados
- [ ] govulncheck verde
- [ ] boot ≤ 5s
```

### 12.10 Não-objetivos do Sprint 0 (explícitos)

- **WAF / rate limit por IP** — fora do escopo (proxy reverso resolve).
- **Bug bounty / responsible disclosure** — pós-1.0.
- **Penetration test externo** — pós-1.0 (antes do release público).
- **Certificações (SOC2, ISO 27001)** — roadmap longo prazo.
- **Migração completa para Vault Transit** — fora do guardrail; `LocalSealer` é o canônico (Fase 7).
- **CSRF em endpoints de webhooks** — Meta/Telegram enviam via POST server-to-server; CSRF não aplica.
- **Rate limit em `/api/setup`** — não-incluído (1 chamada esperada; brute-force coberto por #151 se exposto).
- **Auditoria de dependências (`govulncheck`) rodando em CI** — já está na Fase 7 (`make govulncheck`); Sprint 0 não adiciona.

### 12.11 Referências Sprint 0

- **Auditoria canônica:** `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (todos os 40 findings).
- **Plano de auditoria:** `docs/fase8/FIXES/PLAN_SECURITY_AUDIT.md`.
- **PLan Fase 8:** `docs/fase8/PLAN.md` (carryover C12 boot determinístico + audit log).
- **AGENTS.md:** §1.1 guardrails, §10 patterns, **§Regra 8 (CRITICAL)** RLS fail-closed.
- **DDD-hex review:** `docs/fase8/FIXES/001_DDD_HEXAGONAL_REVIEW.md` (item 3.11 `appQFromCtxOrPool` UNSAFE — base do #133).
- **PR #108** (`bdee3cd`): commits `cc08aa9`, `38368f4`, `a6ee296`, `bcbb880`, `aba5b9b`, `05d6d7a`, `5fdc0b7`, `4177bf2` — fixes já em `main` que serão housekeeping-fechados.

---

> **Última atualização:** junho/2026 (Sprint 0 adicionado — auditoria de segurança como pré-requisito dos Sprints 1–5; 3 ADRs novos 0041-0043; housekeeping das 9 issues stale).
> **Mantenedor:** Felipe D. Svit (mez-go-mono).
> **Próxima revisão:** ao final de cada sprint, com checkmark nos DoD.
