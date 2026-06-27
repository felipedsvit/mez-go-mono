# ADR 0018 — D16: Session cookies HttpOnly + CSRF token em forms

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D16](../../README.md#5-decisões-arquiteturais)

## Contexto

O painel admin é web-based. As alternativas de auth:

1. **Bearer token em `Authorization` header** — seguro, mas
   inadequado para browser (precisa de JS para guardar o token
   e adicionar em cada request). Sem progressive enhancement.
2. **Session cookie HttpOnly + Secure + SameSite=Lax** — clássico
   web. Funciona sem JS. Vulnerável a CSRF se não houver
   proteção adicional.
3. **Bearer token + cookie dupla** — para `/admin` (cookie) e
   para `/api` (token). Mais complexo, mas flexível.

## Decisão

Adotamos a opção 2 com **proteção CSRF obrigatória**:

- Cookie: `__Host-mez_admin` (prefixo `__Host-` força Secure,
  sem Domain, Path=/).
- Atributos: `HttpOnly; Secure; SameSite=Lax; Path=/`.
- TTL: 24h (configurável via `MEZ_SESSION_TTL`).
- **CSRF token** em form hidden (`<input name="csrf" value="...">`).
  Validado em todo POST/PUT/DELETE/PATCH.
- **Double-submit cookie** alternativo: o token é setado em
  cookie `mez_csrf` (não-HttpOnly) E em form hidden. O
  middleware compara ambos. Sem state no server.

Para a API REST (não-web), não há CSRF (não há cookie). Auth
via `Authorization: Bearer <token>` para integrações externas.

## Consequências

### Positivas

- **Progressive enhancement:** form POST funciona sem JS. Login
  e navegação básica não dependem de framework JS.
- **HttpOnly mitiga XSS stealing:** mesmo se um atacante injetar
  JS, ele não consegue ler o cookie via `document.cookie`.
- **`__Host-` prefix mitiga subdomain takeover:** o cookie só
  é aceito se (a) Secure, (b) Path=/, (c) sem Domain. Subdomain
  comprometido não consegue forjar.
- **CSRF protection via double-submit:** state-less, escala
  horizontalmente sem session store distribuído.
- **OWASP 2024 compliance:** atende o top 10 de session
  management.

### Negativas

- **CSRF em forms adiciona boilerplate:** todo `<form>` precisa
  do `<input name="csrf">`. Mitigado por helper `templ.CSRFField(csrfToken)`.
- **Logout explícito necessário:** cookie expira em 24h, mas
  usuário pode querer logout imediato. Implementado em
  `POST /admin/logout`.
- **CSRF + login cross-origin problemático:** se o admin UI
  está em `app.mez.com` e o API em `api.mez.com`, o cookie
  cross-origin precisa de `SameSite=None; Secure`. Mitigado
  por rodar tudo atrás do mesmo hostname (subpath `/admin`
  e `/api` no mesmo origin).
- **CSP recomendado complementar:** Session cookies não são
  defesa contra XSS. Recomenda-se Content-Security-Policy
  strict. Pós-1.0.

## Notas de implementação

Arquivos relevantes:

- `internal/transport/adminweb/middleware/csrf.go` — CSRF
  middleware com double-submit
- `internal/transport/adminweb/middleware/session.go` —
  session cookie config
- `internal/transport/adminweb/handler_login.go` — seta o
  cookie + CSRF
- `internal/transport/adminweb/handler_logout.go` —
  invalida session + limpa cookie
- `internal/core/admin/session.go` — `SessionStore` interface
