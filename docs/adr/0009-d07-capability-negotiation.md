# ADR 0009 — D7: Capability negotiation + fallback media→text

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D7](../../README.md#5-decisões-arquiteturais)

## Contexto

Cada canal tem capacidades diferentes:

- WABA suporta texto, imagem, áudio, vídeo, documento, template,
  reaction.
- Telegram suporta tudo do WABA + sticker + edit + revoke.
- Whatsmeow (via whatsmeow) suporta texto, imagem, áudio, vídeo,
  documento — sem template, sem reaction estável.
- Messenger suporta texto, imagem, áudio, vídeo, template.
- Instagram suporta texto, imagem, áudio, vídeo (story mention).

Se o relay outbox recebe uma mensagem com `type=sticker` para um
canal que não suporta, três alternativas:

1. **Falhar** — operador precisa configurar tipo correto. Ruim
   para resiliência.
2. **Enviar como `text`** com nota "sticker não suportado neste
   canal" — degrada a experiência, mas a mensagem chega.
3. **Capability matrix no sender** — antes de enviar, consulta
   `Channel.Capabilities()`; se não suporta, aplica fallback
   `media→text` automático.

## Decisão

Adotamos a opção 3:

- Cada `Channel` expõe `Capabilities() CapabilitySet` (bitmask
  de flags).
- O `SenderService` consulta a capability ANTES de chamar
  `Sender.Send`. Se a capability requerida está ausente, aplica
  fallback:

| Capability requerida | Fallback |
|----------------------|----------|
| `image` (sem `image` capability) | `text` com URL da imagem |
| `audio` | `text` com link do áudio |
| `video` | `text` com link do vídeo |
| `document` | `text` com nome + link |
| `sticker` | `text` com nome do sticker |
| `template` | erro (sem fallback — template é obrigatório) |
| `reaction` | no-op silencioso |
| `edit` | erro (não dá para "editar" sem ter enviado) |
| `revoke` | no-op silencioso |
| `mark_read` | no-op silencioso |
| `typing` | no-op silencioso |

O fallback é registrado no log com `level=info` e tag
`fallback=media_to_text` para que o operador veja degradações
via log/métrica.

## Consequências

### Positivas

- **Resiliência:** o relay não falha por mismatch de capability.
  Mensagens degradam (perdem o sticker, viram texto com link) mas
  chegam.
- **Operador vê degradações:** o log/métrica permite identificar
  "este tenant tem 50 fallbacks/dia para messenger" e decidir
  entre (a) aceitar, (b) ajustar o template do canal.
- **Capability matrix testada:** a tabela acima vira teste de
  regressão. Adicionar capability nova = adicionar caso.

### Negativas

- **Fallback pode confundir o usuário final:** um sticker vira
  texto com "🎉" — não é o ideal, mas é melhor que a mensagem
  sumir.
- **Templates não têm fallback:** template é obrigatório em
  WABA para mensagens proativas (fora da janela de 24h). Sem
  fallback, falha vai para DLQ. Aceitável — DLQ permite
  reprocessar após o operador resolver.
- **Configuração implícita:** o operador precisa olhar os logs
  para saber que está havendo fallback. Documentado; pós-1.0
  dashboard.

## Notas de implementação

Arquivos relevantes:

- `internal/core/port/capability.go` — `CapabilitySet` bitmask
- `internal/core/port/sender.go` — interface `Sender.Capabilities()`
- `internal/usecase/messaging/sender_service.go` — fallback
  `media→text`
- `internal/core/port/sender_registry.go` — `MemorySenderRegistry`
  que guarda factories + capabilities
