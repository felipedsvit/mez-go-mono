# Decisão 004 — Domain Events (item 3.3 do review 001): **wontfix-1.0**

> **Origem:** análise de prós e contras do item 3.3 da `001_DDD_HEXAGONAL_REVIEW.md`.
> **Data:** junho/2026.
> **Status:** **NÃO IMPLEMENTADO no 1.0.** Reabrir quando multi-worker entrar no roadmap.
> **Issue de tracking:** <https://github.com/felipedsvit/mez-go-mono/issues/128> (criada junho/2026)

---

## 1. Contexto

O review DDD-Hex §3.3 apontou como anti-pattern 🟠 o fato de que o `core/event` carrega apenas **envelopes de transporte** (`InboundEvent`, `OutboundEvent`, `StatusEvent`, `LifecycleEvent`, `DLQEvent`) — não **eventos de domínio**. O domínio não emite nada. O bus é alimentado por código hand-picked no `Ingestor` e no `SenderService`, e o `Reconciler` (excelente peça de resiliência) varre o DB a cada 30s para detectar drift.

A recomendação do review: distinguir `core/event` (envelope) de `core/domain/events` (eventos de domínio: `MessageRouted`, `ConversationAssigned`, `ConversationResolved`, `CredentialsRotated`). O bus continuaria publicando envelopes; handlers de domínio converteriam envelope → domain event. O review marcou isso como "longo prazo, 1 sprint".

Esta nota documenta a decisão de **não implementar no 1.0** e o racional.

---

## 2. Estado atual (verificado no código)

| Peça | Onde | Comportamento atual |
|---|---|---|
| Reconciler | `usecase/reconcile/reconciler.go` | Varre `received` no boot + tick 30s. `FOR UPDATE SKIP LOCKED`, batch 100, error-tolerant, idempotente (status update é dedup natural). **Fail-safe.** |
| Bus | `adapter/broker/bus.go` | 5 canais buffered (1024/1024/256/256/64). `select default → drop` em **todas** as publishes (`bus.go:88-94`, `97-108`, `111-121`, `126-136`, `139-149`). `safeCall` recupera panic. `Drain` espera goroutines. **Best-effort, drop-on-full.** |
| Ingestor | `usecase/messaging/ingest.go` | DB write dentro de `RunInTenantTx`, **depois** chama `bus.PublishInbound`. Dual-write (DB + bus) não é atômico — Reconciler cobre. |
| Domain (após #127) | `core/domain/{conversation,message,...}.go` | `Conversation.Resolve/Assign/NewInboundMessage` e `Message.MarkRouted/MarkNotified` são mutações puras. **Não emitem nada.** |
| OutboxMessage (após #126) | `core/domain/outbox.go` | FSM Pending/Claimed/Sent/Failed/DLQ. Tabela `outbound_events` é uma stream materializada de intenções de envio. |

---

## 3. Pontos positivos de implementar

1. **Fecha o anti-pattern 🟠** que o review cita nominalmente.
2. **Reconciler vira subscriber**, não varredor. O loop de 30s e o `SelectUnroutedMessages` deixam de ser a fonte primária; a mudança para `routed`/`notified` chega via evento. Reconciler vira só **catch-up de bootstrap**.
3. **Desacopla side-effects.** `Conversation.Resolve` hoje retorna `error` e ponto. Com evento, a mesma chamada pode triggar notificação, fan-out, audit, métricas — sem o AR saber de nenhum deles.
4. **DDD puro.** "Aggregate publica domain events em resposta a transições de estado" é a definição canônica.
5. **Audit trail grátis.** Cada transição de estado vira um evento; `outbound_events` (ou uma nova `domain_events`) já é o log de auditoria de domínio.
6. **Habilita 3.8 de verdade** (consolidação `outbound_events` ↔ `messages`): quando outbox é stream de domain events, a separação histórica deixa de fazer sentido.
7. **Testabilidade.** Handlers testáveis dando eventos sintéticos, sem setup completo de usecase.
8. **Múltiplos subscribers gratuitos.** `Resolve` pode acionar analytics, ACD hint, webhooks externos, sem refactor.
9. **Alinha com mez-go (pai).** O pai publica `MessageRouted` no bus; o mono diverge.
10. **Prepara multi-worker.** Quando o mono for shardado (Fase 5+), o bus in-process vira limitante; ter o pattern pronto facilita a transição para NATS/JetStream sem reescrever o domínio.

---

## 4. Pontos negativos / custos

1. **Esforço alto (1 sprint segundo o review).** Não é "adicionar 3 tipos" — é:
   - Novo package `core/domain/events`.
   - Mudança de assinatura em `Conversation.Assign/Resolve`, `Message.MarkRouted/MarkNotified` para retornar `[]DomainEvent`.
   - `EventBus` interface em `port` + implementação **at-least-once** (a atual drop-on-full não serve).
   - Transactional outbox: persistir evento **na mesma tx** da mutação (resolve dual-write, exige migration).
   - Reconciler reescrito: subscriber + sweep de bootstrap.
   - Ingestor: emitir `MessageRouted` quando o relay confirma entrega.
   - Subscription infra (retry policy, dead-letter de eventos de domínio — separado do DLQ de canal).
   - Atualizar ~10 tests que assumem "mutação retorna só error".

2. **O bus atual é unreliable (drop-on-full).** Trocar a dependência do Reconciler para esse bus é **regredir**. Quem implementar 3.3 precisa **primeiro** promover o bus a confiável, e isso por si só é ~30% do trabalho.

3. **Bootstrap é inevitável.** Eventos só existem enquanto o processo roda. Após restart, há mensagens em estados intermediários que nunca emitiram evento. **Reconciler continua necessário como catch-up.** O ganho é marginal se a frequência do Reconciler não cair.

4. **Eventual consistency vira visível.** Hoje "MarkRouted, depois search index atualiza" (síncrono). Com evento, "MarkRouted, talvez o index atualize em breve, talvez não, depende de quantos subscribers e de quão cheios os buffers." Adiciona carga cognitiva, races latentes.

5. **Idempotência obrigatória nos subscribers.** Como o bus atual (e mesmo um futuro confiável) pode reentregar, cada subscriber tem que deduplicar. O Reconciler não tinha esse problema — `WHERE status='received'` é dedup natural.

6. **Refactor de superfície ampla.** `Conversation.Assign` hoje é puro sync. Com evento, todo call site precisa decidir o que fazer com `[]DomainEvent`. `ListService.AssignConversation` (criado em #126) precisa mudar. `SenderService.Send` precisa emitir `MessageNotified`. Ingestor precisa emitir `MessageRouted` quando o relay confirma — mas a confirmação é **out-of-process** (provider webhook), então o evento viria do handler, não do Ingestor.

7. **Debugging mais lento.** "Mensagem X está com status errado" hoje = query + call stack. Com eventos, vira "qual subscriber processou, em que ordem, houve drop?" Logs do bus existentes já não dão causalidade.

8. **Perda de fail-safe do Reconciler atual.** O ponto forte do design atual é que o Reconciler polling **sempre** converge mesmo com bugs no publisher. Migrar para event-driven (mesmo com catch-up) introduz a classe de bug "publisher caiu, ninguém percebeu, dados parados."

9. **Cuidado com o limite do skill.** O review é explícito: "Não introduzir CQRS. Não introduzir Event Sourcing." Implementar 3.3 mal é a porta de entrada para event sourcing ("para reproduzir, lê os eventos"). A tentação de "agora que temos eventos, vamos guardar todos" é forte.

10. **Risco de regressão alto.** A peça central (Ingestor, Reconciler, SenderService) é o que mantém a Fase 3 estável. Mudar assinaturas + adicionar coordenação tem alto risco de bug em produção. O timing (já temos Fase 7 merged com hardening de envelope encryption) não é o melhor.

11. **Custo de subscriber overhead.** Cada `Resolve` dispara N handlers (audit, metrics, notification, analytics). Se algum handler for lento, o AR fica lento.

12. **Custo de migração de testes.** Os tests do `domain` que escrevi em #127 (`TestConversation_Assign`, `TestMessage_MarkRouted` etc.) **não testam eventos**. Para 3.3, cada um vira "fazer mutação, capturar eventos emitidos, asserir no slice retornado." ~12 tests a atualizar + novos para eventos.

---

## 5. Risco-chave destacado

**R1: O bus atual é drop-on-full em todas as 5 publishes (`bus.go:88-94`, `97-108`, `111-121`, `126-136`, `139-149`).** Substituir polling por evento sobre esse bus é regredir, não progredir. Quem for implementar 3.3 deve começar **promovendo o bus a confiável** (ou criando `core/port.DomainEventBus` à parte com semântica at-least-once), e isso por si só é ~30% do trabalho total estimado.

---

## 6. Caminhos viáveis (se a decisão for revertida no futuro)

### A. Mínimo (~3-5d)
`core/domain/events` com tipos puros. AR methods retornam `[]DomainEvent` (sem publicar nada). `DomainEventBus` interface em `port` com **uma única implementação in-process + sync**. Reconciler **continua polling** (caminho fica pronto arquiteturalmente, mas nenhum subscriber real existe ainda).
- **Pró:** barato, prepara o terreno.
- **Contra:** zero benefício operacional até alguém escrever subscribers.

### B. Completo (~1-2 sprints)
Tudo de A + bus confiável (ou outbox transacional) + Reconciler vira subscriber com sweep de bootstrap + Ingestor/SenderService emitem eventos nas transições + tests atualizados.
- **Pró:** cumpre literalmente o que o review pede.
- **Contra:** alto custo, alto risco de regressão, e se o bus não for confiável é ilusório.

### C. Cirúrgico (~5-7d)
Apenas `ConversationResolved` + `MessageRouted` quando o Reconciler processa (evento **após** a query, sem reescrever o Reconciler). Reconciler vira "poll + emit". Reconciliação agora vira fonte de eventos sem perder a fail-safe.
- **Pró:** baixo risco, ganha auditoria/observabilidade.
- **Contra:** não fecha o anti-pattern; é meio-termo.

---

## 7. Decisão

**Não implementar 3.3 no 1.0.** Justificativa:

1. **Custo/benefício desfavorável na escala atual.** 1 sprint para o caminho completo, com benefício marginal: o Reconciler polling é fail-safe e suficiente para 100s de tenants em single-process.
2. **Pré-condição ausente: bus confiável.** Implementar 3.3 sobre o bus atual (drop-on-full) é regredir, não progredir. Promover o bus é trabalho separado, fora do escopo 1.0.
3. **Reabrir em Fase 5+.** Quando multi-worker entrar no roadmap, o bus precisará ser confiável de qualquer jeito (NATS/JetStream ou similar), e o pattern de domain events será natural.

### Pré-condições para reabrir a discussão

- [ ] Bus confiável (at-least-once, persistência, retry policy). Sem isso, 3.3 é promessa falsa.
- [ ] Roadmap multi-worker (Fase 5+) iniciado.
- [ ] Decisão de **consolidação da tabela `outbound_events`** (3.8 do review) tomada — é pré-requisito natural.
- [ ] Análise atualizada de R1 (bus confiável) concluída.

### Não-objetivos (explícitos)

- **CQRS / Event Sourcing:** proibidos por recomendação do review.
- **Tabela `domain_events` dedicada no 1.0:** não criar.
- **Refactor de assinaturas do domain** (`Conversation.Assign` etc.) para retornar `[]DomainEvent`: diferido. Custo alto, sem consumer.

---

## 8. Issue de tracking

A issue **#128** rastreia esta decisão. Comentários são bem-vindos se a análise aqui mudar.

---

*Última atualização: junho/2026.*
