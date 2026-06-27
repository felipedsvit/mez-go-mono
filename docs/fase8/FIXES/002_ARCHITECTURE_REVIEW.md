# Revisão Arquitetural — mez-go-mono

> **Data:** junho/2026 · **Escopo:** arquitetura do 1.0 (Fases 0–8 merged) · **Persona:** Go architect (simplicidade, explicitude, push-back contra abstração prematura)

**Snapshot.** ~16,8k LOC não-teste + ~5,6k LOC teste (185 arquivos Go), 6 migrations, 1 binário. O modelo (in-process bus tipado, RLS FORCED + 3 roles, envelope encryption local, reconciler com `SKIP LOCKED`, outbox + relay com poll fallback) é coerente e bem cabeado.

Abaixo, do mais grave ao cosmético.

---

## 1. Bugs reais (corrigir antes de qualquer outra coisa)

### 1.1 `OutboxRepo.ClaimNext` não segura o lock

`internal/adapter/repository/postgres/outbox.go:104-160` faz `platformPool.Query(... FOR UPDATE SKIP LOCKED ...)`. Como `Query` é uma *statement isolada* (não há `BeginTx`), os row locks são liberados ao fim do statement. O comentário em `db.go:108` mente: "a tx mez_platform é aberta dentro do Claim". `AcquireClaimLock` (linha 278) está **exportado e nunca chamado**. Resultado: dois relays concorrentes podem pegar a mesma linha. Para 1.0 single-process passa despercebido; quebra o modelo no momento de qualquer escala horizontal.

**Fix mínimo:** mover `ClaimNext` para `BeginTxFunc` que retém o tx durante o processamento (`r.outbox.Process(...)` + MarkSent/MarkFailed), ou retornar `(pgx.Tx, []Message, error)` e fazer o caller drenar dentro do tx.

### 1.2 Two `metrics.Registry` instances no mesmo processo

`runServe` em `cmd/server/serve.go:44` cria `metricsReg` e passa para `NewRunnerSink(metricsReg)` (lifecycle metrics). `wireServices` em `cmd/server/wire.go:151` cria *outro* `metricsReg` e o entrega ao bus e ao HTTP `/metrics`. O bus publica métricas no registry A; o `/metrics` expõe o registry B; o lifecycle escreve no C. Métricas de saturação do bus (o sinal de alerta principal do README §7) **não aparecem** no `/metrics` do app.

**Fix:** uma única `metricsReg` em `runServe`, passada para `wireApp`.

### 1.3 `UnsubscribeInbound` compara funções por `reflect.Pointer()`

`internal/adapter/broker/bus.go:184-210`:

```go
if reflect.ValueOf(h).Pointer() == reflect.ValueOf(handler).Pointer() {
```

`reflect.ValueOf(funcVal).Pointer()` devolve o **code pointer**, não a identidade do closure. Dois closures diferentes sobre variáveis capturadas distintas vão comparar iguais. O consumer é removido errado. Em `StatusConsumer` (instância singleton) o problema ainda não dispara, mas qualquer `Subscribe*` chamado duas vezes com literais idênticos quebra.

**Fix:** devolver um `token` opaco de `Subscribe` e `Unsubscribe(token)`. Padrão: `bus.SubscribeInbound(handler) → handle` e `bus.UnsubscribeInbound(handle)`.

### 1.4 `Contact.ProviderID` é gravado e re-escrito com o mesmo valor

`internal/usecase/messaging/ingest.go:109-123`:

```go
contact := &domain.Contact{ ProviderID: evt.MessageID }
// ...
contact.ProviderID = evt.MessageID // sobrescreve com o mesmo valor
```

A linha 110-114 cria com `ProviderID = evt.MessageID`; a 119 sobrescreve com `evt.MessageID` de novo. A condicional "se existir" não verifica nada. Pior: `evt.MessageID` é o ID da **mensagem**, não do **contato** — então o `ProviderID` do contato fica errado (dedup em `messages.provider_msg_id` pode colidir com o ID do contato). É stub.

### 1.5 `Body: ""` no ingest

`internal/usecase/messaging/ingest.go:148-153`: toda mensagem inbound é persistida com `Body: ""`. Se o `evt` não trouxer o conteúdo em `Metadata` (e o caminho atual não lê), o texto da mensagem do cliente **é perdido**. É bug de produto, não de arquitetura, mas está no pipeline principal.

### 1.6 `ForEachTenant` materializa todos os tenants em memória

`internal/adapter/repository/postgres/outbox.go:241-271`:

```go
var tenants []string
for rows.Next() { ... tenants = append(tenants, id) }
for _, tid := range tenants { fn(...) }
```

Itera via slice. Sem enforce de `MEZ_MAX_ACTIVE_TENANTS`. Em 1.0 (≤100) é OK, mas a quebra está lá.

**Fix:** streamar: `for rows.Next() { if err := fn(...); err != nil { return err } }`.

---

## 2. Abstração prematura / violação de camadas (revisar, mas não é agora)

### 2.1 `AppContext` virou god-object

`cmd/server/wire.go:55-107`: 30+ campos, todos os tipos do processo. Dois sintomas:

- Interfaces `poolCloser` e `whatsmeowDisconnector` (`wire.go:110-118`) existem **só** porque `AppContext` precisa expor os internos ao `serve.go:174-209` (shutdown) sem importar os tipos concretos. É a pattern de "context-as-bag" que empurra acoplamento para a borda.
- Testes de subsistema precisam montar o `AppContext` inteiro.

**Sugestão:** quebrar em 2–3 structs (`CoreServices`, `LifecycleHooks`, `HTTPSetup`), ou aceitar a dependência concreta em `serve.go` (afinal, é o mesmo `package main` — `whatsmeow.Manager` e `*pgxpool.Pool` são internos ao binário). O custo do encapsulamento por interface aqui > benefício.

### 2.2 `port.MemorySenderRegistry` no package errado

`internal/core/port/sender_registry.go`: `port` é onde *interfaces* moram. `MemorySenderRegistry` é implementação concreta e ainda depende de `zerolog` + `time` (não é puro). A interface `SenderRegistry` deveria estar em `port/`; a implementação em `internal/adapter/sender/registry/` (ou similar). Hoje, todo teste que importa o port carrega o logger.

### 2.3 Capability factories no `port`

`internal/core/port/resolver.go` mistura `Resolver` + `CapabilitiesWABA/IG/MSG/TG/WAWeb` factory functions. A matriz (D7 + README §11) **muda** à medida que adapters evoluem. A factory + o teste de paridade (`capabilities_test.go`) amarram o port a um snapshot. A `CapabilitySet` deveria ser *declarada pelo adapter* (cada `provider/<canal>/capabilities.go`) e *registrada* no boot — o que o `wire.go:184-189` já faz. Então as factories no `port` são duplicação do que cada adapter deveria exportar.

**Recomendação:** deletar `CapabilitiesWABA/IG/MSG/TG/WAWeb` de `port`; ler do adapter no boot. Teste de paridade passa a varrer os adapters, não o port.

### 2.4 Stub renderers como `var` mutáveis

`internal/transport/adminweb/handlers_app.go:173-188`:

```go
var InboxPage = func(data map[string]any) Renderer { return stubRenderer("inbox", data) }
```

Variáveis package-level que seguram funções. Reatribuíveis a qualquer momento, sem lock, sem ownership claro. O README diz `templ + htmx`; este arquivo é placeholder. Se vai morrer, marque `// Deprecated` e aponte para o componente `templ` correspondente. Se vai viver, tire do escopo global.

### 2.5 `var Upgrader` global com `return true`

`internal/transport/websocket/hub.go:260-267`:

```go
var Upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}
```

Em código de produção multi-tenant, `CheckOrigin: return true` é achado sério. O comentário "Production: validar" deveria ser `panic` ou um valor de config obrigatório. Se for dev-only, faça `NewUpgrader(cfg) Upgrader` e injete no handler.

---

## 3. `init()` + estado global (limpar agora, é barato)

### 3.1 `pkg/logger/logger.go:11-13`

```go
func init() { zerolog.TimeFieldFormat = time.RFC3339Nano }
```

Muta estado de biblioteca externa implicitamente. Não pode falhar, mas é global e não é testável. Mova para `logger.New` ou para um `var DefaultTimeFormat = time.RFC3339Nano` que o `New` aplica.

### 3.2 `var setupTpl = template.Must(template.New("setup").Parse(setupHTML))`

`internal/transport/adminweb/handlers_setup.go:173`: `template.Must` panics no import se o HTML tiver typo. Se `templ` é o framework-alvo, este arquivo é dívida. Se ainda é usado, mova para `var setupTpl = template.Must(template.ParseFS(templatesFS, "setup.gohtml"))` via `//go:embed`.

### 3.3 `var _ = chi.NewRouter` no fim de `wire.go:384`

Código morto. O import `chi` na linha 19 também é morto. Remover.

### 3.4 `var _ = payloadEncoder` em `internal/usecase/messaging/send.go:215`

Código morto. Remover ou usar.

---

## 4. Erros duplicados / dívida menor

- `ErrCredentialsNotFound` declarado em **3 lugares**: `secrets/keyring.go:36`, `webhook/secrets/credentials.go:26`, e `webhook/secrets/resolvers.go:25` (`ErrNotConfigured` é a mesma ideia). Unificar em `port`.
- `ErrNotInTransaction` em `core/admin/tx.go:41` e provavelmente em `postgres/` também.
- `var _ port.Encryptor = s` no teste de `local_sealer.go:192` é um compile-time check. Mova para `var _ port.Encryptor = (*LocalSealer)(nil)` no arquivo de produção (mesma forma que `OutboxRepo`).

---

## 5. Concorrência / shutdown — *quase* OK, mas há um canto

- `reconciler.go:113` e `relay.go:87`: `select` com `<-ctx.Done()` e `<-r.stopCh`. Se `Stop()` for chamado *antes* de `Run()`, o select fica indefinido. Na prática, o `ctx` é cancelado primeiro no shutdown coordenado, então passa. Mas se um teste chama `Stop()` em um Reconciler que nunca rodou, hoje não dá problema porque o Run nem começou. **Apenas garanta via teste** (`TestReconciler_StopIsIdempotent` já cobre).
- `lifecycle.Runner.Run` (`lifecycle/runner.go:291-310`) usa `recover()` dentro da goroutine — bom. O `wg.Done` está no `defer` de cima, então o recover() roda **depois** do `wg.Done`. Ordem correta.
- `Runner.Boot` (`lifecycle/runner.go:142`) marca `startedSet[p.Name] = true` **antes** de chamar `Start`. Se `Start` panicar, o `defer recover()` em Boot captura, marca erro, e o `Shutdown` subsequente ainda chama o `Stop` (que pode estar nil — OK, ignorado). Bom.

---

## 6. Achado a confirmar (não mexi a fundo)

`internal/adapter/repository/postgres/db.go:29-34`:

```go
func appQFromCtx(ctx context.Context, pool *pgxpool.Pool) querier {
    if tx, ok := ctx.Value(appTxKey).(pgx.Tx); ok {
        return tx
    }
    return pool  // <-- fallback "silencioso"
}
```

O comentário diz "fall back to pool for queries executed outside a tenant transaction". Com `FORCE RLS` + role `mez_app` sem `BYPASSRLS`, uma query fora de `RunInTenantTx` **não retorna zero rows em silêncio** — ela falha no `current_setting('mez.tenant_id', false)::uuid` (C4). Logo, o fallback é seguro em produção, mas é uma "bomba de Postgres-error" disfarçada de comportamento silencioso. Mude o nome do helper para `appQFromCtxOrPool` e adicione um comentário explícito de que o fallback só é válido em testes ou em paths admin já garantidos por outra layer (e.g., `RunAsPlatform`).

---

## 7. O que está bom e deve ser preservado

- **`Reconciler`** (`reconciler.go`) é o melhor arquivo do repo: 150 linhas, interface local, `stopCh`/`stopOnce`/`wg` no padrão idiomático, boot sweep separado do tick, `Run` retorna `nil` em `ctx.Done` (não erro). Não tocar.
- **Bus drop-safe + reconciler** (`broker/bus.go:85-96` + reconciler) é exatamente o modelo certo. A "publicação é notificação, DB é fonte da verdade" está correta.
- **`lifecycle.Runner`** (`pkg/lifecycle`) está sólido: timeout por phase, panic recovery, partial shutdown, LIFO, métricas por phase, `Run` para goroutines long-running. A separação de `Start` síncrono vs `Run` async é clara.
- **3 roles Postgres** + `FORCE RLS` + `mez.tenant_id` via `set_config(..., is_local := true)` é a postura correta. O modelo de `RunInTenantTx` + `RunAsPlatform` é o certo.
- **Envelope encryption** com `defer zero(dek)`, KEK só em env, DEK/tenant wrapped, KEK versionada — desenho limpo.
- **Compile-time interface checks** (`var _ port.OutboxWriter = (*OutboxRepo)(nil)`) presentes onde importa.
- **7 packages com `goleak.VerifyTestMain`** — boa higiene. A escolha de *não* centralizar em um único testutil está correta (cada package é dono das suas goroutines).
- **`t.Cleanup(func() { _ = bus.Drain(ctx) })`** em `bus_test.go:36` é o jeito certo de evitar leak no goleak.
- **`cfg.ValidateServe()`** separado de `Load()` está certo — evita validar campos de `migrate` no boot do `serve`.

---

## Resumo executivo

| Severidade | Item | Arquivo |
|---|---|---|
| 🔴 Bug | Two `metrics.Registry` instances | `serve.go:44`, `wire.go:151` |
| 🔴 Bug | `OutboxRepo.ClaimNext` não segura lock | `outbox.go:104` |
| 🔴 Bug | `UnsubscribeInbound` por code pointer | `bus.go:184`, `bus.go:199` |
| 🔴 Bug | `Ingestor` perde `Body` e zoa `ProviderID` | `ingest.go:109-153` |
| 🟠 Refactor | `AppContext` god-object + interfaces ad-hoc | `wire.go:55-118` |
| 🟠 Refactor | `MemorySenderRegistry` no `port` | `sender_registry.go` |
| 🟠 Refactor | Capability factories no port | `resolver.go:73-132` |
| 🟡 Limpar | `init()` no logger | `logger.go:11` |
| 🟡 Limpar | `var Upgrader` global com `return true` | `hub.go:260` |
| 🟡 Limpar | stub renderers mutáveis | `handlers_app.go:173-188` |
| 🟡 Limpar | dead code (`chi`, `payloadEncoder`) | `wire.go:384`, `send.go:215` |
| 🟡 Limpar | erros duplicados | 3 lugares |
| ✅ OK | Reconciler, lifecycle.Runner, bus, envelope, RLS | — |

**Recomendação:** tratar os 4 vermelhos em uma fase de bugfix antes de avançar para 1.0; os laranjas ficam para um PR de "limpeza" sem mexer em wire de boot; os amarelos são tarefas de 30min cada, agrupar.
