# ADR 0017 — D15: Sem prefixo de versão na API (`/messages`)

* **Status:** Aceita (mantida + confirmada)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D15](../../README.md#5-decisões-arquiteturais)

## Contexto

Versionamento de API REST é um debate clássico. As alternativas:

1. **Path versioning** — `/v1/messages`, `/v2/messages`. Cada
   versão tem handlers separados. Trade-off claro: você pode
   sunsetar `/v1` matando um path.
2. **Header versioning** — `Accept: application/vnd.mez.v2+json`.
   URLs limpas, versionamento via content negotiation. Mais
   difícil de testar com curl.
3. **Sem versão** — `/messages` direto. Breaking change = bump
   major do produto (semantica-ish, mas para API).

## Decisão

Adotamos a opção 3: **sem prefixo de versão na URL**.

A primeira versão pública **é** `/messages`, `/conversations`,
`/contacts`, etc. Breaking changes futuros serão tratados por:

- **Major bump do projeto** (mez-go-mono 1.x → 2.x) comunicado
  via changelog + 6 meses de deprecation notice.
- **Migration path** documentada para cada breaking change
  (ex.: "campo `metadata.user_id` foi removido; use
  `metadata.user_ref`").
- **Dois binários side-by-side** se a quebra for inevitável:
  `mez-go-mono v1` e `mez-go-mono v2` rodando em paralelo
  atrás de um reverse proxy que roteia por header.

## Consequências

### Positivas

- **URLs limpas:** o operador não precisa lembrar de `/v3` em
  todos os requests.
- **Menos burocracia:** PRs que adicionam campos opcionais
  não precisam esperar major bump. Versionamento "natural" do
  JSON.
- **Compatibilidade com cliente HTTP simples:** qualquer
  `http.Get` funciona. Sem negociação de Accept complexa.
- **Forward-compat por construção:** campos opcionais podem
  ser ignorados por clientes antigos sem quebrar.

### Negativas

- **Breaking change é custoso:** quando inevitável, o operador
  precisa migrar todas as integrações. Mitigado por fase de
  deprecation (campo antigo continua, novo é adicionado, log
  avisa, depois remove).
- **Sem `/v1` para testes:** QA não consegue apontar para
  "versão 1 estável" vs "versão 2 em staging". Mitigado por
  environments separados (staging vs prod) e tags git.
- **Clientes podem não seguir changelog:** mesmo com aviso,
  alguém vai usar campo removido e quebrar. Mitigado por
  `Accept: application/json; schema=v2` opcional (negociação
  leve) — implementação pós-1.0.

## Notas de implementação

Arquivos relevantes:

- `api/openapi.yaml` — paths sem prefixo de versão
- `internal/transport/http/server/server.go` — rotas
  `/messages`, `/conversations`, etc
- `docs/api-changelog.md` (a criar pós-1.0) — lista de
  breaking changes com migration path
