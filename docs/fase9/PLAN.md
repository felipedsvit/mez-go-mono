# Fase 9 — Maturidade de produção SMM (whatsmeow real + escala horizontal + credenciais live)

> **Status:** planejamento · junho/2026 · tracking em `fase9-tracking`.
> **Escopo:** 1 carryover (#158) + 18 issues novas (#159–#176) = **19 issues · ~22d solo** (3-4 sprints) · single commit (squash) por sprint → `main`.
> **Pré-requisitos:** Fases 0–8 merged (Fase 8 commit `bdee3cd` em `main`).
> **Base normativa:** `docs/fase8/PLAN.md` (C12 — boot determinístico), `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (H2/H9 carryovers), `docs/fase8/FIXES/002_ARCHITECTURE_REVIEW.md` (item 1.1 SKIP LOCKED).
> **Origem do plano:** auditoria arquitetural 5-pilares (junho/2026) — gateway omnichannel `mez-go-mono` avaliado contra critérios de mercado de Social Media Management (SMM) multiredes.

### Mapeamento issue → escopo (carryover + novas)

| Issue | Pilar | Título | Tipo |
|------:|-------|--------|------|
| [#158](https://github.com/felipedsvit/mez-go-mono/issues/158) | P5 | Substituir `stubWhatsmeowClient` por `*whatsmeow.Client` real (Fase 9) | NEW · carryover |
| [#159](https://github.com/felipedsvit/mez-go-mono/issues/159) | P2 | A1 — `OutboxRepo.ClaimNext` em `BeginTx` (`FOR UPDATE SKIP LOCKED` funcional) | REWRITE |
| [#160](https://github.com/felipedsvit/mez-go-mono/issues/160) | P2 | A2 — Circuit breaker per `(tenant, channel)` em volta de `Sender.Send` | NEW |
| [#161](https://github.com/felipedsvit/mez-go-mono/issues/161) | P2 | A3 — Jitter + backoff exponencial persistente em outbox retry | REWRITE |
| [#162](https://github.com/felipedsvit/mez-go-mono/issues/162) | P5 | A4 — Handlers webhook Meta/Telegram retornam 200 antes de ingestar (true async) | REWRITE |
| [#163](https://github.com/felipedsvit/mez-go-mono/issues/163) | P4 | A5 — Scheduled posts: coluna `scheduled_at` + query filtrada + índice parcial | NEW |
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
| [#176](https://github.com/felipedsvit/mez-go-mono/issues/176) | P4 | A5.1 — UI/UX API para agendar posts (endpoint + `cron` reconciler) | NEW |

> **Legenda pilares:** P1=Adapter/Factory · P2=Resiliência/RateLimit · P3=Credenciais · P4=Agendamento · P5=Webhooks/Real-time.

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
| **Whatsmeow** | `stubWhatsmeowClient` | **`*whatsmeow.Client` real** com persistência de QR + reconnect | production-ready WAWeb |
| **Health check** | smoke (`Get()`) | **`Sender.Ping(ctx) error` real por canal** | channel-down detection |

### 1.4 Estimativa ajustada (com reuso)

| Categoria | LOC | Dias |
|---|---:|---:|
| **NEW** (whatsmeow real + circuit breaker + scheduled posts + rate limit + Meta refresh + OIDC nonce + webhook secrets + dedup + DLQ consumer + partition bus + health check) | ~3.800 | 9.5 |
| **REWRITE** (claim tx + jitter/backoff + async webhook + bus partition + honest capability) | ~1.200 | 3.0 |
| **ADAPT** (OIDC nonce + webhook secrets → envelope + honest matrix) | ~600 | 1.5 |
| **MECHANICAL** (dedup cache + `goleak` propagação + remover `port.Channel` + async E2E) | ~700 | 1.5 |
| **Tests** (unit + integration + chaos) | ~2.500 | 4.0 |
| **Buffer** (20% para breaker tuning + jitter math + chaos flake) | — | 2.5 |
| **Total** | **~8.800** | **22.0** |

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

- Multi-process / sharding (pós-Fase 9, §25 limitação assumida do 1.0).
- Vault Transit sealer (pós-1.0, §2 + §22).
- Webhook fan-out para sistemas externos (sem `adapter/sink/` no 1.0).
- UI/UX do painel de scheduled posts (apenas API; o painel vem em sprint pós-Fase 9).
- OpenSearch / analytics (descartado em §2 do `docs/plan.md`).
- Cache externo (Redis continua fora do 1.0; in-memory é mandatório).

---

## 3. Issues detalhadas

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
Sprint 1 (5d) — Production-blockers
├── Day 1: #159 (claim tx)
├── Day 2: #160 (circuit breaker)
├── Day 3-4: #161 (backoff persistente + jitter)
└── Day 5: #162 (async webhook)

Sprint 2 (4d) — Scheduled posts
├── Day 1-2: #163 (schema + query + reconciler)
└── Day 3-4: #176 (API + UI placeholder)

Sprint 3 (4d) — Credenciais
├── Day 1-2: #165 (Meta refresh)
├── Day 2-3: #166 (OIDC nonce + refresh)
└── Day 4: #167 (webhook secrets)

Sprint 4 (5d) — Resiliência + observability
├── Day 1-2: #164 (rate limit)
├── Day 2-3: #168 (dedup cache)
├── Day 3-4: #169 (DLQ consumer)
└── Day 5: #170 + #171 + #173 (cleanup)

Sprint 5 (4d) — Whatsmeow real + health
├── Day 1-2: #158 (real client + IdentityStore)
├── Day 2-3: #172 (honest matrix)
├── Day 3-4: #174 (Ping)
└── Day 4-5: #175 (E2E async)
```

Total: **22 dias úteis** (4-5 semanas solo, ou 2-3 sprints com 2 devs).

### Dependências críticas

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

---

## 9. Não-objetivos (explícitos)

- **Multi-process / sharding** (Fase 10+, requer Redis ou NATS para coordination).
- **Vault Transit sealer** (ADR já diz pós-1.0).
- **OpenSearch / analytics** (descartado em §2 do `docs/plan.md`).
- **UI/UX do painel de scheduled posts** (apenas API + template placeholder; UX real é sprint pós-Fase 9).
- **Sink externo para DLQ** (S3 sink é pós-Fase 9; por enquanto audit + metric + log).
- **Auto-scaling / k8s HPA** (deployment é out-of-scope da Fase 9).
- **Scheduled posts recurring (cron syntax)** — Fase 9 cobre `scheduled_at` (one-shot). Cron syntax (RRULE / Quartz-like) é Fase 10+.
- **WebSocket fan-out para scheduled events** (UI real-time é pós-Fase 9).

---

## 10. Referências

- **Auditoria 5-pilares** (origem do plano): conversa com Arquiteto Sênior SMM (junho/2026), 5 pilares: Adapter/Factory, Resiliência/RateLimit, Credenciais, Agendamento, Webhooks/Real-time.
- **Fase 8 PLAN**: `docs/fase8/PLAN.md` (988 LOC) — base normativa para boot/shutdown phases.
- **Auditoria de Segurança**: `docs/fase8/FIXES/003_SECURITY_AUDIT.md` (H2 #140, H9 #147) — `nonce` OIDC + SKIP LOCKED.
- **Auditoria Arquitetural**: `docs/fase8/FIXES/002_ARCHITECTURE_REVIEW.md` (item 1.1) — SKIP LOCKED.
- **Auditoria DDD-Hexagonal**: `docs/fase8/FIXES/001_DDD_HEXAGONAL_REVIEW.md` — base do `port.Sender`.
- **`docs/plan.md`**: roadmap 0–8; Fase 9 é extensão pós-1.0.
- **AGENTS.md**: §1 Identidade, §10 Patterns obrigatórios, §1.1 guardrails (sem Redis/NATS/Vault/multi-process).
- **`mez-go` pai AGENTS**: `/home/user/felipedsvit/mez-go/AGENTS.md` — referência semântica (não porte literal).

---

> **Última atualização:** junho/2026.
> **Mantenedor:** Felipe D. Svit (mez-go-mono).
> **Próxima revisão:** ao final de cada sprint, com checkmark nos DoD.
