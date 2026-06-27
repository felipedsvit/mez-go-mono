# ADR 0008 — D6: Outbound action-aware

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D6](../../README.md#5-decisões-arquiteturais)

## Contexto

Operações outbound em mensageria não são apenas "enviar mensagem".
Cada canal expõe ações como: `reaction` (reações a mensagens),
`edit` (editar mensagem enviada), `revoke` (apagar), `mark_read`
(marcar como lida), `typing` (indicador "digitando..."), `presence`
(online/offline).

As alternativas:

1. **API única `Send(text)`** — cada provider implementa o mínimo.
   Operações avançadas viriam "forçadas" via `text` malformado
   (ex.: "/react 👍 msgid=..."). Acoplamento ruim.
2. **API rica `Send(ctx, Action)`** com Action sendo um union type
   (sum type) que carrega os campos relevantes por variante.
   Provider implementa o que sabe; capability negotiation (D7)
   cobre o que não sabe.

## Decisão

Adotamos a opção 2: **`domain.Message` carrega `Type` (text/image/
reaction/edit/...) e `Metadata` (campos específicos por tipo)**. O
sender (`port.Sender`) recebe `Message` e decide como despachar
para o canal.

Capacidades de cada canal ficam em `port.CapabilitySet` (D7, ADR 0009):

- `text`, `image`, `audio`, `video`, `document` — mensagens
  básicas
- `reaction` — emoji reagir a uma msg
- `edit` — editar msg enviada (requer `provider_msg_id`)
- `revoke` — apagar msg
- `mark_read` — marcar como lida
- `typing` — indicador
- `presence` — status online

Fallback (D7): se o canal não suporta `reaction`, o `SenderService`
tenta `text` com o emoji (ou falha gracioso).

## Consequências

### Positivas

- **Paridade de capacidades:** o admin UI pode mostrar uma matriz
  "este canal suporta X" sem esconder features. Operador sabe
  upfront o que funciona.
- **Sender único:** o relay outbox despacha via `SenderRegistry` +
  `SenderService` independente do canal. Não há `if channel ==
  WABA` espalhado pelo código.
- **Adicionar ação nova é centralizado:** novo tipo → 1 enum +
  1 capability flag + 1 case no SenderRegistry.

### Negativas

- **Metadata cresce:** campos opcionais por tipo incham o JSONB.
  Aceitável — é metadata, sem impacto em queries indexadas.
- **Provider precisa implementar todas as capabilities que declara:**
  se `CapabilitiesWABA()` lista `reaction` mas o provider não
  implementa, a chamada falha em runtime. Mitigado por testes
  de capability matrix.
- **Capability drift:** se um canal atualiza API e adiciona
  `threading`, precisamos atualizar o capability set. Processo
  manual, mas acontece raramente.

## Notas de implementação

Arquivos relevantes:

- `internal/core/domain/types.go:44-68` — `MessageType` enum
- `internal/core/port/sender.go` — interface `Sender.Send(ctx, *Message)`
- `internal/core/port/capability.go` — `CapabilitySet` flags
- `internal/usecase/messaging/sender_service.go` — orquestra o
  `SenderRegistry` com fallback
