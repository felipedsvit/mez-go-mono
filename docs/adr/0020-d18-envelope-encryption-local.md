# ADR 0020 — D18: Envelope encryption local (AES-256-GCM, DEK/tenant)

* **Status:** Aceita (revista C9)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D18](../../README.md#5-decisões-arquiteturais)

## Contexto

Credenciais de canal (Meta access tokens, Telegram bot tokens)
precisam ser cifradas em repouso. As alternativas:

1. **Vault Transit** — HSM-backed, rotação automática, política
   central. Prós: padrão da indústria. Contras: depende de
   Vault rodando, topologia mono não inclui Vault.
2. **Envelope encryption local** — KEK em env var (master key),
   DEK aleatório por tenant cifrado pela KEK, credencial cifrada
   pela DEK. Prós: zero dependência externa, single-binary.
   Contras: KEK em env var é ponto único de falha (mitigado por
   `MEZ_MASTER_KEY_FILE` e 0600 perms).
3. **Postgres TDE** — encryption-at-rest do cluster inteiro.
   Prós: cobre tudo. Contras: protege contra disk theft, não
   contra app compromise (a app já tem a key).
4. **Sem cifragem** — armazena em plaintext na tabela. Inaceitável
   (vaza via SQL injection, backup leak, ou operador curioso).

## Decisão

Adotamos a opção 2: **envelope encryption local** com a seguinte
estrutura:

- **KEK (Key Encryption Key):** 32 bytes (AES-256) em
  `MEZ_MASTER_KEY` (base64) ou `MEZ_MASTER_KEY_FILE` (path
  para arquivo 0600).
- **DEK (Data Encryption Key):** 32 bytes aleatórios, gerados
  em `SetCredentials` (1 por tenant × channel).
- **Wrap:** `DEK cifrada por KEK` → persistida como
  `channel_credentials.wrapped_dek` (nonce||ciphertext,
  ~60 bytes).
- **Encrypt:** `credencial cifrada por DEK` → persistida como
  `channel_credentials.encrypted`.
- **Algoritmo:** AES-256-GCM (AEAD, com auth tag).
- **Versionamento:** coluna `kek_version` (Fase 7 #89) +
  opcional `rotation_window_until` (recovery, Fase 8+).
- **Rotação:** `cmd/server rotate-kek` (Fase 7 #92) re-wrap
  de todos os DEKs sem re-cifrar credenciais.

`Keyring` (Fase 7 #91) é o orquestrador:

- `SetCredentials(ctx, tenant, channel, plaintext)` — gera DEK,
  wrap, encrypt, persiste, invalida cache.
- `ResolveCredentials(ctx, tenant, channel)` — fetch wrapped_dek,
  unwrap, decrypt, popula cache in-memory (TTL 5min).
- `Delete` / `Invalidate` — cleanup.

`LocalSealer` (Fase 7 #88) é o adapter que satisfaz
`port.Sealer` e `port.Encryptor`.

## Consequências

### Positivas

- **Zero dependência externa:** single-binary, sem Vault, sem
  KMS cloud. Self-contained.
- **Rotação barata:** `cmd/server rotate-kek` re-wrap é O(N_tenants)
  com N rows, mas o `encrypted` (a credencial em si) NÃO é
  tocado. A DEK em plaintext é estável; só o wrap muda.
- **Fail-closed por construção:** se a KEK é perdida, todas as
  credenciais são irrecuperáveis. Trade-off explícito: KEK
  backup é responsabilidade do operador (offline cold storage).
- **Interface `port.Encryptor` permite backend alternativo
  pós-1.0:** `VaultTransitEncryptor` pode ser plugado sem mudar
  Keyring/usecase. O 1.0 só implementa LocalSealer.
- **Cache TTL 5min:** reduz janela de exposição da DEK em
  memória após rotação. Aceitável.

### Negativas

- **KEK é ponto único de falha:** perda da KEK = perda de
  TODAS as credenciais. Documentado em
  [§21 do README](../../README.md#21-riscos-e-mitigações).
  Mitigações:
  - Backup offline (paper, hardware security module).
  - `MEZ_MASTER_KEY_FILE` com 0600 (não env inline).
  - 2-of-2 quórum pós-1.0 (split-key, fora de escopo agora).
- **Rotação manual:** não há rotação automática de DEK por
  tempo. Operador roda `rotate-kek` após suspeita de
  comprometimento. Pós-1.0: cron.
- **Cache em memória:** se o processo for comprometido, o
  atacante pode extrair a DEK do cache. Mitigado por TTL
  curto + `zero(dek)` após uso + single-process (sem
  extração remota trivial).
- **Sem hardware acceleration:** AES-NI depende da CPU.
  Mitigável com `aes.NewCipher` (Go usa AES-NI nativamente).
- **Limite assumido:** single KEK no 1.0 (decisão §25). Quórum
  M-of-N é pós-1.0.

## Notas de implementação

Arquivos relevantes:

- `pkg/crypto/envelope.go` — `Envelope.Wrap/Unwrap/Encrypt/
  Decrypt` (carryover Fase 0)
- `internal/adapter/crypto/local_sealer.go` — `LocalSealer`
  satisfaz `port.Sealer` + `port.Encryptor` (Fase 7 #88)
- `internal/core/port/sealer.go` — interfaces `Sealer`,
  `Encryptor` (renomeação de `Keyring` em #88)
- `internal/adapter/repository/postgres/credentials_repo.go` —
  `ChannelCredentialsRepo` (Fase 7 #90)
- `internal/core/domain/credentials.go` — `ChannelCredentials`
  struct
- `internal/usecase/secrets/cache.go` — `dekCache` in-memory
  (TTL 5min, Fase 7 #91)
- `internal/usecase/secrets/keyring.go` — `Keyring` orquestrador
  (Fase 7 #91)
- `internal/usecase/secrets/rotate_kek.go` — `Rotate` usecase
  (Fase 7 #92)
- `cmd/server/rotate_kek.go` — CLI subcomando
- `migrations/0006_kek_version.up.sql` — `kek_version` +
  `rotation_window_until` (Fase 7 #89)
