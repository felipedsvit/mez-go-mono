# WhatsApp Web (whatsmeow) — canal informal

> Canal não-oficial via lib `go.mau.fi/whatsmeow` (tulir/whatsmeow).
> Conexão persistente (1 client/tenant), sessão pareada via QR,
> mídia transduzida (OGG/Opus, WebP) via `pkg/media.FFmpegTranscoder`.

Issue de tracking: **#158** (Fase 9 — substituir `stubWhatsmeowClient`).

## Estado atual (Fase 9 — `fase8+` branch)

| Componente | Status | Arquivo |
|---|---|---|
| `Client` interface | ✅ Completa (16 métodos) | `internal/adapter/provider/whatsmeow/client.go:31-77` |
| `RealClient` (whatsmeow real) | ✅ Implementado | `internal/adapter/provider/whatsmeow/real_client.go` |
| `NewRealClientFactory` | ✅ Implementado | `internal/adapter/provider/whatsmeow/real_factory.go` |
| `DeviceIdentity` (anti-ban E1) | ✅ Implementado | `internal/adapter/provider/whatsmeow/identity.go` |
| `HistoryRepo` (whatsapp_history) | ✅ Implementado | `internal/adapter/repository/postgres/whatsapp_history_repo.go` |
| `Manager.SetClientFactory` | ✅ Implementado | `internal/adapter/provider/whatsmeow/manager.go:78-95` |
| Wire-up no `cmd/server/wire.go` | ✅ Implementado | `cmd/server/wire.go:189-205` |
| `stubWhatsmeowClient` (default) | ✅ Mantido para CI | `internal/adapter/provider/whatsmeow/stub_client.go` |
| Tabela `whatsapp_history` | ✅ Migration existe | `migrations/0004_whatsmeow.up.sql:61-69` |
| Testes unitários | ✅ 18 testes cobrem nil-safety + assertions | `internal/adapter/provider/whatsmeow/real_client_test.go` |
| Testes integração (testcontainers) | ✅ Build tag `integration` | `tests/integration/real_client_test.go` |

## Setup inicial

### 1. Configurar o session store (Postgres)

O whatsmeow real precisa de um Postgres para persistir a sessão
(tabelas `whatsmeow_*` criadas pelo `sqlstore.New(ctx, "pgx", dsn)`).
Use o mesmo Postgres da app, ou um dedicado (recomendado em produção).

```bash
# Variavel de ambiente
export MEZ_WHATSMEOW_ENABLED=true
export MEZ_WHATSMEOW_DEVICE_DSN="postgres://mez_app:password@localhost:5432/mez_whatsmeow?sslmode=disable"

# Anti-ban: finge ser Chrome no Mac OS (default)
export MEZ_WHATSMEOW_IDENTITY_KIND=chrome
export MEZ_WHATSMEOW_IDENTITY_OS="Mac OS"
```

### 2. Parear a conta (primeiro start)

Na primeira inicialização (sem `Store.ID`), o real client emite
QR codes via `GetQRChannel`. Para parear:

1. Reinicie o `cmd/server` (vai logar "whatsmeow: real client factory enabled")
2. Acesse `GET /api/channels/whatsmeow/qrcode` (ou equivalente admin UI)
3. O QR code é renderizado no frontend (htmx refresh a cada 30s)
4. No celular: WhatsApp → Menu → Aparelhos conectados → Conectar
5. Escaneie o QR
6. O `Manager.CurrentQR` retorna "success" e `cli.IsConnected()` vira true

### 3. Operar normalmente

Após pareamento, todas as chamadas de envio passam pelo real client.
O reconnect é automático (whatsmeow internamente) e o `Reconnect`
wrapper da mono aplica backoff exponencial.

## Anti-ban (E1, E5, E6)

### E1 — Device Identity

`DeviceIdentity.Apply()` é chamado **uma vez** antes do primeiro
`NewClient` (gotcha do whatsmeow: `store.SetOSInfo` e
`DeviceProps.PlatformType` são GLOBAIS do pacote). Configurável:

- `MEZ_WHATSMEOW_IDENTITY_KIND=chrome` (default) — emula Chrome
- `MEZ_WHATSMEOW_IDENTITY_KIND=edge` — emula Edge
- `MEZ_WHATSMEOW_IDENTITY_KIND=none` — desabilita (mantém firma whatsmeow default, **NÃO recomendado em produção**)

### E5 — Rate limit (warmup)

`internal/adapter/repository/postgres/whatsapp_account_state.go`
rastreia `day_sent_count`, `health_score`, `timelock_until` e `banned_at`
por `(tenant_id, jid)`. O adapter do whatsmeow consulta
`WhatsAppStateRepo.LoadState` antes de cada envio:

```go
// adapter.go (trecho relevante)
if a.stateR != nil {
    st, err := a.stateR.LoadState(ctx, a.tenant, jid.String())
    if err == nil && st.BannedAt.IsZero() == false {
        return "", fmt.Errorf("whatsmeow: tenant banned at %s", st.BannedAt.Format("2006-01-02"))
    }
}
```

### E6 — Warmup

`Reconnect` (no mono) aplica backoff exponencial em reconexões.
Configurável via `MEZ_WHATSMEOW_MAX_BACKOFF` (default 30min).

## Mídia (ffmpeg/cwebp)

O `pkg/media.FFmpegTranscoder` é **obrigatório** para o whatsmeow
aceitar PTT (OGG/Opus) e stickers (WebP). Sem ffmpeg no container,
o transcoder entra em modo passthrough e o WhatsApp **rejeita** a
mensagem (HTTP 400).

Container já vem com ffmpeg instalado:

```dockerfile
# deployments/Dockerfile
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    libwebp-tools
```

**Concurrency**: `MEZ_FFMPEG_CONCURRENCY=4` (default). Cada PTT/WebP
leva 50-200ms; 4 simultâneos cobre a maioria dos workloads.

## HistorySync

O whatsmeow emite `*events.HistorySync` na primeira conexão de uma
conta restaurada (pode ter milhares de mensagens). O `EventProcessor`
do mono tem um canal separado para esses eventos (queue bounded
= 8) que:

1. Extrai `(jid, msg_id, timestamp, from_me, body, type)` via
   `extractHistoryMessages` (compatível com a interface do
   `*events.HistorySync` da lib e com `HistorySyncEvent` local)
2. Persiste na tabela `whatsapp_history` via `HistoryRepo.InsertMany`
3. Bounded: máximo 1000 mensagens/tenant por start (OOM guard)

Migration já existe em `migrations/0004_whatsmeow.up.sql:61-69`.
RLS fail-closed (FORCE RLS + mez_app sem BYPASSRLS — C3+C4).

## D6 Actions

| Action | Implementado em | Notas |
|---|---|---|
| `reaction` | `RealClient.SendReaction` (BuildReaction + SendMessage) | Emoji vazio = remove |
| `edit` | `RealClient.EditMessage` (BuildEdit + SendMessage) | Janela de 20min (`EditWindow`) |
| `revoke` | `RealClient.RevokeMessage` (BuildRevoke + SendMessage) | Para dono da mensagem |
| `mark_read` | `RealClient.MarkRead` (`cli.MarkRead` com `ReceiptTypeRead`) | Múltiplos IDs |
| `typing` | `RealClient.SendChatPresence` (`ChatPresenceComposing/Paused`) | Com `ChatPresenceMediaText` |
| `presence` | `RealClient.SendPresence` (`PresenceAvailable/Unavailable`) | Global (sem chat específico) |

## Gotchas (do whatsmeow)

Documentado no `tulir/whatsmeow/_autodocs/configuration.md` e
no AGENTS.md do pai:

1. **Driver pgx via `database/sql`**: blank import `_ "github.com/jackc/pgx/v5/stdlib"`. O `pgxpool` do mono **NÃO** é compatível com o `sqlstore` (gotcha documentado).
2. **`store.SetOSInfo` é GLOBAL**: deve ser chamado antes de qualquer `NewClient`. Por isso, `Manager.SetClientFactory` chama `Identity.Apply()` via `sync.Once`.
3. **`*whatsmeow.Client` não é thread-safe**: o `Dispatcher` per-tenant serializa chamadas (single goroutine/tenant).
4. **`GetQRChannel` falha se já conectado**: retorna `ErrQRAlreadyConnected`. Use só na primeira vez.
5. **`EditWindow = 20min`**: edita apenas mensagens próprias dentro da janela.
6. **Mídia deve ser transduzida**: PTT (OGG/Opus) e sticker (WebP) são obrigatórios. Sem ffmpeg, WhatsApp rejeita.

## Smoke test manual (pós-deploy)

```bash
# 1. Iniciar o server com MEZ_WHATSMEOW_ENABLED=true
./cmd/server serve

# 2. Pegar QR code (vai mostrar "stub-qr-code" se stub, ou QR real)
curl -s http://localhost:8080/api/channels/whatsmeow/qrcode | jq -r .code | feh -

# 3. Escanear com WhatsApp (celular)

# 4. Verificar conexão
curl -s http://localhost:8080/api/channels/whatsmeow/health | jq .

# 5. Enviar mensagem de teste
curl -X POST http://localhost:8080/api/messages \
  -H "Content-Type: application/json" \
  -d '{"peer_id":"5511999999999@s.whatsapp.net","type":"text","body":"olá"}'

# 6. Verificar logs (wamid deve ser real, não "stub-*")
```

## Migração do stub para o real

O `fase8-tracking` branch anterior usava o stub. Para migrar:

1. `MEZ_WHATSMEOW_ENABLED=true` no env
2. `MEZ_WHATSMEOW_DEVICE_DSN` apontando para Postgres (pode ser o mesmo da app, com database dedicado)
3. Restart
4. Parear via QR (uma vez por tenant)
5. Sessão persistida em `whatsmeow_*` tables

## Referências

- `tulir/whatsmeow` (lib): https://github.com/tulir/whatsmeow
- `tulir/whatsmeow/_autodocs/configuration.md` — boot canônico
- `tulir/whatsmeow/_autodocs/api-reference/message.md` — SendMessage + waE2E
- `tulir/whatsmeow/_autodocs/api-reference/events.md` — AddEventHandler
- `tulir/whatsmeow/_autodocs/errors.md` — sentinelas (ErrNotConnected, ErrNotLoggedIn)
- Issue #158 — plano completo da Fase 9
- `mez-go/internal/adapter/provider/whatsmeow/` — adapter de produção do pai (referência)
- `mez-go/cmd/mez-worker-whatsmeow/main.go` — boot canônico (sqlstore + pgx)
- `mez-go/docs/auditoria/plano-correcao.md` — histórico de anti-ban
