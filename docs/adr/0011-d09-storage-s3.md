# ADR 0011 — D9: Storage S3-compatible (MinIO no dev)

* **Status:** Aceita (mantida)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D9](../../README.md#5-decisões-arquiteturais)

## Contexto

Mensagens podem carregar mídia (imagem, áudio, vídeo, documento,
sticker). O storage precisa ser:

- **Durável** — não perder mídia após restart.
- **Acessível por tenant** — URL assinada ou proxy HTTP que
  verifica permissão.
- **Compatível com S3** — para que o operador escolha entre
  AWS S3, MinIO self-hosted, Cloudflare R2, Wasabi, etc.

As alternativas:

1. **Filesystem local** — simples, mas não escala horizontalmente
   e backup é manual.
2. **Postgres BYTEA** — backup incluído no `pg_dump`, mas
   incha a DB e mata performance em vídeos >10MB.
3. **S3-compatible** — MinIO local para dev, AWS S3 / R2 / Wasabi
   para prod. Operator escolhe.

## Decisão

Adotamos a opção 3: **storage S3-compatible** via `minio-go/v7`.

Layout de chaves:

```
s3://<bucket>/tenants/<tenant_id>/<conversation_id>/<message_id>.<ext>
s3://<backup-bucket>/tenants/<tenant_id>/backups/<backup_id>/...
```

Configuração:

- `MEZ_S3_ENDPOINT` — `http://localhost:9000` (MinIO dev) ou
  `https://s3.amazonaws.com` (prod)
- `MEZ_S3_BUCKET` — bucket de mídia (default `mezgo-media`)
- `MEZ_S3_BACKUP_BUCKET` — bucket de backups (default
  `mezgo-backups`)
- `MEZ_S3_ACCESS_KEY` / `MEZ_S3_SECRET_KEY` — credenciais

Não expomos URL pública: o handler HTTP serve mídia via proxy
que valida o `tenant_id` da sessão antes de baixar do S3. URLs
pré-assinadas são geradas **apenas** para integrações externas
(ex.: Meta pedindo o asset) com TTL de 5 min.

## Consequências

### Positivas

- **Flexibilidade operacional:** rodar MinIO em dev (sem custo)
  e AWS S3 / R2 em prod (managed). Sem mudança de código.
- **Backup incremental trivial:** `mc mirror` ou lifecycle rules
  do S3 replicam o bucket para outra região. Sem código de
  replicação.
- **URLs pré-assinadas:** Meta, Telegram, e outros provedores
  podem baixar mídia via URL temporária sem auth no mono. Mitiga
  o "preciso abrir meu S3 para a internet".
- **Versioning e lifecycle são do S3:** não reinventamos.

### Negativas

- **Dependência externa:** o mono não sobe sem S3 configurado.
  Mitigado por: bucket opcional em dev (skip via env), e
  health check explícito no boot.
- **Latência de download:** o proxy mono → S3 adiciona ~50ms
  vs servir do filesystem. Aceitável para o caso de uso.
- **Custo de egress em cloud:** downloads grandes (vídeo 50MB)
  podem custar caro se o tenant baixa repetidamente. Mitigado
  por CloudFront / CDN em frente (pós-1.0).
- **Credenciais AWS no env:** `MEZ_S3_SECRET_KEY` é sensível.
  Mesmo risco de `MEZ_MASTER_KEY` (Fase 7 #92). Operador usa
  `MEZ_S3_SECRET_KEY_FILE` em prod.

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/storage/s3/store.go` — wrapper `Store` com
  `Put`, `Get`, `DownloadStream`, `UploadBytes`
- `cmd/server/wire.go:181-189` — inicialização do `Store`
- `internal/usecase/backup/export.go` — usa `Store` para
  subir manifest + NDJSON
- `deployments/docker-compose.yml` — MinIO local para dev
