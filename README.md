# mez-go-mono

> **Omnichannel Messaging Gateway** em Go — *greenfield no wiring*, **monólito modular**
> em **container único**, integrando 5 canais (WABA, WhatsMeow, Instagram Direct,
> Messenger, Telegram Bot), normalizando eventos num modelo unificado, com painel
> web (`templ + htmx`) e API REST consistente validada por OIDC.

[![build](https://img.shields.io/badge/build-CI-blue)]()
[![go](https://img.shields.io/badge/go-1.22+-00ADD8)]()
[![license](https://img.shields.io/badge/license-proprietary-lightgrey)]()
[![status](https://img.shields.io/badge/status-pre--1.0-orange)]()

---

## Índice

1. [O que é (e o que não é)](#1-o-que-é-e-o-que-não-é)
2. [Escopo 1.0](#2-escopo-10)
3. [Mudanças desta revisão (changelog arquitetural)](#3-mudanças-desta-revisão-changelog-arquitetural)
4. [Topologia](#4-topologia)
5. [Decisões arquiteturais](#5-decisões-arquiteturais)
6. [Garantias de entrega e durabilidade](#6-garantias-de-entrega-e-durabilidade)
7. [Bus in-process](#7-bus-in-process)
8. [Multi-tenancy e RLS](#8-multi-tenancy-e-rls)
9. [Modelo de dados](#9-modelo-de-dados)
10. [Criptografia de credenciais](#10-criptografia-de-credenciais)
11. [Canais e matriz de capacidades](#11-canais-e-matriz-de-capacidades)
12. [Autenticação](#12-autenticação)
13. [Backup / Restore / Reset](#13-backup--restore--reset)
14. [API REST e OpenAPI](#14-api-rest-e-openapi)
15. [Painel web (rotas)](#15-painel-web-rotas)
16. [Estrutura de diretórios](#16-estrutura-de-diretórios)
17. [Quickstart](#17-quickstart)
18. [Configuração (variáveis de ambiente)](#18-configuração-variáveis-de-ambiente)
19. [Build e desenvolvimento](#19-build-e-desenvolvimento)
20. [Operação em produção](#20-operação-em-produção)
21. [Riscos e mitigações](#21-riscos-e-mitigações)
22. [Reuso do mez-go (porte)](#22-reuso-do-mez-go-porte)
23. [Roadmap e estimativas](#23-roadmap-e-estimativas)
24. [Definition of Done](#24-definition-of-done)
25. [Limitações conhecidas](#25-limitações-conhecidas)

---

## 1. O que é (e o que não é)

`mez-go-mono` é a **substituição consolidada** do `mez-go` atual, hoje fragmentado em
**6 binários** (`mez-core`, `mez-ui`, `mez-worker-whatsmeow`, `mez-analytics-consumer`,
`mez-automation`, `mez-campaigns`), acoplado a **NATS JetStream** e com débito acumulado
(docs desatualizadas, bugs de code-review V2/V3 abertos).

O objetivo é **um produto: um binário, uma imagem**. Quando o mono atingir paridade no
escopo 1.0, o `mez-go` multi-binário é **arquivado**. Não se mantêm os dois em paralelo —
codebases divergem e bugfix vira trabalho dobrado.

**Princípio de migração:** *portar* o domínio/usecase/adapters já provados; **não importar**
as costuras que são o débito. O que não vem junto: o broker NATS (vira bus in-process), os
5 binários extras (colapsam em 1) e o over-scope V2/V3.

> ⚠️ **Importante:** "greenfield" refere-se ao *wiring* (cmd, bus, apresentação). A maior
> parte do código de domínio e dos adapters é **porte mecânico parcial** — ver
> [§22](#22-reuso-do-mez-go-porte) para a contabilidade honesta de esforço, que **não** é
> proporcional a LOC.

---

## 2. Escopo 1.0

### Dentro (o produto)

Gateway omnichannel com **5 canais em paridade**, inbox unificado com agentes, painel admin,
auth (OIDC + bootstrap), backup/restore/reset, API REST + OpenAPI.

### Fora do 1.0 (débito a descartar)

`analytics`/TimescaleDB, `crm`, `automation`/River, `campaigns`, `marketplace`,
feature-flags-como-produto, tooling GDPR e o **Vault sealer**. Essas features (~1.900 LOC no
pai) brigam com a meta "um container". Voltam pós-1.0 **só se** justificadas por demanda real.

> 🔧 **Correção desta revisão:** a master key local (envelope encryption, ver [§10](#10-criptografia-de-credenciais))
> basta no modelo single-box. **Vault Transit é opcional e pós-1.0** — o plano original
> mantinha simultaneamente "descartar Vault sealer" e "portar Vault Transit (510 LOC)", uma
> contradição que esta revisão resolve a favor da crypto local.

---

## 3. Mudanças desta revisão (changelog arquitetural)

Esta seção lista as **correções aplicadas** sobre o plano original. Cada item resolve um risco
ou uma ambiguidade identificada na revisão de arquitetura.

| # | Área | Problema no plano original | Correção aplicada |
|---|------|----------------------------|-------------------|
| C1 | Bus inbound | `persist → 2xx → publish` garante a *linha*, não o *processamento downstream*. Crash entre commit e consumo deixa mensagem persistida-mas-não-roteada, sem replay. | **Reconciliation loop no boot** + varredura periódica que reprocessa mensagens em estado não-roteado. Ver [§6](#6-garantias-de-entrega-e-durabilidade). |
| C2 | Bus saturação | "buffer bounded + counter" não define política quando o buffer enche. Bloquear após o 2xx esgota o pool HTTP em burst. | **Publish inbound non-blocking + drop-safe** (DB é fonte da verdade; reconciliation cobre o drop). Outbound depende do outbox + poll. Ver [§7](#7-bus-in-process). |
| C3 | RLS | Owner de tabela **bypassa RLS por padrão**; sem isso o isolamento é fail-open. Não havia menção. | **`FORCE ROW LEVEL SECURITY`** em todas as tabelas multi-tenant + role de aplicação **sem `BYPASSRLS`** + role separado para migrations/owner. Ver [§8](#8-multi-tenancy-e-rls). |
| C4 | RLS fail-closed | `current_setting` ausente podia falhar de forma ambígua. | Policy exige `mez.tenant_id` setado; ausência → **erro (fail-closed)**, testado. |
| C5 | RLS admin | Não estava dito como o admin global opera cross-tenant sob RLS. | Caminho **explícito** `RunAsPlatform` com role dedicado de bypass auditado. Ver [§8](#8-multi-tenancy-e-rls). |
| C6 | Restore FK | "Upsert por linha" sem ordenação topológica viola FKs (`messages` antes de `conversations`). | **Ordem topológica** de restore + constraints `DEFERRABLE INITIALLY DEFERRED` dentro da tx. Ver [§13](#13-backup--restore--reset). |
| C7 | Restore schema | "Recusa se incompatível" torna todo backup inútil após qualquer migration. | **Replay de migrations** sobre o backup antes do upsert; manifesto carrega a versão de origem. Ver [§13](#13-backup--restore--reset). |
| C8 | Backup consistência | COPY multi-tabela + tar S3 não são point-in-time atômicos; tx longa gera bloat. | Snapshot do DB em **tx única `REPEATABLE READ`** com caveat de bloat documentado; S3↔DB declarado **não-atômico**. Ver [§13](#13-backup--restore--reset). |
| C9 | Vault | Contradição: descarta Vault sealer mas porta Vault Transit; topologia não tem Vault. | **Envelope encryption local** (AES-256-GCM, DEK/tenant, KEK via master key). Vault Transit vira backend opcional pós-1.0. Ver [§10](#10-criptografia-de-credenciais). |
| C10 | Single-box | §12 não listava blast radius, deploy-downtime, migrate-on-boot, IP compartilhado, contenção de recursos, slow WS consumer. | Todos adicionados a [§21](#21-riscos-e-mitigações) com mitigação. |
| C11 | Estimativas | 23-32 dias solo assume "porte mecânico" cobrindo a troca do broker (que é transversal). | Reestimado em **35-50 dias úteis**, com whatsmeow e backup realocados e uma fase de estabilização. Ver [§23](#23-roadmap-e-estimativas). |
| C12 | Boot/shutdown | Não havia fase para o cabeamento do processo único (ordem de boot, shutdown coordenado). | **Fase 8 — Estabilização do processo único** adicionada. Ver [§23](#23-roadmap-e-estimativas). |

---

## 4. Topologia

```
┌──────────────────────── mez-go-mono (1 binário) ────────────────────────┐
│                                                                          │
│  Clientes HTTP/WS ─► HTTP (chi) ◄─► WS Hub (per-tenant, in-memory)       │
│  Meta/Telegram webhooks ─► /webhooks/* (verif. assinatura, fail-closed)  │
│                              │                                           │
│                    In-process Bus (typed channels)                       │
│                    inbound.* / outbound.* / status / dlq                 │
│                              │                                           │
│   ┌──────────┬──────────┬──────────┬──────────┐   WhatsMeow              │
│   │ WABA     │ IG       │ Messenger│ TG Bot   │   (1 client/tenant;      │
│   │ stateless│ stateless│ stateless│ stateless│    dispatcher bounded;   │
│   └──────────┴──────────┴──────────┴──────────┘    recover/goroutine)    │
│                              │                                           │
│   Usecase: messaging (ingest/send), routing, outbox+relay, RECONCILER    │
│                              │                                           │
│        Postgres (RLS FORCED) │ S3/MinIO │ Cache (opcional)               │
│                                                                          │
│  Browser admin ─► Painel templ+htmx: /setup │ /login │ /admin/* │ /app/* │
└──────────────────────────────────────────────────────────────────────────┘
          │                          │
   Postgres (sidecar)          MinIO (sidecar)
```

> O diagrama reflete a topologia **corrigida**: o `reconciler` aparece ao lado do `outbox relay`
> (C1), a RLS é marcada como `FORCED` (C3), e **não há Vault na topologia** (C9). A crypto é
> local ao processo.

---

## 5. Decisões arquiteturais

| # | Decisão | Justificativa | Status |
|---|---------|---------------|--------|
| D1 | Bus in-process **tipado** (métodos concretos por evento; **não `any`**) | Type-safety em compile-time, zero type-assertion no handler. Tópicos fixos → métodos concretos. | mantida |
| D2 | **Inbound durável antes do ack**: persiste (dedup `ON CONFLICT`) e **só então** retorna 2xx | Channel Go não sobrevive a crash; gravar-antes-de-200 evita perda silenciosa | mantida + **C1/C2** |
| D2b | **Reconciler no boot e periódico** varre mensagens persistidas-mas-não-roteadas | D2 garante a linha, não o processamento downstream | **novo (C1)** |
| D3 | Outbox + relay in-process com **poll de fallback** | Resiliência a crash; relay drena no boot mesmo sem sinal in-process | mantida + reforço |
| D4 | **1 client whatsmeow por tenant** | Simplifica vs pool multi-tenant | mantida |
| D5 | RLS via `RunInTenantTx(ctx, tenantID, fn)` + **`FORCE RLS`** + role app sem `BYPASSRLS` | Isolamento real, fail-closed | mantida + **C3/C4** |
| D5b | `RunAsPlatform(ctx, fn)` para operações cross-tenant do admin | Admin global precisa enxergar todos os tenants | **novo (C5)** |
| D6 | Outbound action-aware (`reaction, edit, revoke, mark_read, typing, presence`) | Paridade de capacidades | mantida |
| D7 | Capability negotiation + fallback media→text | Matriz como código | mantida |
| D8 | Webhook Meta unificado com verificação `X-Hub-Signature-256` por app, fail-closed | Reduz superfície | mantida |
| D9 | Storage S3-compatible (MinIO no dev) | Mídia em `s3://<bucket>/tenants/<id>/...` | mantida |
| D10 | Graceful shutdown completo (signal + whatsmeow `Disconnect()` + bus drain + relay flush) | Sem corrupção de session store | mantida + reforço |
| D11 | `migrate` subcommand com golang-migrate embed | Sem binário extra | mantida |
| D12 | OpenAPI gerado por `oapi-codegen`; CI valida diff | Spec é contrato | mantida |
| D13 | `templ + htmx` para painel (HDA, sem build JS) | Sem SPA | mantida |
| D14 | Bootstrap wizard (Argon2id), OIDC para os demais | Híbrido validado | mantida |
| D15 | **Sem prefixo de versão** na API (`/messages`) | Decisão confirmada | mantida |
| D16 | Session cookies HttpOnly + CSRF token em forms | Padrão web seguro | mantida |
| D17 | Audit log de toda ação admin **e cross-tenant** | Rastreabilidade; `RunAsPlatform` é auditado | mantida + reforço |
| D18 | **Envelope encryption local** (AES-256-GCM, DEK/tenant, KEK = master key) | Single-box não exige Vault; resolve a contradição | **revista (C9)** |

---

## 6. Garantias de entrega e durabilidade

Esta seção é a correção central da revisão (**C1/C2**). O modelo do plano original — *persistir
antes do 2xx* — é **necessário mas insuficiente**. Ele garante que a mensagem foi gravada; não
garante que o processamento downstream (routing, automações futuras, WS broadcast persistente)
ocorreu, porque um `channel` Go **não tem replay**.

### Inbound — o caminho e suas garantias

```
provider → webhook handler
            ├─ 1. verifica assinatura (fail-closed)
            ├─ 2. persiste mensagem + dedup ON CONFLICT (tenant_id, provider_msg_id)
            │     dentro de RunInTenantTx  → COMMIT
            ├─ 3. retorna 2xx ao provider          ← fronteira de durabilidade
            └─ 4. bus.PublishInbound(...)  (non-blocking, drop-safe)  ← apenas notificação
```

- **Passos 1-3** são a fronteira de durabilidade. Depois do COMMIT, a mensagem existe e está
  deduplicada. O provider não re-tenta após o 2xx.
- **Passo 4** é *notificação*, não durabilidade. Se o buffer do bus estiver cheio, o publish
  **descarta** a notificação (non-blocking) em vez de bloquear o handler HTTP. Isso evita a
  cascata de goroutines presas que esgotaria o pool HTTP sob burst de um único tenant.

### Por que o drop é seguro: o Reconciler (C1)

Como o passo 4 pode descartar, a **fonte da verdade é o banco**, e um *reconciliation loop*
garante o processamento eventual:

```go
// internal/usecase/reconcile/reconciler.go (esboço)
//
// Roda no boot e em intervalo (ex.: 30s). Varre mensagens em estado
// "received" que ainda não foram roteadas/notificadas e reprocessa.
func (r *Reconciler) Run(ctx context.Context) error {
    return r.repo.ForEachTenant(ctx, func(tenantID string) error {
        return r.tx.RunInTenantTx(ctx, tenantID, func(q Queries) error {
            pending, err := q.SelectUnroutedMessages(ctx, batchSize) // status='received'
            if err != nil { return err }
            for _, m := range pending {
                if err := r.routing.Assign(ctx, m); err != nil { return err }
                r.bus.PublishInbound(m)      // re-notifica (best-effort)
                _ = q.MarkRouted(ctx, m.ID)  // status='routed'
            }
            return nil
        })
    })
}
```

Estados de uma mensagem inbound:

| Estado | Significado | Quem avança |
|--------|-------------|-------------|
| `received` | persistida, ainda não roteada | webhook handler (no insert) |
| `routed` | atribuída/processada por `routing` | bus consumer **ou** reconciler |
| `notified` | broadcast WS entregue aos clientes conectados | WS hub (best-effort) |

> O reconciler é o que transforma o "drop-safe" em uma garantia real: **nenhuma mensagem
> persistida fica órfã indefinidamente**, mesmo após crash entre commit e consumo.

### Outbound — outbox + relay (D3)

```
usecase.Send → INSERT outbox (status='pending') dentro de RunInTenantTx → COMMIT
                      │
                      ├─ sinal in-process (rápido)  ─┐
                      │                              ├─► relay goroutine drena
                      └─ poll de fallback (5s)  ─────┘
                                  │
                                  ▼
                        provider.Send(...) → status='sent' | retry | 'dlq'
```

O **poll de fallback** é obrigatório (não opcional): garante que, após um crash entre o INSERT
no outbox e o sinal in-process, a row pendente ainda seja drenada no boot e periodicamente.
Dedup de envio via `(tenant_id, message_id)` unique.

### Resumo das garantias

| Direção | Sobrevive a crash? | Mecanismo |
|---------|--------------------|-----------|
| Inbound (linha) | ✅ | persist-before-2xx |
| Inbound (processamento) | ✅ | reconciler (C1) |
| Outbound | ✅ | outbox + relay + poll de fallback |
| WS broadcast | ❌ (best-effort) | clientes reconectam e dão polling htmx |

---

## 7. Bus in-process

Substitui o NATS JetStream por **channels Go tipados**. Tópicos fixos → métodos concretos
(`PublishInbound`, `PublishOutbound`, `PublishStatus`), não `interface{}`.

### Política de saturação (C2)

| Tópico | Política quando o buffer enche | Razão |
|--------|--------------------------------|-------|
| `inbound.*` | **non-blocking, drop-safe** | DB é fonte da verdade; reconciler cobre o drop |
| `outbound.*` | não usa buffer crítico — durabilidade está no outbox | relay drena do DB |
| `status` | non-blocking, drop-safe | status reprocessável a partir do DB |
| `dlq` | **blocking com timeout curto** | DLQ não pode perder; se cheio, registra erro e força flush |

```go
// internal/adapter/broker/bus.go (esboço)
func (b *Bus) PublishInbound(m Message) {
    select {
    case b.inbound <- m:
        b.metrics.published.Inc()
    default:
        // drop-safe: a mensagem já está no DB; o reconciler reprocessa.
        b.metrics.dropped.Inc()
        b.log.Warn().Str("topic", "inbound").Msg("bus buffer cheio; drop (reconciler cobre)")
    }
}
```

### Backpressure observável

Cada channel expõe métricas Prometheus: `bus_published_total`, `bus_dropped_total`,
`bus_buffer_depth`, `bus_consumer_lag`. Um `bus_dropped_total` crescente é o sinal de que o
buffer está subdimensionado **ou** de que um consumer está lento — e que o reconciler está
carregando a folga.

### Memória (limitação assumida)

Com 1 client whatsmeow/tenant + dispatcher + buffers por tenant **num único processo**, o heap
escala **linear com o número de tenants ativos**, sem isolamento entre eles. Um tenant em burst
infla os buffers compartilhados. Mitigação parcial: buffers bounded por tenant e `MEZ_MAX_ACTIVE_TENANTS`
como teto operacional. Isolamento real de memória só com multi-process (fora do 1.0).

---

## 8. Multi-tenancy e RLS

A correção mais importante de segurança da revisão (**C3/C4/C5**). O modelo é Postgres RLS
com `set_config('mez.tenant_id', _, is_local := true)` dentro de cada transação.

### Fundação obrigatória (na migration 0001)

```sql
-- 1) Habilita E FORÇA RLS. FORCE é o que impede o OWNER da tabela de bypassar a policy.
ALTER TABLE messages       ENABLE ROW LEVEL SECURITY;
ALTER TABLE messages       FORCE  ROW LEVEL SECURITY;   -- C3: sem isto, owner = fail-open
-- (repetir para conversations, contacts, channel_credentials, agents, tenant_owners, outbox, audit_log)

-- 2) Policy fail-closed: exige mez.tenant_id setado.
--    current_setting(..., missing_ok := false) LANÇA erro se ausente → fail-closed (C4).
CREATE POLICY tenant_isolation ON messages
    USING      (tenant_id = current_setting('mez.tenant_id', false)::uuid)
    WITH CHECK (tenant_id = current_setting('mez.tenant_id', false)::uuid);
```

### Separação de roles

| Role | Login? | `BYPASSRLS`? | Usado por |
|------|:------:|:------------:|-----------|
| `mez_migrate` (owner das tabelas) | ✅ | — | `cmd/server migrate` apenas |
| `mez_app` (role da aplicação) | ✅ | **NÃO** | serviço HTTP/usecases (caminho normal) |
| `mez_platform` (cross-tenant) | ✅ | **SIM (auditado)** | apenas `RunAsPlatform` |

> A aplicação **nunca** conecta como `mez_migrate` (owner). Se conectasse, `FORCE RLS` ainda
> protegeria, mas a separação de roles é defesa em profundidade. `mez_app` **não** tem
> `BYPASSRLS` — qualquer query fora de `RunInTenantTx` falha (fail-closed), o que é o
> comportamento desejado.

### Os dois caminhos de acesso

```go
// Caminho normal: toda query de tenant passa por aqui.
func (t *TxRunner) RunInTenantTx(ctx context.Context, tenantID string, fn func(Queries) error) error {
    return t.pool.BeginTxFunc(ctx, func(tx pgx.Tx) error {
        // set_config LOCAL: resetado no fim da tx, seguro com pooling.
        if _, err := tx.Exec(ctx, "SELECT set_config('mez.tenant_id', $1, true)", tenantID); err != nil {
            return err
        }
        return fn(New(tx))
    })
}

// Caminho cross-tenant do admin global (C5): role mez_platform, SEMPRE auditado.
func (t *TxRunner) RunAsPlatform(ctx context.Context, actor string, fn func(Queries) error) error {
    return t.platformPool.BeginTxFunc(ctx, func(tx pgx.Tx) error {
        // audit obrigatório: quem, quando, qual operação cross-tenant.
        if err := writeAudit(ctx, tx, actor, "platform_access"); err != nil {
            return err
        }
        return fn(New(tx))
    })
}
```

Operações que exigem `RunAsPlatform`: listar todos os tenants, dashboard global de health,
métricas agregadas, reset/backup iniciado pelo admin global. **Cada uma** gera registro em
`audit_log` — é o ponto histórico de vazamento cross-tenant, então é o ponto mais auditado.

### Teste de regressão obrigatório

O CI inclui um teste que tenta ler `messages` de outro tenant **sem** `RunInTenantTx` e
**sem** `mez_platform`, e exige que a query **falhe** (não retorne zero linhas — falhe). Isso
valida que o fail-closed continua ativo após mudanças de schema.

---

## 9. Modelo de dados

### Tabelas principais

| Tabela | Papel | Chave de isolamento |
|--------|-------|---------------------|
| `tenants` | multi-tenant core | (próprio `id`) |
| `admin_globals` | admins do gateway (1+), senha Argon2id | — (global) |
| `tenant_owners` | donos de tenant; identidade OIDC | `tenant_id` |
| `agents` | atendentes; OIDC + permissões + quota | `tenant_id` |
| `channel_credentials` | 1 linha por `(tenant, channel)`, cred cifrada (DEK) | `tenant_id` |
| `contacts` | contatos por canal | `tenant_id` |
| `conversations` | thread unificada | `tenant_id` |
| `messages` | mensagens normalizadas | `tenant_id` |
| `outbox` | fila de envio durável | `tenant_id` |
| `audit_log` | ações admin + acessos `RunAsPlatform` | `tenant_id` nullable (global) |

### Identidade OIDC (estável)

`tenant_owners` e `agents` usam `UNIQUE(oidc_issuer, oidc_sub)`. **Nunca** mapear por `email`
(mutável e reatribuível no IdP). `email`/`name` ficam só como atributos de exibição.

### Chaves primárias — UUID

Todas as PKs são **UUID v4** (não `bigserial`). Isso é **requisito**, não preferência: o
restore lógico (ver [§13](#13-backup--restore--reset)) insere IDs explícitos. Com `bigserial`,
o restore não avançaria a sequence e o próximo INSERT colidiria. Com UUID, o problema não existe.

### Canais (enum)

```go
const (
    ChannelWABA   Channel = "waba"
    ChannelWAWeb  Channel = "whatsmeow"   // informal
    ChannelIG     Channel = "instagram"
    ChannelMSG    Channel = "messenger"
    ChannelTGBot  Channel = "telegram_bot"
)
```

### Foreign keys deferíveis (C6)

As FKs internas do recorte de tenant (`messages → conversations → contacts`) são declaradas
`DEFERRABLE INITIALLY DEFERRED`, para que o restore por upsert dentro de uma única transação
não precise respeitar ordem de inserção linha-a-linha:

```sql
ALTER TABLE messages
    ADD CONSTRAINT fk_messages_conversation
    FOREIGN KEY (conversation_id) REFERENCES conversations(id)
    DEFERRABLE INITIALLY DEFERRED;
```

---

## 10. Criptografia de credenciais

**Decisão revista (C9).** O plano original era contraditório: descartava o "Vault sealer" em
§0.2 mas mantinha "Vault Transit" como porte de 510 LOC, com a topologia sem nenhum Vault. Esta
revisão resolve a favor da **crypto local**, coerente com o modelo single-box.

### Envelope encryption local

```
master key (KEK)  ──── carregada de MEZ_MASTER_KEY (env) ou arquivo montado
   │
   ├─ wrap/unwrap ──►  DEK por tenant (AES-256-GCM)
                          │
                          └─ cifra/decifra credenciais de canal
                             (tokens Meta, bot tokens, etc.)
```

- **KEK**: master key de 32 bytes, injetada via `MEZ_MASTER_KEY` (base64) ou arquivo
  (`MEZ_MASTER_KEY_FILE`). Nunca persiste no DB.
- **DEK por tenant**: gerada no provisionamento, cifrada (wrapped) pela KEK, e o *wrapped DEK*
  é guardado em `channel_credentials`. A DEK em claro só existe em memória durante a operação.
- **Algoritmo**: AES-256-GCM (autenticado). Nonce por operação.

```go
// pkg/crypto/envelope.go (esboço)
type Envelope struct{ kek []byte }

func (e *Envelope) EncryptForTenant(wrappedDEK, plaintext []byte) ([]byte, error) {
    dek, err := e.unwrapDEK(wrappedDEK) // AES-GCM(kek, wrappedDEK)
    if err != nil { return nil, err }
    defer zero(dek)
    return aesgcmSeal(dek, plaintext)
}
```

### Rotação

- **Rotação de DEK**: re-cifra as credenciais do tenant com nova DEK; operação por tenant,
  auditada.
- **Rotação de KEK**: re-wrap de todos os DEKs. Operação `cmd/server rotate-kek` com a KEK
  antiga e a nova; roda offline (janela de manutenção).

### Vault Transit como backend opcional (pós-1.0)

A interface `Sealer` abstrai o backend:

```go
type Sealer interface {
    Wrap(ctx context.Context, dek []byte) (wrapped []byte, err error)
    Unwrap(ctx context.Context, wrapped []byte) (dek []byte, err error)
}
```

O 1.0 usa `LocalSealer` (KEK em memória). Um `VaultTransitSealer` pode ser plugado pós-1.0
**sem mudar o modelo de dados** — mas isso adiciona um SPOF/sidecar e contraria "um container",
então fica fora do escopo 1.0 por decisão explícita.

---

## 11. Canais e matriz de capacidades

> **Fonte única de verdade = código.** A matriz canônica é o `CapabilitySet` declarado por cada
> adapter em `internal/adapter/provider/<canal>/capabilities.go`, validado por
> `capabilities_test.go`. A tabela abaixo é **ilustrativa/derivada**, não normativa — em
> divergência, vale o código.
>
> Atenção: `inline_keyboard` agrupa paradigmas distintos (TG inline keyboard ≠ WABA interactive
> buttons/list ≠ IG quick replies); cada adapter expõe a cap específica que de fato suporta.

Capacidades comuns a **todos** os canais: `text`, `media`, `reactions`. As diferenças
relevantes (subconjunto; a tabela completa vive em `capabilities.go`):

| Cap | WABA | WM | IG | MSG | TGB |
|-----|:----:|:--:|:--:|:---:|:---:|
| edit | ❌ | ✅ | ❌ | ❌ | ✅ |
| delete/revoke | ✅ | ✅ | ❌ | ❌ | ✅ |
| templates | ✅ | ❌ | ❌ | ❌ | ❌ |
| groups / newsletter | ❌ | ✅ | ❌ | ❌ | ✅ |
| presence/typing | ❌ | ✅ | ❌ | ✅ | ✅ |
| mark_read | ✅ | ✅ | ❌ | ❌ | ❌ |
| handover | ❌ | ❌ | ✅ | ✅ | ❌ |
| persistent_menu / OTN | ❌ | ❌ | ❌ | ✅ | ❌ |
| story_reply | ❌ | ❌ | ✅ | ❌ | ❌ |
| calls / disappearing / blocklist | ❌ | ✅ | ❌ | ❌ | ❌ |
| forum/topics / payments / gifts | ❌ | ❌ | ❌ | ❌ | ✅ |
| inline_keyboard (paradigmas distintos) | ✅ | ❌ | ✅ | ✅ | ✅ |

### Negociação de capacidade e fallback

No envio, o sender resolve a capacidade do canal de destino. Se a operação não é suportada
(ex.: `edit` em IG), aplica-se o fallback declarado (ex.: `media → text` quando o canal não
aceita mídia). A resolução é em compile-time-friendly: cada provider declara seu `CapabilitySet`,
e `capabilities_test.go` garante que a matriz e o código não divergem.

### WhatsMeow — modelo simplificado

1 client por tenant (D4), com dispatcher de buffers bounded e **`recover()` por goroutine de
dispatcher** (mitigação de C10 — um panic num tenant não derruba o processo). Reconexão
automática + graceful `Disconnect()` no shutdown. Session store em Postgres.

---

## 12. Autenticação

### Admin global (bootstrap)

```
1. Container sobe → SELECT count(*) FROM admin_globals = 0
2. Habilita APENAS GET /setup (wizard)
3. Wizard: define email + senha admin
4. Argon2id hash → INSERT admin_global → invalida /setup para sempre
```

### Admin global (uso normal)

- `GET /login` → form email+senha
- `POST /login` → verifica Argon2id → seta session cookie (HttpOnly, SameSite=Lax, Secure em prod)
- Middleware checa sessão em `/admin/*`

### Tenant owners + agentes (OIDC)

- `GET /auth/oidc/login?tenant=...` → redirect para `MEZ_OIDC_ISSUER` (state + PKCE)
- Callback valida ID token via JWKS, mapeia **`(issuer, sub)`** → `tenant_owners` ou `agents`.
  `sub` é estável; **não** mapear por `email`.
- Sessão paralela (mesmo cookie, payload diferente).

### API REST (programática)

- `Authorization: Bearer <jwt>` validado via JWKS.
- Header `X-Tenant-ID` obrigatório, **validado contra a claim** do token.
- Rejeita tokens sem claim de tenant.

---

## 13. Backup / Restore / Reset

> **Não usar `pg_dump --tenant` / `pg_restore --clean`.** `pg_dump` não filtra linhas (não
> existe `--tenant`) e `pg_restore --clean` derruba/recria tabelas compartilhadas, **apagando
> os outros tenants**. Backup por-tenant é **export lógico no nível da aplicação**, dentro de
> `RunInTenantTx` (a RLS garante o recorte). Tudo em stream (`io.Pipe`).

### Backup (export lógico)

```
1. POST /admin/tenants/:id/backup → cria job assíncrono.
2. ABRE UMA ÚNICA tx REPEATABLE READ (snapshot consistente do DB — C8).
   Para cada tabela: COPY (SELECT * FROM <t> WHERE tenant_id=$1) TO STDOUT (NDJSON).
   Sem dump de schema.
3. Em paralelo: lista objetos S3 tenants/<id>/ → tar em stream.
4. io.Pipe + multipart upload → bucket mezgo-backups.
   Manifesto embute: schema_version (migration version) + created_at + checksums.
5. Audit log + retorna backup_id. UI mostra progresso via htmx polling.
```

**Caveats documentados (C8):**

- A tx única `REPEATABLE READ` dá snapshot consistente do **DB**, mas uma tx longa drenando
  tabelas grandes **bloqueia VACUUM e gera bloat** durante o backup. Para tenants grandes,
  agendar fora de pico.
- O `tar` do S3 **não** está no mesmo snapshot. DB e mídia **não são point-in-time atômicos**:
  uma mensagem nova durante o backup pode ter a linha sem o blob, ou o inverso. É um trade-off
  aceito e **declarado**, não um bug.

### Restore (import lógico idempotente)

```
1. POST /admin/tenants/:id/restore com backup_id (confirmação dupla "RESTORE").
2. Lê schema_version do manifesto.
   ├─ se == schema atual → segue.
   └─ se < schema atual → REPLAY das migrations sobre os dados do backup
                          antes do upsert (C7). Recusa apenas se > atual.
3. Download do S3 → dentro de RunInTenantTx, com FKs DEFERIDAS (C6):
      upsert por linha (INSERT ... ON CONFLICT DO UPDATE), em ordem topológica:
      contacts → conversations → messages → outbox.
   Nunca --clean / DDL destrutiva.
4. Extrai mídia para S3 (tenants/<id>/).
5. Audit log.
```

> **C6 (ordenação FK):** o upsert respeita a ordem topológica das FKs. As constraints
> `DEFERRABLE INITIALLY DEFERRED` permitem que, mesmo dentro de uma transação, a verificação
> de integridade só ocorra no COMMIT — evitando violação de FK durante a inserção.
>
> **C7 (schema):** sem o replay de migrations, todo backup viraria inútil após qualquer
> migration. O manifesto carrega a versão de origem; o restore aplica as migrations faltantes
> ao backup antes de gravar. Backups mais novos que o schema atual são recusados (não há
> "downgrade" de schema).

### Reset (wipe por-tenant)

```
1. POST /admin/tenants/:id/reset (confirmação dupla "RESET" + senha admin).
2. Encerra a sessão whatsmeow do tenant ANTES do delete:
   client.Disconnect() + apaga session store (evita corrupção).
3. DELETE FROM <t> WHERE tenant_id=$1 por tabela, na tenant-tx.
   NÃO usar TRUNCATE — TRUNCATE é table-level e atingiria TODOS os tenants.
4. Delete do prefixo S3 tenants/<id>/.
5. Audit log + WS broadcast (força logout dos agentes do tenant).
```

---

## 14. API REST e OpenAPI

### Pipeline

```
1. Spec autoral: api/openapi.yaml (source of truth)
2. make openapi-gen → oapi-codegen -generate types,server → api/openapi.gen.go
3. Handlers consomem tipos gerados + implementam ServerInterface
4. CI valida diff de openapi.gen.go (spec é contrato)
```

### Endpoints documentados

```
POST   /auth/oidc/login
GET    /messages
POST   /messages
PATCH  /messages/:id
DELETE /messages/:id
POST   /messages/:id/reactions
GET    /conversations
POST   /conversations/:id/assign
POST   /conversations/:id/resolve
GET    /channels/:channel/health
POST   /channels/:channel/credentials
GET    /channels/whatsmeow/qrcode
GET    /admin/services
POST   /admin/tenants
POST   /admin/tenants/:id/backup
POST   /admin/tenants/:id/restore
POST   /admin/tenants/:id/reset
```

> Webhooks Meta + Telegram ficam **fora** do OpenAPI (documentados em markdown), pois seu
> contrato é definido pelos providers, não por nós.

---

## 15. Painel web (rotas)

| Rota | Função | Auth |
|------|--------|------|
| `GET /setup` | Wizard inicial (só se 0 admins) | nenhuma |
| `GET /login` | Form de login | nenhuma |
| `POST /login` | Autentica admin/owner/agent | nenhuma |
| `GET /auth/oidc/login` | Inicia fluxo OIDC | session |
| `GET /auth/oidc/callback` | Callback OIDC | state+code |
| `GET /admin/` | Dashboard admin global | admin |
| `GET /admin/services` | Serviços + health + métricas | admin |
| `GET /admin/users` | Admins, owners, agents | admin |
| `GET /admin/tenants` | CRUD de tenants | admin |
| `GET /admin/tenants/:id/channels` | Configurar 5 canais | owner/admin |
| `GET /admin/tenants/:id/qrcode` | QR-code whatsmeow (refresh htmx) | owner |
| `GET /admin/tenants/:id/agents` | CRUD agentes + permissões | owner |
| `GET /admin/tenants/:id/backup` | Iniciar backup | owner/admin |
| `POST /admin/tenants/:id/reset` | Wipe (confirmação dupla) | owner/admin |
| `GET /app/` | Dashboard tenant (inbox) | owner/agent |
| `GET /app/conversations` | Lista de conversas | owner/agent |
| `GET /app/conversations/:id` | Thread de mensagens | owner/agent |
| `POST /app/conversations/:id/messages` | Enviar mensagem | owner/agent |
| `GET /app/channels` | Status dos 5 canais | owner |

### Padrões htmx

```
QR-code refresh:   hx-trigger="every 5s" enquanto não conectado
Backup progresso:  hx-get="/admin/backup/status" com polling
Inbox real-time:   hx-trigger="new-message from:body" via WS extension
Edit inline:       hx-put + hx-target
```

---

## 16. Estrutura de diretórios

```
mez-go-mono/
├── cmd/server/                  # serve + migrate + setup + rotate-kek (NOVO C9)
│   ├── main.go  migrate.go  setup.go  rotate_kek.go  wire.go
├── internal/
│   ├── core/                    # domain, event, port (SEM deps externas) + Sealer
│   ├── usecase/
│   │   ├── messaging/  routing/  outbox/         # ingest, send, relay + poll
│   │   ├── reconcile/           # NOVO (C1): reconciler inbound
│   │   ├── auth/  admin/  channels/  backup/     # admin inclui RunAsPlatform (C5)
│   ├── adapter/
│   │   ├── broker/              # bus tipado + política de saturação (C2)
│   │   ├── repository/postgres/ # RunInTenantTx + RunAsPlatform (C5) + FORCE RLS (C3)
│   │   ├── storage/s3/  idp/oidc/  meta/
│   │   ├── crypto/              # LocalSealer (envelope C9); VaultTransitSealer pós-1.0
│   │   └── provider/{waba,whatsmeow,instagram,messenger,tgbot}/
│   └── transport/{http,websocket,adminweb}/
├── pkg/{config,logger,media,crypto,health,metrics,qrcode}/
├── migrations/                  # 0001 com FORCE RLS + 3 roles + FKs deferíveis (C3/C5/C6)
├── api/{openapi.yaml,openapi.gen.go}
├── deployments/{Dockerfile,docker-compose.yml,entrypoint.sh}
└── configs/  go.mod  Makefile  AGENTS.md  CLAUDE.md  README.md
```

---

## 17. Quickstart

### Pré-requisitos

- Docker + Docker Compose
- Go 1.22+ (para desenvolvimento local)
- `templ`, `oapi-codegen`, `golang-migrate` (instalados via `make tools`)

### Subir o stack

```bash
# 1. Clonar
git clone https://github.com/felipedsvit/mez-go-mono.git
cd mez-go-mono

# 2. Gerar a master key (envelope encryption — C9)
export MEZ_MASTER_KEY=$(openssl rand -base64 32)

# 3. Subir postgres + minio + app
docker compose -f deployments/docker-compose.yml up -d

# 4. As migrations rodam no boot do app (migrate embed).
#    Acompanhar:
docker compose logs -f app

# 5. Bootstrap do admin global
open http://localhost:8080/setup
```

### Smoke test

```bash
curl -s http://localhost:8080/health   # → 200
curl -s http://localhost:8080/readyz   # → 200 quando DB + S3 prontos
```

> ⚠️ **`migrate` roda no boot** (D11). Se uma migration falhar, o container **não sobe** —
> isto é deliberado (fail-closed), mas significa que migrations destrutivas exigem janela de
> manutenção. Ver [§20](#20-operação-em-produção).

---

## 18. Configuração (variáveis de ambiente)

| Variável | Obrigatória | Default | Descrição |
|----------|:-----------:|---------|-----------|
| `MEZ_HTTP_ADDR` | — | `:8080` | endereço de bind do HTTP |
| `MEZ_DATABASE_URL` | ✅ | — | DSN do Postgres (role `mez_app`) |
| `MEZ_MIGRATE_DATABASE_URL` | ✅ | — | DSN do Postgres (role `mez_migrate`, owner) |
| `MEZ_PLATFORM_DATABASE_URL` | ✅ | — | DSN do Postgres (role `mez_platform`, bypass auditado) |
| `MEZ_MASTER_KEY` | ✅* | — | KEK base64 (32 bytes) para envelope encryption |
| `MEZ_MASTER_KEY_FILE` | ✅* | — | alternativa: caminho de arquivo com a KEK |
| `MEZ_S3_ENDPOINT` | ✅ | — | endpoint S3/MinIO |
| `MEZ_S3_BUCKET` | ✅ | `mezgo-media` | bucket de mídia |
| `MEZ_S3_BACKUP_BUCKET` | ✅ | `mezgo-backups` | bucket de backups |
| `MEZ_S3_ACCESS_KEY` | ✅ | — | credencial S3 |
| `MEZ_S3_SECRET_KEY` | ✅ | — | credencial S3 |
| `MEZ_OIDC_ISSUER` | ✅ | — | issuer OIDC (JWKS) |
| `MEZ_OIDC_CLIENT_ID` | ✅ | — | client ID OIDC |
| `MEZ_SESSION_SECRET` | ✅ | — | chave de assinatura de sessão |
| `MEZ_BUS_INBOUND_BUFFER` | — | `1024` | tamanho do buffer do tópico inbound |
| `MEZ_BUS_OUTBOUND_BUFFER` | — | `1024` | tamanho do buffer do tópico outbound |
| `MEZ_RECONCILE_INTERVAL` | — | `30s` | intervalo do reconciler (C1) |
| `MEZ_OUTBOX_POLL_INTERVAL` | — | `5s` | poll de fallback do relay (D3) |
| `MEZ_MAX_ACTIVE_TENANTS` | — | `100` | teto operacional de tenants ativos (limite de memória) |
| `MEZ_FFMPEG_CONCURRENCY` | — | `4` | semáforo global de transcoding |
| `MEZ_LOG_LEVEL` | — | `info` | nível de log (zerolog) |
| `MEZ_METRICS_ADDR` | — | `:9090` | endpoint Prometheus |

`*` Exatamente um de `MEZ_MASTER_KEY` ou `MEZ_MASTER_KEY_FILE` é obrigatório.

---

## 19. Build e desenvolvimento

### Makefile (alvos principais)

```bash
make tools         # instala templ, oapi-codegen, golang-migrate
make generate      # templ generate + oapi-codegen
make build         # compila o binário único
make test          # go test -race -shuffle=on ./...
make openapi-gen   # regenera api/openapi.gen.go
make migrate-up    # aplica migrations (local)
make docker        # build da imagem
make lint          # golangci-lint
```

### Critérios de build verde

- `make build` compila sem erro.
- `make test` passa com `-race` e `-shuffle=on` (detecta data races no bus e no whatsmeow
  dispatcher — críticos neste modelo).
- `make generate` não deixa diff (templ e openapi sincronizados).

### Testes

| Camada | Estratégia |
|--------|-----------|
| domain/usecase | unit, sem I/O |
| repository | **testcontainers** (Postgres real, valida RLS) |
| RLS isolation | teste de regressão fail-closed (ver [§8](#8-multi-tenancy-e-rls)) |
| backup round-trip | export → reset → restore → diff (valida C6/C7) |
| providers | mock client + golden files |
| whatsmeow | mock client (sessão simulada) |
| bus | concorrência + saturação (valida drop-safe + reconciler) |

---

## 20. Operação em produção

### Deploy é downtime total (C10)

Um binário com estado de sessão whatsmeow **não permite rolling deploy**: dois processos não
podem manter o mesmo client conectado, e o session store não suporta dois donos. Toda
atualização derruba todos os canais de todos os tenants.

- **Janela de manutenção** é necessária para qualquer deploy.
- **Mitigação operacional:** avisar tenants; agendar fora de pico; encurtar o tempo de boot
  (migrations rápidas; warm-up de clients whatsmeow paralelo).
- **Evolução pós-1.0:** multi-process por shard de tenant — mas isto é **reescrita** (WS hub
  in-memory e whatsmeow single-process travam o modelo), não um passo incremental.

### Migrations rodam no boot (C10)

`migrate` embed roda no start do container. Consequências:

- Migration que falha → container não sobe → outage. É fail-closed por design.
- **Migrations destrutivas** exigem cuidado: rodar em janela, com backup lógico prévio.
- Recomendação: migrations sempre **forward-only** e idempotentes onde possível.

### Observabilidade

- **Métricas Prometheus** em `MEZ_METRICS_ADDR`: bus (`bus_dropped_total` é o alerta-chave),
  outbox depth, reconciler lag, whatsmeow connection state por tenant, latência de provider.
- **Logs estruturados** (zerolog): toda ação admin e todo acesso `RunAsPlatform` em `audit_log`.
- **Health/readiness**: `/health` (liveness) e `/readyz` (DB + S3 prontos).

### Alertas recomendados

| Sinal | Condição | Significado |
|-------|----------|-------------|
| `bus_dropped_total` crescente | > 0 sustentado | buffer subdimensionado ou consumer lento; reconciler carregando folga |
| `reconciler_pending` alto | acima do baseline | processamento downstream atrasado |
| `outbox_pending` crescente | sustentado | provider degradado ou relay travado |
| `whatsmeow_disconnected` | por tenant | sessão caiu; possível ban ou rede |
| panic recuperado | qualquer | bug num dispatcher; investigar antes que escale |

---

## 21. Riscos e mitigações

Esta seção incorpora os **riscos de single-box** ausentes do plano original (C10), além dos
riscos já mapeados.

| Risco | Severidade | Mitigação |
|-------|:----------:|-----------|
| **Blast radius = 100%**: panic não recuperado derruba todos os tenants | alta | `recover()` por goroutine de dispatcher/tenant; testes com `-race`; circuit breaker por tenant |
| **Deploy = downtime total** (sem rolling) | alta | janela de manutenção; boot rápido; aceito como limitação do 1.0 |
| **`migrate` no boot vira outage** se falhar | média | migrations forward-only + backup prévio + teste de migration em staging |
| **WhatsMeow + IP único de saída** → risco de ban | média | egress dedicado por tenant pós-1.0; monitorar `whatsmeow_disconnected`; rate-limit de envio |
| **Contenção de pool Postgres** (backup/burst consome conexões) | média | pool separado para `mez_platform` e jobs de backup; limites por tenant |
| **ffmpeg global satura entre tenants** | média | semáforo (4) + worker pool; pós-1.0, cota por tenant |
| **Slow WS consumer** bloqueia broadcast | baixa | buffer por cliente + drop do cliente lento (best-effort; htmx reconecta) |
| **Memória escala linear com tenants ativos** (sem isolamento) | média | buffers bounded + `MEZ_MAX_ACTIVE_TENANTS`; multi-process pós-1.0 |
| In-process bus perde notificação em crash | baixa | **resolvido por C1**: reconciler reprocessa; DB é fonte da verdade |
| Inbound perdido entre commit e consumo | baixa | **resolvido por C1** (reconciler) |
| Meta webhooks unificados frágeis | baixa | `X-Hub-Signature-256` por app, fail-closed, log estruturado |
| Painel sem CSRF | baixa | middleware CSRF em todo POST/PUT/DELETE |
| Backup de S3 com objetos grandes estoura memória | baixa | stream `io.Pipe` + chunked upload |
| **RLS fail-open via owner** | **alta** | **resolvido por C3**: `FORCE RLS` + role app sem `BYPASSRLS` |
| **Vazamento cross-tenant pelo admin** | alta | `RunAsPlatform` dedicado e **auditado** (C5) |
| **Restore viola FK / colide sequence** | média | **resolvido por C6** (FKs deferíveis + ordem topológica) e UUID PKs |
| **Backup inútil após migration** | média | **resolvido por C7** (replay de migrations no restore) |

---

## 22. Reuso do mez-go (porte)

> ⚠️ **LOC não é proxy de esforço.** O grosso do código do pai foi desenhado em torno de um
> broker durável (NATS) com ack/nack, redelivery e consumo desacoplado. "Remover subjects NATS"
> **não é mecânico** quando o broker é transversal — toda a lógica de erro/retry que assumia
> essas garantias precisa ser **reescrita**, e ela atravessa os componentes marcados como
> "mecânicos". A contabilidade abaixo é honesta sobre isso.

### Porta (núcleo de mensageria, ~23.300 LOC medidas)

| Componente (pai) | LOC | Esforço real |
|------------------|----:|--------------|
| `core/{domain,event,port}` | 2.783 | mecânico (import path; remover subjects NATS) |
| `usecase/messaging` | 477 | mecânico |
| `usecase/routing` | 344 | mecânico |
| `outbox` | 1.290 | **reescrita parcial**: modelo de entrega NATS→channel muda (sem ack/redelivery) |
| `adapter/repository/postgres` | 3.899 | mecânico + **adicionar `FORCE RLS` e `RunAsPlatform`** |
| `adapter/storage/s3` | 96 | mecânico |
| `transport/http` | 1.748 | mecânico (handlers + middleware OIDC/apikey/csrf) |
| `transport/websocket` | 178 | mecânico |
| `transport/adminweb` | 3.729 | **lógica porta; `html/template` → `templ` é reescrita de apresentação + re-cabeamento htmx/WS** |
| `provider/waba` | 738 | mecânico |
| `provider/instagram` | 1.118 | mecânico |
| `provider/messenger` | 1.307 | mecânico |
| `provider/tgbot` | 1.565 | mecânico |
| `provider/whatsmeow` | 2.815 | **cirurgia**: multi-pool → 1 client/tenant toca reconexão/pairing/dispatcher |
| `pkg/*` | 1.244 | mecânico |
| `adapter/secret/sealer` | 510 | **revisto (C9)**: vira `LocalSealer` envelope; Vault Transit opcional |
| **Subtotal** | **~23.800** | + ~3.400 LOC de testes que também portam (com ajustes) |

### Substitui (é o débito; não porta)

| Item (pai) | LOC | Destino |
|------------|----:|---------|
| `adapter/broker` (NATS JetStream) | 455 | **bus in-process** (~400-600 LOC novos) |
| 5 binários extras em `cmd/` | — | colapsam em `cmd/server` único |

### Descarta (over-scope V2/V3 fora do 1.0)

`usecase/{analytics,crm,automation,campaigns,marketplace}` ≈ **1.900 LOC** não portadas.

> 🔧 **Correção de contabilidade (C9):** o plano original listava o Vault sealer
> simultaneamente em "porta" (§11.1, 510 LOC) e em "descarta" (§11.3). Esta revisão resolve:
> o `Sealer` **porta** como `LocalSealer` (envelope local); o **backend Vault Transit** é que
> fica fora do 1.0. Não há dupla contagem.

### Genuinamente novo (~3.500-5.500 LOC)

- Bus in-process tipado + política de saturação (~400-600).
- **Reconciler inbound** (C1) (~300-500). *Novo nesta revisão.*
- `cmd/server` único: serve + migrate + setup + rotate-kek + wiring Fx (~400-500).
- Rewrite `templ` + re-cabeamento htmx/WS (~1.500-2.000).
- Backup/restore/reset lógico com FK deferida + replay de migration (~1.000-1.500). *Único bloco
  sem precedente no pai; ampliado por C6/C7.*
- `FORCE RLS` + `RunAsPlatform` + teste de regressão fail-closed (~200-400). *Novo (C3/C5).*

---

## 23. Roadmap e estimativas

> 🔧 **Reestimativa (C11/C12).** O plano original (23-32 dias solo) assumia "porte mecânico"
> cobrindo a troca do broker — que é transversal e exige reescrita. Esta revisão reestima em
> **35-50 dias úteis** para dev solo, com whatsmeow e backup realocados e uma **fase de
> estabilização do processo único** adicionada.

### Fase 0 — Esqueleto + bootstrap (2-3 dias) [em andamento]

- Módulo Go, Makefile, config, logger, health.
- `cmd/server` com serve + migrate + setup.
- Docker multi-stage (ffmpeg + libwebp + templ).
- `docker-compose.yml`: postgres + minio + app.
- **Migration 0001 com `FORCE RLS` + roles (`mez_migrate`/`mez_app`/`mez_platform`) + FKs deferíveis (C3/C5/C6).**
- `core/{domain,event,port}` com 5 canais.
- Bus in-process com política de saturação (C2).
- Templ base + htmx; wizard `/setup` funcional.

### Fase 1 — Auth + admin (3-4 dias)

- Login local admin (Argon2id) + session cookies.
- OIDC JWKS verifier.
- `/admin/*` middleware + `RunAsPlatform` auditado (C5).
- CRUD tenants + tenant owners.
- Audit log; OpenAPI inicial + `oapi-codegen`.

### Fase 2 — Pipeline inbound (4-5 dias) *(+1 dia: reconciler)*

- Repos postgres com `RunInTenantTx`.
- Outbox table + relay + poll de fallback.
- **Reconciler inbound (C1)** + estados `received/routed/notified`.
- Webhook Meta unificado (verif assinatura, fail-closed).
- WABA/IG/MSG/TG: mapper + ingestor (persist+dedup → 2xx → bus).
- API: `GET /conversations`, `GET /messages`.
- Testes testcontainers + **teste de regressão RLS fail-closed**.

### Fase 3 — Pipeline outbound (3-4 dias)

- Sender com capability resolve + fallback.
- Clientes WABA/IG/MSG/TG.
- API: `POST /messages`; status pipeline.

### Fase 4 — WhatsMeow (6-8 dias) *(realocado de 4-5)*

- `pkg/media` (ffmpeg/cwebp, semáforo).
- Session store em postgres.
- `whatsmeow.Manager` (1 client/tenant) — **reescrita do pool**.
- Dispatcher (buffers bounded) + **`recover()` por goroutine (C10)**.
- Send: text/image/audio/sticker/video; actions: reaction/edit/revoke/mark_read/typing/presence.
- AutoReconnect + graceful `Disconnect()`.

### Fase 5 — Painel completo (5-6 dias) *(realocado de 4-5)*

- `/app/*` (inbox, conversas, send).
- `/admin/services`, `/admin/users`, `/admin/tenants/:id/channels`.
- QR-code whatsmeow (PNG + htmx refresh).
- WS real-time na inbox; CSRF middleware.
- **Re-cabeamento htmx/WS sobre os templates reescritos.**

### Fase 6 — Backup/Restore/Reset (5-7 dias) *(realocado de 2-3)*

- Export lógico (COPY-por-tenant, tx `REPEATABLE READ`, stream S3).
- **Restore idempotente com ordem topológica + FKs deferidas (C6) + replay de migrations (C7).**
- Reset (confirmação dupla); painel com progresso; audit log.

### Fase 7 — Hardening (2-3 dias)

- Envelope encryption (DEK/tenant) + `rotate-kek` (C9).
- CI: build-and-test, openapi-validate, govulncheck.
- Documentação final.

### Fase 8 — Estabilização do processo único (3-4 dias) *(NOVO — C12)*

- Ordem de boot determinística (migrate → sealer init → pools → bus → reconciler → providers → HTTP).
- **Graceful shutdown coordenado**: signal → parar aceitar HTTP → drain WS → bus drain → relay flush → whatsmeow `Disconnect()` por tenant.
- Teste de chaos: kill -9 em pontos críticos; validar que reconciler e outbox recuperam.
- Teste de boot frio com N tenants (warm-up paralelo de whatsmeow).

### Total

| Estimativa | Dias úteis | Semanas (solo) |
|------------|:----------:|:--------------:|
| Plano original | 23-32 | 5-6 |
| **Revisado** | **35-50** | **7-10** |

Maior incerteza: **Fase 4 (whatsmeow)** e **Fase 6 (backup)**, os dois blocos com mais
reescrita e menos precedente direto no pai.

---

## 24. Definition of Done

- [ ] `make build` verde em CI.
- [ ] `make test` verde em CI (`-race` + `-shuffle=on`).
- [ ] `docker compose up` sobe postgres + minio + app.
- [ ] `curl /health` retorna 200; `curl /readyz` retorna 200.
- [ ] Wizard `/setup` cria admin.
- [ ] OIDC login funciona com IdP configurado.
- [ ] 5 canais recebem webhooks (quando configurados).
- [ ] Painel renderiza todas as rotas listadas.
- [ ] QR-code whatsmeow é gerado e atualiza via htmx.
- [ ] Backup gera arquivo no bucket; **restore round-trip valida igualdade (C6/C7)**.
- [ ] Reset wipe com confirmação dupla funciona.
- [ ] OpenAPI spec bate com handlers (CI valida).
- [ ] `govulncheck` passa.
- [ ] **Teste de regressão RLS fail-closed passa (C3/C4).**
- [ ] **`RunAsPlatform` gera audit log em todo acesso cross-tenant (C5).**
- [ ] **Reconciler recupera mensagens órfãs após `kill -9` (C1).**
- [ ] **Outbox drena no boot via poll de fallback (D3).**
- [ ] **`recover()` por dispatcher comprovado: panic de um tenant não derruba o processo (C10).**
- [ ] **`rotate-kek` re-wrap de todos os DEKs sem perda (C9).**
- [ ] Documentação (AGENTS.md, CLAUDE.md, README) atualizada.

---

## 25. Limitações conhecidas

Estas são **decisões conscientes** do modelo single-box 1.0, não bugs:

1. **Sem zero-downtime deploy.** Toda atualização é janela de manutenção (estado whatsmeow).
2. **Blast radius = processo.** Mitigado por `recover()`, mas um bug não tratado num dispatcher
   pode derrubar todos os tenants.
3. **Memória escala com tenants ativos**, sem isolamento entre eles. Teto operacional via
   `MEZ_MAX_ACTIVE_TENANTS`.
4. **IP de saída compartilhado** para whatsmeow — risco de ban concentrado. Egress dedicado é
   pós-1.0.
5. **Backup DB↔S3 não é atômico.** Snapshot do DB é consistente; a mídia no S3 não está no
   mesmo ponto no tempo.
6. **Restore não faz downgrade de schema.** Backups mais novos que o schema atual são recusados.
7. **WS é best-effort.** Não há entrega garantida no broadcast; clientes reconectam e dão
   polling htmx.
8. **Vault Transit fora do 1.0.** A crypto é local; quem exige KMS externo gerenciado deve
   esperar o `VaultTransitSealer` pós-1.0.

Tudo o que escala além de single-box (multi-process, sharding por tenant, egress dedicado, KMS
externo) é **pós-1.0**, e algumas dessas evoluções são reescritas, não passos incrementais —
em particular o WS hub e o whatsmeow single-process.

---

*README atualizado nesta revisão com as correções C1–C12. Em caso de divergência entre este
documento e o código, **vale o código** (matriz de capacidades, contratos OpenAPI e policies RLS
são validados em CI).*
