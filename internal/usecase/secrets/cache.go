// Package secrets — cache.go: cache in-memory de DEK decifrada por tenant.
//
// A chave de cache é a string do tenant_id. O valor é a entrada dekEntry
// contendo o DEK em plaintext (32 bytes), a wrapped DEK original (para
// detectar mudança após rotação), a versão da KEK, e um expiresAt.
//
// Por que cache?
//
//   - Decifrar a DEK em cada Encrypt/Decrypt custa 1 unwrap KEK + 1 decrypt
//     GCM (≈ 50µs em hardware moderno). Em um hot-path de webhook inbound
//     (potencialmente 100s req/s) isso é overhead evitável.
//
//   - A DEK é estável entre rotações de KEK (a DEK em si não muda — só
//     o wrap dela). Logo cachear a DEK é seguro: invalidamos quando
//     setamos credenciais novas (SetCredentials) ou após rotação de KEK
//     (InvalidateFn do RotateKEK).
//
// Por que TTL 5min?
//
//   - Reduz a janela de exposição se a memória for dumpada (após 5min o
//     DEK em plaintext some, só wrapped_dek fica na DB).
//
//   - Garante convergência eventual: se InvalidateFn falhar após uma
//     rotação, em 5min o cache re-popula a partir da DB e usa o novo
//     wrapped_dek.
package secrets

import (
	"sync"
	"time"
)

// dekCache é thread-safe via mutex. Não usa sync.Map porque:
//   - o read-modify-write do Put (check expirado + insert) precisa de
//     atomicidade;
//   - tamanho esperado é < 1000 entradas (número de tenants ativos), então
//     a contenção do mutex é desprezível.
type dekCache struct {
	mu      sync.Mutex
	m       map[string]dekEntry
	ttl     time.Duration
	now     func() time.Time
}

type dekEntry struct {
	dek        []byte
	wrappedDEK []byte
	kekVersion int
	expiresAt  time.Time
}

// newDEKCache constrói o cache com TTL configurável. ttl=0 → default 5min.
func newDEKCache(ttl time.Duration) *dekCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &dekCache{
		m:   make(map[string]dekEntry),
		ttl: ttl,
		now: time.Now,
	}
}

// Get retorna a DEK cacheada se válida (não expirada e kekVersion bate).
// Retorna ok=false em miss, expirado, ou version mismatch (após rotate).
func (c *dekCache) Get(tenantID string, kekVersion int) (dek, wrappedDEK []byte, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[tenantID]
	if !found {
		return nil, nil, false
	}
	if c.now().After(e.expiresAt) {
		// Expirado — zera e remove.
		zero(e.dek)
		delete(c.m, tenantID)
		return nil, nil, false
	}
	if e.kekVersion != kekVersion {
		// Versão mudou (rotação) — descarta.
		zero(e.dek)
		delete(c.m, tenantID)
		return nil, nil, false
	}
	return e.dek, e.wrappedDEK, true
}

// Put insere a DEK no cache com TTL. Chave é tenantID. Sobrescreve entrada
// anterior (com zero da anterior para reduzir janela de exposição).
func (c *dekCache) Put(tenantID string, dek, wrappedDEK []byte, kekVersion int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.m[tenantID]; ok {
		zero(prev.dek)
	}
	c.m[tenantID] = dekEntry{
		dek:        dek,
		wrappedDEK: wrappedDEK,
		kekVersion: kekVersion,
		expiresAt:  c.now().Add(c.ttl),
	}
}

// Invalidate remove a entrada do tenantID e zera a DEK em memória.
// Idempotente: no-op se não houver entrada.
func (c *dekCache) Invalidate(tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[tenantID]; ok {
		zero(e.dek)
		delete(c.m, tenantID)
	}
}

// Len retorna o número de entradas (para testes/métricas).
func (c *dekCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
}

// zero sobrescreve b com zeros. Cópia local para não importar crypto.
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
