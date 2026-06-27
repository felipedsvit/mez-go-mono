# ADR 0014 — D12: OpenAPI gerado por oapi-codegen + CI valida diff

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D12](../../README.md#5-decisões-arquiteturais)

## Contexto

API REST documentada e tipada é requisito de qualquer produto sério.
As alternativas:

1. **Hand-written handlers + doc em Swagger UI à parte** — o `oapi-codegen`
   gera apenas types, e os handlers são escritos manualmente. Drifts
   entre spec e implementação são comuns.
2. **Spec-first: escrever `openapi.yaml`, gerar types e server stub
   via `oapi-codegen`** — handlers implementam o stub gerado. Spec
   é a fonte da verdade; CI valida que o `openapi.gen.go` está em
   sync com o yaml.
3. **Code-first: implementar handlers, gerar spec automaticamente**
   — várias libs (ex.: swaggo/swag) fazem isso, mas o spec gerado
   é "o que o código faz", não "o que o código deveria fazer". Spec
   vira documentação reativa.

## Decisão

Adotamos a opção 2: **spec-first via `deepmap/oapi-codegen`**.

- `api/openapi.yaml` é a **fonte da verdade**. Editores: Humanos
  revisam PRs no yaml.
- `make openapi-gen` roda `oapi-codegen -generate types,server
  -package api api/openapi.yaml > api/openapi.gen.go`.
- `make openapi-validate` (Fase 7 #93) roda `openapi-gen` e falha
  se o `api/openapi.gen.go` mudou → garante que o PR inclui
  o regen.
- O servidor HTTP `internal/transport/http/server/server.go`
  implementa as interfaces geradas (`ServerInterface`).

## Consequências

### Positivas

- **Spec é contrato:** consumidor da API (frontend, integrações
  externas) lê o yaml, não o código. Mudança de assinatura
  requer mudança de spec primeiro.
- **Tipos consistentes:** request/response têm Go types estáticos.
  Sem `map[string]any`. Sem `json.RawMessage` espalhado.
- **Validação automática:** oapi-codegen gera middlewares de
  validação de path/query/body contra o spec. Erro 400 antes
  do handler ser chamado.
- **CI pega drift:** `make openapi-validate` falha o build se
  alguém modificou o handler sem regerar.

### Negativas

- **Workflow mais verboso:** editar endpoint = (1) yaml,
  (2) `make openapi-gen`, (3) implementar handler. Não é hot-reload.
- **Limitações do oapi-codegen com OpenAPI 3.1:** warning hoje
  (lido no CI). Pós-1.0 podemos migrar para 3.0.x ou esperar
  o suporte oficial.
- **YAML verbose:** spec completa tem ~600 linhas. Mitigado por
  `$ref` entre componentes.
- **Cuidado com discriminator:** oneOf/anyOf no yaml geram
  unions em Go, que são awkward (`type Message struct { ... }` com
  campos opcionais). Padrão atual: evitar discriminators,
  preferir campos opcionais com `omitempty`.

## Notas de implementação

Arquivos relevantes:

- `api/openapi.yaml` — spec fonte
- `api/openapi.gen.go` — gerado, **não editar manualmente**
- `api/oapi-codegen.yaml` — config do gerador
- `Makefile:60-65` — `openapi-gen` target
- `Makefile:67-71` — `openapi-validate` target (Fase 7 #93)
- `internal/transport/http/server/server.go` — implementa
  `ServerInterface`
