# CLAUDE.md

Architecture essay for `github.com/felipedsvit/mez-go-mono` (Go, hexagonal/clean,
single-binary monorepo). For the high-signal operational subset, read
[AGENTS.md](./AGENTS.md) first.

This document explains the *why* behind the architecture — corrections
C1–C12, decisions D1–D18, and the pipeline inbound/outbound. It assumes
familiarity with the README and the [mez-go (pai) CLAUDE.md](https://github.com/felipedsvit/mez-go/blob/master/CLAUDE.md).

---

## 1. Topology and divergence from mez-go (pai)

`mez-go-mono` is the **consolidated replacement** of `mez-go`. Where the
parent has 6 binaries + NATS JetStream, the mono has 1 binary + bus
in-process. Where the parent has `SECURITY DEFINER` for cross-tenant ops,
the mono has 3 roles (`mez_migrate`/`mez_app`/`mez_platform`) with
`FORCE ROW LEVEL SECURITY`.

```
┌─── mez-go-mono (1 binário) ────────────────────────────────────────────┐
│                                                                        │
│  Clientes HTTP/WS ─► HTTP (chi) ◄─► WS Hub (per-tenant, in-memory)       │
│  Meta/Telegram webhooks ─► /webhooks/* (verif. assinatura, fail-closed)│
│                              │                                         │
│                    In-process Bus (typed channels)                     │
│                    inbound.* / outbound.* / status / dlq / lifecycle    │
│                              │                                         │
│   ┌──────────┬──────────┬──────────┬──────────┐   WhatsMeow (Fase 4)  │
│   │ WABA     │ IG       │ Messenger│ TG Bot   │   1 client/tenant;     │
│   │ stateless│ stateless│ stateless│ stateless│   dispatcher bounded   │
│   └──────────┴──────────┴──────────┴──────────┘                         │
│                              │                                         │
│   Usecase: messaging (ingest/send), routing, outbox+relay, RECONCILER  │
│                              │                                         │
│        Postgres (RLS FORCED) │ S3/MinIO │ Cache (opcional)             │
│                                                                        │
│  Browser admin ─► Painel templ+htmx: /setup │ /login │ /admin/* │ /app/*│
└────────────────────────────────────────────────────────────────────────┘
```

The reconciliation loop (C1) sits next to the outbox relay — both are
durability mechanisms that compensate for the bus not surviving a crash.

---

## 2. Multi-tenancy: 3 roles, FORCE RLS, fail-closed

The most important security correction of the revision (C3/C4/C5). The
model is Postgres RLS with `set_config('mez.tenant_id', _, is_local := true)`
inside every transaction. `FORCE ROW LEVEL SECURITY` is what prevents
the OWNER of the table from bypassing the policy.

### Two access paths

```go
// Normal path: every tenant query goes through here.
func (t *TxRunner) RunInTenantTx(ctx context.Context, tenantID string,
    fn func(Queries) error) error {
    return t.appPool.BeginTxFunc(ctx, func(tx pgx.Tx) error {
        // set_config LOCAL: reset on tx end, safe with pooling.
        if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)",
            tenantID); err != nil {
            return err
        }
        return fn(New(tx))
    })
}

// Cross-tenant path (C5): mez_platform role, ALWAYS audited.
func (t *TxRunner) RunAsPlatform(ctx context.Context, actor string,
    fn func(Queries) error) error {
    return t.platformPool.BeginTxFunc(ctx, func(tx pgx.Tx) error {
        // audit mandatory: who, when, which cross-tenant operation.
        if err := writeAudit(ctx, tx, actor, "platform_access"); err != nil {
            return err
        }
        return fn(New(tx))
    })
}
```

Operations that require `RunAsPlatform`: list all tenants, global
dashboard, aggregated metrics, reset/backup initiated by global admin.
**Each one** writes to `audit_log` — this is the historical anchor of
cross-tenant leakage, so it's the most-audited point.

### Regression test

The CI includes a test that tries to read `messages` from another
tenant **without** `RunInTenantTx` and **without** `mez_platform`, and
requires the query to **fail** (not return zero rows — fail). This
validates fail-closed stays active after schema changes.

---

## 3. Delivery and durability guarantees (C1/C2/D3)

This is the central correction of the revision. The original plan
"persist before 2xx" guarantees the *line*, not the *downstream
processing*. A Go channel has no replay.

### Inbound

```
provider → webhook handler
            ├─ 1. verify signature (fail-closed)
            ├─ 2. persist message + dedup ON CONFLICT
            │     within RunInTenantTx  → COMMIT
            ├─ 3. return 2xx to provider          ← durability frontier
            └─ 4. bus.PublishInbound(...) (non-blocking, drop-safe)  ← notification only
```

- Steps 1–3 are the durability frontier. After COMMIT, the message
  exists and is deduped. The provider doesn't retry after 2xx.
- Step 4 is *notification*, not durability. If the bus buffer is full,
  the publish **drops** the notification (non-blocking) instead of
  blocking the HTTP handler. This avoids the cascade of stuck
  goroutines that would exhaust the HTTP pool under a single-tenant
  burst.

### Why the drop is safe: the Reconciler (C1)

Because step 4 may drop, the **source of truth is the database**, and
a *reconciliation loop* guarantees eventual processing:

```go
// internal/usecase/reconcile/reconciler.go (esboço)
func (r *Reconciler) Run(ctx context.Context) error {
    return r.repo.ForEachTenant(ctx, func(tenantID string) error {
        return r.tx.RunInTenantTx(ctx, tenantID, func(q Queries) error {
            pending, err := q.SelectUnroutedMessages(ctx, batchSize)
            if err != nil { return err }
            for _, m := range pending {
                if err := r.routing.Assign(ctx, m); err != nil { return err }
                r.bus.PublishInbound(m)      // re-notify (best-effort)
                _ = q.MarkRouted(ctx, m.ID)  // status='routed'
            }
            return nil
        })
    })
}
```

States of an inbound message:

| State | Meaning | Who advances |
|-------|---------|--------------|
| `received` | persisted, not yet routed | webhook handler (on insert) |
| `routed` | assigned/processed by `routing` | bus consumer **or** reconciler |
| `notified` | WS broadcast delivered | WS hub (best-effort) |

The reconciler is what turns "drop-safe" into a real guarantee: **no
persisted message stays orphaned indefinitely**, even after a crash
between commit and consumption.

### Outbound

```
usecase.Send → INSERT outbox (status='pending') within RunInTenantTx → COMMIT
                  │
                  ├─ in-process signal (fast)  ─┐
                  │                              ├─► relay goroutine drena
                  └─ poll fallback (5s)  ───────┘
                              │
                              ▼
                    provider.Send(...) → status='sent' | retry | 'dlq'
```

The **poll fallback is mandatory** (not optional): it guarantees that
after a crash between outbox INSERT and in-process signal, the pending
row is still drained on boot and periodically. Send dedup via
`(tenant_id, message_id)` unique.

### Summary

| Direction | Crash-safe? | Mechanism |
|-----------|-------------|-----------|
| Inbound (line) | ✅ | persist-before-2xx |
| Inbound (processing) | ✅ | reconciler (C1) |
| Outbound | ✅ | outbox + relay + poll fallback |
| WS broadcast | ❌ (best-effort) | clients reconnect + htmx polling |

---

## 4. Bus in-process (C2)

Replaces NATS JetStream with **typed Go channels**. Fixed topics →
concrete methods (`PublishInbound`, `PublishOutbound`, `PublishStatus`,
`PublishDLQ`, `PublishLifecycle`) — no `interface{}`.

### Saturation policy

| Topic | Policy when buffer fills | Why |
|-------|--------------------------|-----|
| `inbound` | **non-blocking, drop-safe** | DB is source of truth; reconciler covers the drop |
| `outbound` | no critical buffer — durability is in outbox | relay drains from DB |
| `status` | non-blocking, drop-safe | status reprocessable from DB |
| `dlq` | **blocking with short timeout** | DLQ cannot lose; if full, log error and force flush |

```go
func (b *Bus) PublishInbound(m Message) {
    select {
    case b.inbound <- m:
        b.metrics.published.Inc()
    default:
        // drop-safe: the message is already in the DB; the reconciler reprocesses.
        b.metrics.dropped.Inc()
        b.log.Warn().Str("topic", "inbound").Msg("bus buffer full; drop (reconciler covers)")
    }
}
```

Each channel exposes Prometheus metrics: `bus_published_total`,
`bus_dropped_total`, `bus_buffer_depth`, `bus_consumer_lag`. A growing
`bus_dropped_total` is the signal that the buffer is undersized **or**
that a consumer is slow — and that the reconciler is carrying the slack.

### Memory (acknowledged limitation)

With 1 whatsmeow client/tenant + dispatcher + per-tenant buffers **in a
single process**, the heap scales **linearly with the number of active
tenants**, with no isolation between them. A tenant in burst inflates
shared buffers. Partial mitigation: bounded buffers per tenant and
`MEZ_MAX_ACTIVE_TENANTS` as an operational ceiling. Real memory
isolation only with multi-process (out of 1.0 scope).

---

## 5. Pipeline inbound — the pieces

### Ingestor (`internal/usecase/messaging/ingest.go`)

Pipeline: `resolveContact → resolveConversation → upsertMessage (dedup
ON CONFLICT) → INSERT outbox`, all within **one** `RunInTenantTx` for
atomicity. After commit, publishes to the bus (notification only).

Key differences from mez-go (pai):
- `tenant_id` is UUID (not string) in mez-go-mono
- `Contact.Identities []ChannelIdentity` → our `ProviderID` (single)
- outbox is written at the end of ingest (in the parent, it's part of
  the Send; here we anticipate so the relay is ready)
- dedup is atomic via `ON CONFLICT` (not `ExistsByProvider` check then
  insert)

### Router (`internal/usecase/routing/router.go`)

Simplified vs parent (no ACD, no queues, no skills, no sticky). Just
returns `unassigned` for now. The interface stays stable so the
reconciler and bus consumer call the same method; when ACD arrives
(Phase 5), the internal logic changes but the signature doesn't.

### OutboxRelay (`internal/usecase/outbox/relay.go`)

Drains `outbound_events` in status='pending'. Two triggers:
- In-process signal (`Notify()`) — fast, no poll latency
- Poll fallback (5s default) — covers crash between enqueue and notify

Also drains immediately on boot (covers the crash case). Sender is an
interface; the **default is `NoopSender`** in Phase 2 (returns
`ErrSenderNotImplemented`, leaves the message in `pending`, logs warn
each tick). Real adapters (WABA, IG, Messenger, TG, WhatsMeow) plug in
Phase 3 without changing the infra.

### Reconciler (`internal/usecase/reconcile/reconciler.go`)

The closing piece of C1. Cross-tenant (`RunAsPlatform` via
`InboundEventsRepo.SelectUnroutedMessages` with `FOR UPDATE SKIP LOCKED`).
Runs:
- Once on boot (covers `kill -9` before Run completes)
- Periodically (default 30s) for drift

---

## 6. Pipeline outbound — staged

Phase 2 ships only the *infra* (outbox + relay + Sender interface). The
real adapters (WABA/IG/MSG/TG/WhatsMeow) come in Phase 3.

The `Sender` interface is the only change point:

```go
type Sender interface {
    Send(ctx context.Context, m domain.Message) (providerMsgID string, err error)
}
```

Adapter registry (Phase 3): `outbox.SenderRegistry` maps
`(channel, tenantID)` to a `Sender`. The relay looks up on each
`process()` call. For Phase 2, the registry has only `NoopSender`.

---

## 7. Webhooks — fail-closed (D8)

Both Meta and Telegram webhooks enforce **fail-closed signature
verification**. A misconfigured app is a security incident, not a
silent acceptance.

### Meta (`internal/adapter/webhook/meta/handler.go`)

- Endpoint: `POST /webhooks/meta/:app_id`
- Header: `X-Hub-Signature-256: sha256=<hex>`
- Algorithm: `hmac.Equal(sha256(secret), body)`
- App secret: per-`app_id`, looked up via `AppSecretResolver` (env-based
  in Phase 2; DB-backed in Phase 5)
- Limits: `maxBodyBytes = 2 MiB` (defense against payload bombs)
- Fail-closed:
  - No signature → 403
  - Bad signature → 403
  - No app secret configured → 503
  - Body too large → 413

### Telegram (`internal/adapter/webhook/telegram/handler.go`)

- Endpoint: `POST /webhooks/telegram/:tenant_id`
- Header: `X-Telegram-Bot-Api-Secret-Token: <secret>`
- Algorithm: `subtle.ConstantTimeCompare(expected, got)`
- Secret: per-`tenant_id`, looked up via `SecretResolver`
- Fail-closed: same as Meta, with `maxBodyBytes = 1 MiB`

### Why HMAC over body is dangerous

The app secret sits in memory during request processing. A dump
vulnerability leaks it. Mitigations in this revision:
- `defer zero(secret)` at end of handler
- Short-lived (only during the request)

Full mitigation (Vault Transit) is post-1.0.

---

## 8. API REST (`internal/transport/http/api`)

Phase 2 endpoints (no version prefix, per D15):

```
POST   /webhooks/meta/:app_id            (Meta signature)
POST   /webhooks/telegram/:tenant_id     (Telegram secret)
GET    /api/conversations                (Bearer JWT)
GET    /api/messages?conversation_id=X   (Bearer JWT)
POST   /api/messages                     (501 — Phase 3)
POST   /api/conversations/:id/assign     (Bearer JWT)
POST   /api/conversations/:id/resolve    (Bearer JWT)
GET    /api/channels/:channel/health     (Bearer JWT)
```

### Auth: Bearer JWT (HS256)

`BearerAuth` middleware (Phase 2) extracts `tenant_id` from the
JWT claim and injects it into the context (read by
`api.TenantFromContext`). The OIDC/JWKS variant arrives in Phase 5.

The JWT secret comes from `MEZ_API_JWT_SECRET` and is a fail-closed
field in `cfg.ValidateServe()` (mandatory for `serve` subcommand).

### POST /api/messages = 501 (not 404)

The endpoint exists and is documented in OpenAPI. It returns `501 Not
Implemented` with a body explaining that the real send comes in
Phase 3. This way front-end and API consumers see the contract
before Phase 3 ships.

---

## 9. Outbox → OutboxRepo (`internal/adapter/repository/postgres/outbox.go`)

Two pools:
- `appPool` (mez_app) — used by `Enqueue` (RLS applies, normal path)
- `platformPool` (mez_platform, BYPASSRLS) — used by `Claim`,
  `MarkSent`, `MarkFailed`, `MarkDLQ`, `ForEachTenant` (cross-tenant)

Why split? Because Enqueue runs **inside** the tenant-tx (so the
outbox row respects RLS and the same-tenant invariant), while Claim
needs to read across tenants. The split is the same pattern as the
mez-go (pai) `SECURITY DEFINER` functions, but implemented at the Go
layer with role separation.

### Why `FOR UPDATE SKIP LOCKED`

Two replicas of the relay (or relay + reconciler) might try to claim
the same row. `SKIP LOCKED` makes the second one skip the row and grab
the next. This is what makes horizontal scaling safe without
distributed locks.

---

## 10. Boot order (C12)

The single-process model demands a deterministic boot order. From
`cmd/server/wire.go`:

1. Logger
2. Config (viper, fail-fast)
3. DB pools (app, platform, admin)
4. TxRunner
5. Repos
6. Bus
7. Ingestor + Router (subscribe to bus inbound)
8. OutboxRepo + Relay (Sender noop in Phase 2)
9. InboundEventsRepo + Reconciler
10. Webhook handlers (Meta + Telegram)
11. API handlers
12. HTTP server

### Graceful shutdown (D10 + C12)

Signal → `srv.Shutdown` → `bus.Drain` → `relay.Stop` →
`reconciler.Stop` → pools close. Without this order, the bus can
lose notifications or the relay can lose in-flight claims.

---

## 11. Carryover to next phases

- **Phase 3** (outbound): `Sender` adapters (WABA/IG/MSG/TG/WhatsMeow)
  plug into `outbox.SenderRegistry`. The `Sender` interface is the only
  change point.
- **Phase 4** (WhatsMeow): the `whatsmeow.Manager` (1 client/tenant)
  replaces the parent pool model. `recover()` per goroutine of
  dispatcher (C10 mitigation).
- **Phase 5** (painel): ACD full (queues, skills, sticky) on
  `routing.Router`; cookie session middleware; `/app/*` (inbox) and
  `/admin/*` (full).
- **Phase 6** (backup): restore topológico with FKs deferíveis (groundwork
  in Phase 2 migration 0003 — `DEFERRABLE INITIALLY DEFERRED`).
- **Phase 7** (hardening): envelope encryption (DEK/tenant) +
  `rotate-kek`; `text_enc BYTEA` in messages; JWT key rotation;
  Vault Transit Sealer.
- **Phase 8** (estabilização): boot determinístico from Phase 2 is the
  base; chaos tests + warm-up paralelo de whatsmeow + observability
  final.

---

## 12. References

- `README.md` — visão geral + decisões D1–D18 + C1–C12
- `docs/plan.md` — roadmap macro (Fase 0 → 8)
- `docs/faseN/PLAN.md` — plano detalhado da fase N
- `AGENTS.md` — quick-reference operacional
- `mez-go/AGENTS.md` + `mez-go/CLAUDE.md` — referência do pai
- `mez-go/api/openapi.yaml` — fonte do Phase 2 OpenAPI (#45)
- `mez-go/internal/usecase/messaging/ingest.go` — fonte do Phase 2
  Ingestor (#36)
- `mez-go/internal/outbox/relay.go` — fonte do Phase 2 Relay (#38)
- `mez-go/internal/transport/http/webhook_meta.go` — fonte do Phase 2
  webhook Meta (#40)
