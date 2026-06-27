# ADR 0013 — D11: `migrate` subcommand com golang-migrate embed

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D11](../../README.md#5-decisões-arquiteturais)

## Contexto

Migrations em DB precisam ser executadas de forma ordenada e
idempotente. As alternativas:

1. **Binário externo `migrate` (golang-migrate CLI)** — o operador
   instala via `go install` e roda `migrate -source file://migrations
   -database $DSN up`. Adiciona dependência externa fora do binário
   do mono.
2. **Subcomando `migrate` no próprio binário do mono** — `mez-go-mono
   migrate up/down/version/force`. O CLI do golang-migrate é
   importado como library.
3. **Auto-migrate no boot** — o `serve` aplica migrations pendentes
   antes de aceitar tráfego. Conveniente, mas perigoso (boot
   parcial se migration falha).

## Decisão

Adotamos a opção 2: **subcomando `migrate` com a library
`github.com/golang-migrate/migrate/v4`**.

- `mez-go-mono migrate up` — aplica todas as pendentes
- `mez-go-mono migrate down 1` — reverte 1
- `mez-go-mono migrate version` — mostra versão atual
- `mez-go-mono migrate force <v>` — força versão (recovery)

A library `migrate` é embed-friendly: `embed.FS` em
`migrations/` é passada como source. O binário do mono é
**autocontido** — sem dependência de CLI externo.

**NÃO** fazemos auto-migrate no boot. O operador roda
explicitamente, e o `serve` falha fast se a versão do schema
não bate com a esperada (C7: refuse upgrade).

## Consequências

### Positivas

- **Binário único:** `mez-go-mono` faz tudo. Sem
  `go install golang-migrate/migrate` no operador.
- **Versionamento explícito:** `migrate version` é um comando
  de leitura; útil em troubleshooting.
- **Recovery forçado:** `migrate force <v>` permite resolver
  estado "dirty" (migration parcialmente aplicada) sem
  hack no DB.
- **Mesmo conjunto de migrations em dev/CI/prod:** `embed.FS`
  garante que o binário carrega as migrations que foram
  compiladas, sem risco de drift.

### Negativas

- **Library `migrate` é pesada:** adiciona ~5MB ao binário.
  Aceitável — single-binary era o objetivo.
- **Sem auto-migrate:** o operador precisa lembrar de rodar
  após `git pull`. Mitigado por `Makefile` target
  `make run-migrate` e por CI que valida `migrate status`
  antes de aprovar o PR.
- **Migrações irreversíveis (DROP COLUMN) requerem cuidado:**
  se a versão nova remove uma coluna que a app ainda usa,
  o boot do serve quebra. Documentado em
  `migrations/README.md`.

## Notas de implementação

Arquivos relevantes:

- `cmd/server/migrate.go` — implementação do subcomando
- `migrations/` — `0001_init.up.sql` ... `0006_kek_version.up.sql`
- `pkg/config/config.go:71-76` — `MEZ_MIGRATE_DATABASE_URL`
  (DSN com role `mez_migrate`, BYPASSRLS)
- `Makefile:106` — `run-migrate` target
