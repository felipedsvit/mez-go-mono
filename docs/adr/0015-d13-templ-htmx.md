# ADR 0015 — D13: templ + htmx (HDA, sem build JS)

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D13](../../README.md#5-decisões-arquiteturais)

## Contexto

O painel admin (`/admin/...`) precisa de UI. As alternativas:

1. **SPA React/Vue/Svelte** com build JS, API REST por baixo.
   - Prós: UX rica, ecossistema grande.
   - Contras: bundle JS grande, latência de boot, hydration, build
     pipeline adicional (webpack/vite), versionamento de API.
2. **Server-rendered HTML + htmx** (Hypermedia-Driven Application).
   - Prós: zero build JS, sem hydration, progressive enhancement
     (funciona sem JS), simplicidade operacional.
   - Contras: UX menos "rica" (modais complexos, drag-drop são
     mais chatinhos).
3. **templ + htmx + Alpine.js** para pequenos adornos.
   - templ gera Go code type-safe para HTML (sem string concat).
   - htmx faz partial page updates sem JS framework.
   - Alpine para interatividade client-side que htmx não cobre
     (dropdown menus, toggles).

## Decisão

Adotamos a opção 3: **templ + htmx + Alpine.js esparso**.

- Templates `.templ` são type-safe; `templ generate` produz
  `.templ.go` com `Render` method.
- htmx attributes (`hx-get`, `hx-post`, `hx-target`, `hx-swap`,
  `hx-trigger`) fazem chamadas parciais; o servidor responde
  com HTML fragment, não JSON.
- Alpine.js carregado via CDN único (~14KB gzip), usado APENAS
  para interatividade que htmx não cobre naturalmente (dropdowns,
  toggles de tema, confirmações inline).

O HTML servido tem `<script src="htmx.min.js">` (CDN) e
`<script defer src="alpine.min.js">` (CDN). Sem build step
no CI para o frontend.

## Consequências

### Positivas

- **Zero build JS:** `make serve` sobe sem `npm install` nem
  `node_modules`. Operador com Go + Postgres + S3 + browser
  tem o painel completo.
- **Latência de página:** TTFB é a página inteira (Go render).
  Sem FCP/LCP de SPA. Sem waterfall de JS.
- **Progressive enhancement:** se o JS do htmx falhar ao carregar,
  os forms tradicionais (`<form method="POST">`) continuam
  funcionando. Fallback natural.
- **Templates type-safe:** erro de digitação em `class` é
  erro de compilação, não de runtime.

### Negativas

- **Menos ecossistema de componentes:** não tem "shadcn para
  templ". Equivalente é copiar templates próprios.
- **htmx requer mentalidade diferente:** desenvolvedor acostumado
  com React precisa reaprender (sem estado local, sem
  `useEffect`, etc.). Onboarding mais lento.
- **Tailwind opcional:** o mono não usa Tailwind por padrão;
  CSS é escrito à mão em `static/admin.css`. Mais verboso,
  mais opinativo. Aceitável.
- **Alpine.js para coisas simples:** o que seria `useState` em
  React vira `x-data="{ open: false }"`. Funcional, mas
  limitado.

## Notas de implementação

Arquivos relevantes:

- `internal/transport/adminweb/templates/` — `.templ` files
- `internal/transport/adminweb/server.go` — handler que render
  templ components
- `static/admin.css` — CSS escrito à mão
- `static/htmx.min.js` / `static/alpine.min.js` — pinned,
  opcionalmente CDN
- `Makefile:60-62` — `templ-gen` target
