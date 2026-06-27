# Revisão Clean Architecture + DDD + Hexagonal — mez-go-mono

> **Data:** junho/2026 · **Escopo:** avaliação do 1.0 (Fases 0–8 merged) sob a ótica de Clean Architecture (regra de dependência), DDD (modelagem tática) e Hexagonal (ports/adapters)
> **Persona:** Go architect (simplicidade, explicitude, push-back contra abstração prematura)

## Verdict em uma linha

A **infraestrutura hexagonal está correta e a regra de dependência é respeitada** (domain → usecase → adapter → transport, sem inversão). Mas a **modelagem de domínio é anêmica**, o que faz a "arquitetura limpa" virar "arquitetura limpa mas com use cases fazendo o trabalho do domain layer". E há **dois anti-patterns textuais do skill** (Skipping Use Cases + Anemic Domain Model) que devem ser corrigidos juntos ou não fazem sentido isoladamente.

---

## 1. Mapeamento para a estrutura do skill

| Skill | mez-go-mono | Nota |
|---|---|---|
| `domain/` | `internal/core/domain/` + `internal/core/admin/` + `internal/core/event/` | Duas bounded contexts (messaging + admin) — bom. `core/event` é envelope de transporte, não domain events. |
| `application/` | `internal/usecase/` | Existe; subdividido por área (messaging, backup, secrets, reconcile, …). |
| `infrastructure/` | `internal/adapter/` | Boa divisão (broker, repository, provider, storage, webhook, cache, crypto, auth). |
| `main` | `cmd/server/` | Único entrypoint + subcomandos. |
| Ports (driven) | `internal/core/port/` | Bem definidos, com compile-time checks. |

A camada `core/` cumpre três papéis ao mesmo tempo (domain, event envelopes, port). Isso é defensável (mantém imports pequenos) mas confunde "domain event" com "transport envelope".

---

## 2. O que está correto

### 2.1 Regra de dependência é respeitada
`internal/core/domain` importa só `time` da stdlib. Verificado:
```
$ grep -h '"github' internal/core/domain/*.go
(zero resultados)
```
Zero dependências externas. **Domain está puro.** Isso é a parte mais valiosa do desenho.

### 2.2 Bounded contexts existem, ainda que só parcialmente
`core/domain` (messaging) e `core/admin` (identity/audit) têm modelos separados — inclusive dois tipos `Tenant` com shapes diferentes:

- `core/domain.Tenant` (`tenant.go:8-15`): `ID TenantID, Name, Slug, Active`
- `core/admin.Tenant` (`admin/tenant.go:19-26`): `ID string, Name, Slug, Status TenantStatus`

Isto é **literalmente o que o skill recomenda** ("Bounded Context" — modelos diferentes em contextos diferentes). Bom. O `core/admin` é a parte mais DDD-correta do repo: tem `Role.HasPermission`, `User.IsActive`, `AuditFilter`, factory `NewTenant(name, slug)` com validação de slug. Comportamento no domain.

### 2.3 Compile-time interface checks
`var _ port.OutboxWriter = (*OutboxRepo)(nil)` em `outbox.go:39-40` e o pattern similar em `whatsmeow/stub_client.go:26`. Está onde importa.

### 2.4 Ports têm uma única responsabilidade clara
`port.Sender` (envio), `port.SenderRegistry` (descoberta per-tenant), `port.Resolver` (capability matrix), `port.Sealer`/`Encryptor` (crypto), `port.TxRunner` (RLS). Nenhuma porta é "god port".

### 2.5 Use cases *orquestram*, não *implementam*
`SenderService.Send` (`usecase/messaging/send.go:74`) e `ucbackup.Service.Export` (`usecase/backup/export.go:112`) são transações compostas: persistem + enfileiram + notifificam. É o que o application layer deveria fazer.

### 2.6 Driver adapter (transport) chama use case
`adminweb/handlers_backup.go:56` chama `s.backup.Export(...)`. `api/handlers.go:213` chama `h.sender.SendAction(...)`. **Para escritas**, o caminho é correto: transport → use case → port.

---

## 3. Anti-patterns encontrados

### 3.1 🔴 Anemic Domain Model (CRÍTICO)

O skill lista como o primeiro anti-pattern a evitar. **Mez-go-mono é um caso clássico.**

`domain.Message` (`domain/message.go:8-22`):
```go
type Message struct {
    ID, TenantID, Channel, ConversationID, ContactID ...
    Direction, Type, Status, Body, ProviderMsgID, Metadata ...
    CreatedAt, UpdatedAt time.Time
}
```
Zero métodos. Não há `Message.MarkRouted()`. Não há `Message.New(...)`. Não há `Message.IsInbound()`. Não há invariantes (e.g., "outbound messages always have a ProviderMsgID after Sent").

`domain.Conversation` (`domain/conversation.go:8-17`): idem. Não há `Conversation.Open()`, `Conversation.Assign(agent)`, `Conversation.Resolve()`.

`domain.Contact` (`domain/contact.go:7-16`): idem. Não há `Contact.UpsertFrom(channel, peerID)`.

Compare com `core/admin/role.go:49-58`:
```go
func (r Role) HasPermission(perm Permission) bool { ... }
func (r Role) IsPlatform() bool { ... }
```
`core/admin` tem **comportamento no domain**. `core/domain` não. Mesma equipe, dois padrões.

A consequência é o que o `Ingestor` faz em `usecase/messaging/ingest.go:107-167`: 60 linhas de "transaction script" — abre tx, faz upsert do contact, upsert da conversation, insert da message, insert do outbox. Tudo no use case. **A conversation nunca sabe que tem mensagens; a message nunca sabe que está numa conversation.** São 4 tabelas coordenadas sem aggregate root.

**Como deveria ser** (esboço):
```go
// domain/conversation.go
type Conversation struct { ... }
func (c *Conversation) Open(channel, externalID) error { ... }
func (c *Conversation) Assign(agentID) error { ... }
func (c *Conversation) Resolve() error { ... }
func (c *Conversation) NewInboundMessage(...) (*Message, error) { ... }

// domain/message.go
type Message struct { ... }
func (m *Message) MarkRouted() error { ... }
func (m *Message) MarkNotified() error { ... }
```

**Severidade: alta.** Sem isso, o resto da "arquitetura limpa" é cosmético.

### 3.2 🔴 Skipping Use Cases (CRÍTICO) — *parcial*

O skill lista como anti-pattern: "Controllers call repositories directly in a use-case architecture".

Mez-go-mono mistura os dois padrões:

| Endpoint | Caminho | Veredito |
|---|---|---|
| `POST /api/messages` | transport → `SenderService.Send` (usecase) | ✅ |
| `POST /api/messages/{id}/reactions` | transport → `SenderService.SendAction` | ✅ |
| `PATCH /api/messages/{id}` | transport → `SenderService.SendAction` | ✅ |
| `DELETE /api/messages/{id}` | transport → `SenderService.SendAction` | ✅ |
| `POST /api/conversations/{id}/assign` | transport → `SenderService.SendAction` | ✅ |
| **`GET /api/conversations`** | transport → **`h.convRepo.ListByTenant(...)`** | 🔴 |
| **`GET /api/messages`** | transport → **`h.msgRepo.ListByConversation(...)`** | 🔴 |
| `GET /api/channels/{channel}/health` | transport → `senderReg.Health(...)` | ⚠️ (port, ok se justificado) |
| `POST /admin/tenants/{id}/backup` | transport → `ucbackup.Service.Export` | ✅ |

`api/handlers.go:95` e `:114` chamam `port.ConversationRepo` e `port.MessageRepo` direto. As writes vão via use case; as reads não. Resultado: se amanhã "listar conversas" precisar de filtragem por agente, deduplicação, ouvalidação cross-tenant, isso vai vazar para o handler.

**Fix:** criar `usecase/messaging.ListConversations(ctx, tenantID, filter)` e `usecase/messaging.ListMessages(ctx, tenantID, convID)`. Não é grande.

### 3.3 🟠 Domain Events ausentes

`internal/core/event/event.go` define `InboundEvent`, `OutboundEvent`, `StatusEvent`, `DLQEvent`, `LifecycleEvent`. **Esses são envelopes de transporte, não domain events** — carregam `TenantID, Channel, MessageID` e nada mais. O bus publica/recebe esses envelopes.

Um domain event no sentido DDD carrega o que **aconteceu** (`MessageRouted`, `ConversationResolved`, `CredentialsRotated`) com dados de negócio. Hoje a única forma de saber "uma mensagem foi roteada" é inferir da mudança de status no DB. Não há `events.MessageRouted{...}` com subscribers no domínio.

Consequência prática: o `Reconciler` (excelente peça de resiliência) varre o DB por mensagens em status errado em vez de reagir a um evento. A diferença é:
- **Sem domain event:** o reconciler é uma *correção* que precisa de polling.
- **Com domain event:** o reconciler seria uma *garantia* sobre um evento que *deveria* ter sido consumido.

O outbox + reconciler é uma boa compensação pragmática — **mas só funciona se o domain model expuser o evento**. Senão o "sistema de eventos" é só o bus, e o bus é só um canal de transporte.

**Recomendação:** distinguir `core/event` (envelope) de `core/domain/events` (eventos de domínio: `MessageRouted`, `ConversationAssigned`, `CredentialsRotated`). O bus pode continuar publicando envelopes; os handlers de domínio podem converter envelope → domain event.

### 3.4 🟠 Capability factories no port layer

`internal/core/port/resolver.go:73-132` define `CapabilitiesWABA()`, `CapabilitiesInstagram()`, `CapabilitiesMessenger()`, `CapabilitiesTelegram()`, `CapabilitiesWhatsMeow()`. São funções que **retornam o que cada adapter suporta**.

O skill diz: "ports declaram interfaces; adapters implementam". Capability matrix é uma propriedade do **adapter**, não do port. A linha atual amarra o port ao snapshot de hoje — se WABA ganhar `templates` v2, o port muda.

Compare: `wire.go:184-189` já registra as capabilities no boot, lendo-as dos adapters. As factories no port são **redundantes**.

**Fix:** deletar as 5 factories de `port/resolver.go`; cada adapter expõe `Capabilities()` e o wire registra.

### 3.5 🟠 `MemorySenderRegistry` (concreto) no package `port`

`internal/core/port/sender_registry.go:43-128` define `MemorySenderRegistry` (concreto, com `sync.RWMutex`, `zerolog.Logger`, cache TTL). Port deveria ter só a interface `SenderRegistry`. Hoje qualquer teste que importe `port` carrega o logger.

**Fix:** mover `MemorySenderRegistry` para `internal/adapter/sender/registry/` ou similar; manter só a interface em `port/`.

### 3.6 🟠 Cross-context leak: `OutboxRepo` lê a tabela `tenants` do admin context

`internal/adapter/repository/postgres/outbox.go:241-271`:
```go
SELECT id FROM tenants WHERE active = true ORDER BY created_at
```
Isso é uma query cross-context dentro do adapter de messaging. Dois problemas:
1. O `Tenant` no contexto de messaging é `core/domain.Tenant` (com `Active bool`); o `Tenant` no contexto admin é `core/admin.Tenant` (com `Status TenantStatus`). A query assume a tabela do admin context, com a coluna `active` do admin context.
2. Mudou a coluna no admin (e.g., de `active` para `status='active'`) e o relay quebra silenciosamente.

**Fix:** ou (a) expor uma port cross-context (`port.TenantEnumerator`) que o admin adapter implementa, ou (b) aceitar que tenants são "infraestrutura compartilhada" e mover a query para um package neutro (`internal/adapter/repository/shared/`).

### 3.7 🟠 God transaction no `Ingestor.Ingest`

`usecase/messaging/ingest.go:107-167` toca 4 tabelas (`contacts`, `conversations`, `messages`, `outbound_events`) numa única transação sem aggregate root definido. Se o skill pergunta "should this be its own aggregate?", a resposta aqui é "sim, é um aggregate — mas o modelo não diz qual".

Cenário: duas instâncias de mez-go-mono recebem webhooks do mesmo contact em paralelo. Ambas fazem `upsert contact + open conversation + insert message + insert outbox` ao mesmo tempo. Sem aggregate root, **o que serializa?** A unique constraint em `(tenant_id, channel, external_id)`? Talvez — mas isso é sorte, não design.

**Recomendação:** declarar `Conversation` como aggregate root. `Conversation.NewMessage(...)` carrega o contact e o channel; é a conversation que sabe o que é "uma mensagem pertence a esta thread". O contact vira reference-by-ID fora do aggregate (cross-aggregate, eventual consistency).

### 3.8 🟠 "Outbox" e "Message" são duas tabelas paralelas

`usecase/messaging/send.go:110-122`:
```go
if err := s.outbox.Insert(ctx, &msg); err != nil { ... }
if err := s.repo.Insert(ctx, &msg); err != nil { ... }
```
A message vai para `messages` E para `outbound_events`. São dois armazenamentos. O `outbound_events.payload` é um JSONB com `message_id` denormalizado.

A pergunta DDD é: **o outbox é parte da message, ou é um conceito separado?** Hoje é separado (duas tabelas, dois ports, `OutboxWriter` ≠ `OutboxRelay`). Mas semanticamente, "a mensagem foi enfileirada para envio" é uma transição de estado da própria mensagem. Outbox deveria ser um **status** da message + uma fila de retries, não uma entidade paralela.

**Recomendação:** consolidar. `Message.Status` ganha `Enqueued`, `Claimed`, `Sent`, `Failed`, `DLQ`. `outbox` vira uma view/tabela de retries indexada por `(status, next_attempt_at)`. Um único repo: `MessageRepo.Enqueue`, `MessageRepo.Claim`, `MessageRepo.MarkSent`, `MessageRepo.MarkFailed`, `MessageRepo.MarkDLQ`. O `port.OutboxWriter` e `port.OutboxRelay` colapsam em `port.MessageRepo`.

Se a equipe discorda (e pode haver razão para manter paralelo por causa do volume de retries), **ao menos** o domain model precisa refletir isso: criar um aggregate `OutboxMessage` que *referencia* `Message` por ID, não duplicar os campos.

### 3.9 🟡 Repository per Entity vs per Aggregate

`port/repository.go` separa `ContactRepo`, `ConversationRepo`, `MessageRepo` (3 repos para 3 entidades). O skill diz "Repository per Aggregate, not per Entity". Se o aggregate é `Conversation` (raiz), deveria haver um `ConversationRepo` que carrega junto o contact e a message sob demanda. **A separação atual é CRUD thinking.**

Trade-off: em Go, é comum ter um repo por tabela porque as queries SQL são diferentes. O skill reconhece isso implicitamente: "Pick one convention per codebase". O problema real é que **não há aggregate declarado**, então não há como saber qual é o "per aggregate" aqui.

### 3.10 🟡 `domain.ChannelCredentials` é um modelo de infraestrutura, não de domínio

`domain/credentials.go:23-32` é uma linha de tabela (wrapped_dek, encrypted, kek_version, rotation_window_until). Esses campos são **detalhes de implementação** do envelope encryption. O domínio não precisa saber que a credencial tem um `wrapped_dek` — só que "existe uma credencial por (tenant, channel)" e que ela é cifrada.

**Fix:** mover `ChannelCredentials` para `internal/core/port/` (como `CredentialRow` que já existe em `port/repository.go:75-81`) ou para `internal/usecase/secrets/` (já que só o Keyring usa).

### 3.11 🟡 `appQFromCtx` silenciosamente faz fallback para o pool

`internal/adapter/repository/postgres/db.go:29-34`:
```go
func appQFromCtx(ctx context.Context, pool *pgxpool.Pool) querier {
    if tx, ok := ctx.Value(appTxKey).(pgx.Tx); ok {
        return tx
    }
    return pool
}
```
O comentário diz "fall back to pool for queries executed outside a tenant transaction". Isso é **acoplamento implícito** entre o adapter e a convenção do use case ("sempre passe por RunInTenantTx"). Se um use case esquecer, o adapter roda sem RLS — e o erro do Postgres ("missing mez.tenant_id") é opaco.

Não é furo de Clean Architecture (dependência aponta para dentro), mas é furo de **fail-closed**: o `mez_app` role sem `BYPASSRLS` torna o erro *eventual* (no SELECT), não *imediato* (no use case). **Renomear para `appQFromCtxOrPool` e marcar com `// UNSAFE`** deixa o erro mais óbvio.

### 3.12 🟡 `OutboxRepo.ForEachTenant` materializa a lista em memória

`internal/adapter/repository/postgres/outbox.go:250-257`:
```go
var tenants []string
for rows.Next() { tenants = append(tenants, id) }
for _, tid := range tenants { fn(...) }
```
Já reportado na revisão anterior. Aqui, a perspectiva DDD: o `ForEachTenant` itera o agregado `Tenant` mas materializa a coleção. **Streamar** (`for rows.Next() { fn(tid) }`) é o que um aggregate-aware repository faria.

### 3.13 🟡 Duplicação de `ErrCredentialsNotFound` em 3 lugares

`secrets/keyring.go:36`, `webhook/secrets/credentials.go:26`, `webhook/secrets/resolvers.go:25` (`ErrNotConfigured`). Dívida menor — unificar em `port`.

---

## 4. O que está bom e deve ser preservado

- **Regra de dependência:** o domain não importa nada externo. Excelente.
- **Bounded contexts (parcial):** `core/domain` vs `core/admin` é DDD correto.
- **`core/admin` é a referência interna:** `Role.HasPermission`, `User.IsActive`, `Tenant.NewTenant` com validação. Comportamento no domain. Use esse package como exemplo para `core/domain`.
- **Ports e compile-time checks:** `var _ port.X = (*Y)(nil)` onde importa.
- **Use cases de escrita** (`SenderService`, `ucbackup.Service`, `ucmessaging.Ingestor`, `ucoutbox.Relay`) são orquestrações reais, não CRUD.
- **Lifecycle.Runner** coordena boot/shutdown com phases explícitas. **Excelente**.
- **Reconciler** é o exemplo mais claro de "infraestrutura para garantir uma garantia de domínio". Manter.
- **goleak.VerifyTestMain** em 7 packages críticos. Higiene.

---

## 5. Resumo executivo

| Severidade | Item | Local |
|---|---|---|
| 🔴 Anti-pattern | Anemic Domain Model | `core/domain/{message,conversation,contact,tenant}.go` |
| 🔴 Anti-pattern | Skipping Use Cases (read paths) | `transport/http/api/handlers.go:95, 114` |
| 🟠 Refactor | Domain Events ausentes | `core/event/event.go` (todos os tipos) |
| 🟠 Refactor | Capability factories no port | `port/resolver.go:73-132` |
| 🟠 Refactor | `MemorySenderRegistry` no port | `port/sender_registry.go:43-128` |
| 🟠 Refactor | Cross-context leak (outbox lê `tenants`) | `postgres/outbox.go:241-271` |
| 🟠 Refactor | God transaction no Ingestor (sem aggregate) | `usecase/messaging/ingest.go:107-167` |
| 🟠 Refactor | Outbox e Message como tabelas paralelas | `usecase/messaging/send.go:110-122` + `postgres/outbox.go:47-80` |
| 🟡 Limpar | Repository per Entity vs per Aggregate | `port/repository.go` |
| 🟡 Limpar | `ChannelCredentials` no domain | `domain/credentials.go` |
| 🟡 Limpar | `appQFromCtx` silent fallback | `postgres/db.go:29-34` |
| 🟡 Limpar | `ForEachTenant` materializa | `postgres/outbox.go:241-271` |
| 🟡 Limpar | `ErrCredentialsNotFound` triplicado | 3 arquivos |
| ✅ OK | Domain puro (zero deps externas) | `core/domain/*` |
| ✅ OK | Bounded contexts messaging vs admin | `core/{domain,admin}/` |
| ✅ OK | Compile-time port checks | vários |
| ✅ OK | Use cases de escrita | `usecase/**/send.go, ingest.go, service.go, relay.go` |

---

## 6. Recomendação de sequência

O Anemic Domain Model e o Skipping Use Cases são a mesma ferida vista de dois lados: se o domain não tem comportamento, o use case vira CRUD script; se o use case vira CRUD script, o transport acaba chamando repo direto. Corrigir um sem o outro é cosmético.

**Sequência sugerida:**

1. **Curto prazo (1-2 dias):** corrigir as 2 violações de `transport → repo` direto. Criar `usecase/messaging.ListConversations` e `ListMessages`. O domain continua anêmico mas o fluxo fica Clean.
2. **Médio prazo (1 sprint):** introduzir comportamento mínimo no domain. `Message.MarkRouted()`, `Conversation.Assign(agent)`, `Conversation.Resolve()`, `Message.NewInbound(channel, peerID)`. O Ingestor e o Reconciler viram chamadores desses métodos.
3. **Médio prazo:** decidir se "outbox" é estado da message ou agregado separado. Consolidar em torno do agregado escolhido.
4. **Longo prazo:** introduzir domain events (`MessageRouted`, `ConversationAssigned`, `CredentialsRotated`) que o bus carrega como envelopes; o Reconciler vira subscriber de evento, não varredor de DB.

Não introduzir CQRS. Não introduzir Event Sourcing. O skill é explícito: "Most systems don't need full CQRS or Event Sourcing". Mez-go-mono não precisa.

---

*Última atualização: junho/2026. Fases 0–8 merged.*
