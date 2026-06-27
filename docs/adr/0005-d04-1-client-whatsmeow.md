# ADR 0005 — D4: 1 client whatsmeow por tenant

* **Status:** Aceita (mantida desde o pai)
* **Data:** 2026-06-27
* **Issue:** #94
* **Referência:** [README §5, linha D4](../../README.md#5-decisões-arquiteturais)

## Contexto

O canal WhatsApp via whatsmeow (multidevice) exige um `*whatsmeow.Client`
por número de telefone. As alternativas de modelagem no mono:

1. **1 client global compartilhado** — impossível: o client carrega
   sessão por número, e dois tenants diferentes podem ter o mesmo
   número em contas distintas.
2. **Pool com sharding por hash(tenantID) % N** — modelo do pai
   `mez-go` com `pkg/shard`. Mapeamento estável mas adiciona um
   indirection e exige reconciliação quando N muda.
3. **1 client lazy por tenant, cacheado em memória** — primeira
   referência cria, demais reusam. Single-process garante que o
   client está sempre no mesmo endereço de memória.

## Decisão

Adotamos a opção 3: **`whatsmeow.Manager` mantém um `map[tenantID]→
*whatsmeow.Client` em memória**, com `GetOrCreate(ctx, tenantID, fn)`
que cria o client on-demand via callback injetado (tipicamente o
`whatsmeow.NewClient` com sqlstore).

Garantias:

- **Serialização obrigatória:** `whatsmeow.Client` não é thread-safe
  para `SendMessage`/events. Cada client tem seu próprio mutex
  interno; o manager serializa `GetOrCreate` para evitar race de
  inicialização.
- **Buffer bounded:** cada client tem buffers de 2048 events
  pendentes e 8 history sync — protege contra memory blowup se
  um tenant fica offline.
- **Disconnect on shutdown:** graceful shutdown chama
  `manager.Disconnect(tenantID)` por tenant, garantindo que o
  whatsmeow feche o socket limpo (sem session store corrompido).
- **`recover()` por goroutine:** panic de 1 tenant (ex.: payload
  malformado do whatsmeow) não derruba o processo. Mitigação
  do risco C10.

## Consequências

### Positivas

- **Simplicidade:** zero sharding, zero hash, zero reconciliação
  de partição. O "1 tenant = 1 client" é literal.
- **Boot determinístico:** não há "warm-up" de pool; cada tenant
  carrega o client no primeiro uso. Aceitável — a latência do
  primeiro `Send` é dominada pelo handshake do whatsmeow, não pela
  alocação do client.
- **Cleanup trivial:** `manager.Disconnect(tenantID)` fecha
  exatamente o que precisa fechar. Sem "clientes órfãos" como
  aconteceria num pool com sharding estático.

### Negativas

- **Memória proporcional ao número de tenants ativos:** 1 client
  pesa ~5MB (whatsmeow + sqlstore). Para 1000 tenants ativos,
  ~5GB. Mitigado pelo `MaxActiveTenants` (default 100) que faz
  eviction LRU — ver `MEZ_MAX_ACTIVE_TENANTS`.
- **IP de saída compartilhado:** todos os tenants saem pelo mesmo
  IP do processo. Se a Meta banir o IP, todos os tenants
  WhatsApp caem. Aceitável para o 1.0 (single-box); mitigado
  pós-1.0 com NAT por tenant ou proxy dedicado.
- **Sem horizontal scaling no 1.0:** 2 processos do mono
  criariam 2 clients por tenant, com state divergente. O
  mono **não** suporta multi-replica no 1.0 (decisão de
  topologia single-process). Documentado em [§21 do README](../../README.md#21-riscos-e-mitigações).

## Notas de implementação

Arquivos relevantes:

- `internal/adapter/provider/whatsmeow/manager.go` — `Manager` com
  `GetOrCreate`, `Disconnect`, mutex interno
- `internal/adapter/provider/whatsmeow/client.go` — wrapper
  thread-safe sobre `*whatsmeow.Client` (mutex + buffer)
- `cmd/server/wire.go:174-175` — inicialização do manager
- `pkg/config/config.go:34` — `max_active_tenants`
