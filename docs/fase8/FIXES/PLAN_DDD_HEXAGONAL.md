# DDD / Hexagonal — Plano de Execução da Revisão 001

> Origem: `docs/fase8/FIXES/001_DDD_HEXAGONAL_REVIEW.md` (junho/2026).
> Escopo aprovado: 12 dos 13 itens. **Exceto 3.3 (Domain Events)** — **`wontfix-1.0`** conforme decisão documentada em `004_DOMAIN_EVENTS_DECISION.md` (issue #128).

## Status Final

| Item | Issue | Status | Resumo |
|---|---|---|---|
| 3.4 | #120 | ✅ | 5 capability factories movidas para adapters |
| 3.5 | #121 | ✅ | `MemorySenderRegistry` movida para `internal/adapter/sender/memory` |
| 3.9 | #124 | ✅ | Convenção documentada no port (repo per entity, AR=Conversation) |
| 3.10 | #118 | ✅ | `ChannelCredentials` deletado de `core/domain`; `port.CredentialRow` é o canônico |
| 3.13 | #119 | ✅ | `ErrCredentialsNotFound` consolidado em `port` (1 só) |
| 3.6 | #122 | ✅ | `port.TenantEnumerator` + `adapter/repository/postgres/tenant_enumerator.go` |
| 3.11 | #123 | ✅ | `appQFromCtx` → `appQFromCtxOrPool` (UNSAFE marker) |
| 3.12 | #123 | ✅ | `ForEachTenant` streaming (via `TenantEnumerator.ForEachActive`) |
| 3.1+3.7 | #125 | ✅ | `Conversation` como AR; `NewConversation`, `Assign`, `Resolve`, `NewInboundMessage`, `MarkRouted`, `MarkNotified`, `MarkClaimed`, `MarkSent`, `MarkFailed`, `MarkDLQ` |
| 3.2 | #126 | ✅ | `usecase/messaging.ListService` com `ListConversations`, `ListMessages`, `AssignConversation`, `ResolveConversation` |
| 3.8 | #126 | ✅ | `domain.OutboxMessage` (FSM) + `port.OutboxWriter.Enqueue`; tabela mantém paralela (decisão consciente) |
| **3.3** | **#128** | **wontfix-1.0** | **Domain events: análise em `004_DOMAIN_EVENTS_DECISION.md`. Reabrir quando bus confiável + multi-worker (Fase 5+).** |

## Tabela de Execução

| Item | Issue alvo | Arquivos | Tipo | Esforço | Bloqueado por |
|---|---|---|---|---|---|
| **3.10** | Mover `ChannelCredentials` do domain para `port` | `core/domain/credentials.go` (deletar) + `core/port/repository.go` (já tem `CredentialRow`, reusar) | MECHANICAL | 0.3d | — |
| **3.13** | Unificar `ErrCredentialsNotFound` (3 cópias) em `port` | `port/repository.go` (novo) + `secrets/keyring.go` + `webhook/secrets/{credentials,resolvers}.go` | MECHANICAL | 0.3d | — |
| **3.4** | Mover `CapabilitiesXxx()` do port para os adapters | `port/resolver.go` (deletar 5 funcs) + 5 `adapter/provider/*/...` (já existem, só mudam para definir internamente) | MECHANICAL | 0.3d | — |
| **3.5** | Mover `MemorySenderRegistry` para `adapter/sender/registry` | `port/sender_registry.go` (manter só interface) + novo `adapter/sender/registry/registry.go` | MECHANICAL | 0.5d | — |
| **3.9** | Documentar convenção "repo per entity mas 1 AR" | só comentário nos 3 repos | DOC | 0.1d | — |
| **3.6** | Cross-context leak: criar `port.TenantEnumerator`, mover query | `port/repository.go` (nova interface) + novo `adapter/repository/postgres/tenant_enumerator.go` + `outbox.go` ForEachTenant (substituir) | ADAPT | 0.5d | — |
| **3.11** | `appQFromCtx` → `appQFromCtxOrPool` + marker UNSAFE | `postgres/db.go` (rename + comentário) | MECHANICAL | 0.1d | — |
| **3.12** | `ForEachTenant` streaming (sem materializar) | `postgres/outbox.go:241-271` (loop em rows direto) | MECHANICAL | 0.2d | — |
| **3.1+3.7** | Comportamento no domain + `Conversation` como AR | `core/domain/{conversation,message,contact,tenant}.go` (métodos), `usecase/messaging/ingest.go` (regras) | REWRITE | 1.5d | — |
| **3.2** | Use cases de leitura (ListConversations, ListMessages) | novo `usecase/messaging/list.go` + `transport/http/api/handlers.go` (substituir chamadas diretas) | ADAPT | 0.5d | 3.1+3.7 |
| **3.8** | `OutboxMessage` agregado referenciando `Message` por ID (sem duplicar campos) | `core/domain/outbox.go` (novo) + `port/repository.go` (`OutboxWriter` ajustado) + `usecase/messaging/send.go` + `usecase/outbox/relay.go` (revisar) | ADAPT | 1.0d | 3.1+3.7 |

**Total:** ~5.3d (1 sprint)

## Decisões Arquiteturais

1. **Aggregate root (3.7):** `Conversation` é o AR do subdomínio "thread". `Message` é entidade dentro do AR. `Contact` é reference-by-ID (cross-aggregate, eventual consistency).
2. **Comportamento no domain (3.1):**
   - `Conversation.NewInboundMessage(...)` retorna `(*Message, error)`. O `Ingestor` chama este método em vez de construir `Message` cru.
   - `Message.MarkRouted()` / `Message.MarkNotified()` mutam `Status` apenas se a transição é válida (FSM guard).
   - `Conversation.Assign(agentID string)` / `Conversation.Resolve()` mudam estado e disparam domain event.
3. **Domain events (3.3 — DEFERRED):** as assinaturas dos métodos do domain retornam `[]DomainEvent` ao invés de publicar via bus. O Ingestor coleta e o bus (futuro) os traduz em envelopes. Isso prepara o terreno para 3.3 sem introduzir o bus de eventos ainda.
4. **`OutboxMessage` (3.8 — parcial):** introduzimos o tipo `domain.OutboxMessage{ID, MessageID, Channel, TenantID, Attempts, NextAttemptAt, LastError}`. O `OutboxWriter` ganha um método `Enqueue(ctx, *OutboxMessage)` que substitui o atual `Insert(ctx, *Message)`. O relay continua usando `OutboxMessage` e hidrata `Message` sob demanda. **Não consolidamos a tabela** (decisão consciente: o review aceita manter paralelo "se há razão para volume de retries"). A consolidação física virá junto com 3.3.
5. **`TenantEnumerator` (3.6):** interface cross-context em `port`. Implementação no `adapter/repository/postgres`. O `OutboxRepo.ForEachTenant` injeta o enumerator em vez de ler a tabela diretamente.
6. **Limpezas (3.11, 3.12, 3.13):** mecânicas, zero impacto comportamental.

## Definition of Done (geral)

- [ ] Nenhum transport chama `port.Repo` direto para leitura de conversa/mensagem
- [ ] `core/domain.Message` e `core/domain.Conversation` têm métodos de comportamento
- [ ] `Conversation.NewInboundMessage` é a única forma de criar uma `Message` inbound a partir do usecase
- [ ] `CapabilitiesXxx()` não vive em `port` (cada adapter exporta a sua)
- [ ] `MemorySenderRegistry` não vive em `port`
- [ ] `OutboxRepo.ForEachTenant` usa `port.TenantEnumerator` (não lê `tenants` direto)
- [ ] `domain.ChannelCredentials` deletado, `port.CredentialRow` é o canônico
- [ ] `ErrCredentialsNotFound` em 1 lugar só
- [ ] `appQFromCtxOrPool` marcado `// UNSAFE` e renomeado
- [ ] `ForEachTenant` não materializa lista
- [ ] `go test -race -shuffle=on ./...` verde para todos os packages afetados

## Não-Objetivos (explícitos)

- **3.3 Domain Events completo:** **`wontfix-1.0`**. Análise completa em `004_DOMAIN_EVENTS_DECISION.md` (issue #128). Pré-condições para reabrir: bus confiável + roadmap multi-worker (Fase 5+).
- **CQRS / Event Sourcing:** proibidos por recomendação do review.
- **Movimento de `ChannelCredentials` para `usecase/secrets/`:** optei por `port` (já tem `CredentialRow`), evitando novo package.
- **Refactor de `ConversationRepo.UpdateStatus` para usar `Conversation.Resolve`:** feito junto com 3.2 (handler resolve → use case → AR method).
- **Testes de propriedade:** unit tests com `testing` são suficientes.
