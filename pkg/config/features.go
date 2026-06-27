// Package config helpers for runtime feature flag rollout.
//
// ADR-0041 (Sprint 0): toda feature nova de segurança começa com default OFF
// e liga via env var + rollout percentual. Esse helper centraliza a lógica
// para evitar duplicação em cada handler/middleware.
//
// Convenção de env var:
//
//	MEZ_FEATURE_<NAME>=true|false  → liga/desliga binário
//	MEZ_FEATURE_<NAME>_PCT=0-100   → rollout gradual (override do bool)
//
// Lê uma vez no boot (cache + mutex) e expõe funções puras.
package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	cacheMu   sync.RWMutex
	boolCache = map[string]bool{}
	pctCache  = map[string]int{}
)

// IsFeatureEnabled retorna true se a feature deve estar ativa no processo
// atual. Ordem de precedência:
//
//  1. MEZ_FEATURE_<NAME>_PCT (0-100) — se setado, usa como rollout %
//     combinado com hash determinístico do caller. Útil para A/B gradual.
//  2. MEZ_FEATURE_<NAME> ("true"/"false") — liga/desliga binário.
//  3. defaultVal — fallback se nenhuma env var estiver setada.
//
// NOTA: a versão com hash de caller fica em IsFeatureEnabledFor() abaixo.
func IsFeatureEnabled(name string, defaultVal bool) bool {
	cacheMu.RLock()
	if v, ok := boolCache[name]; ok {
		cacheMu.RUnlock()
		return v
	}
	cacheMu.RUnlock()
	val := readFeatureBool(name, defaultVal)
	cacheMu.Lock()
	boolCache[name] = val
	cacheMu.Unlock()
	return val
}

// IsFeatureEnabledFor combina IsFeatureEnabled com rollout percentual
// baseado em hash do subject (ex: userID, tenantID). O hash é determinístico
// — mesmo subject sempre cai no mesmo bucket.
//
// Uso: IsFeatureEnabledFor("authz_v2", false, "user:"+userID)
//
// Quando _PCT=100, todos os subjects passam. Quando _PCT=0, nenhum passa.
func IsFeatureEnabledFor(name string, defaultVal bool, subject string) bool {
	pct := ReadFeaturePct(name, -1) // -1 = não setado
	if pct < 0 {
		return IsFeatureEnabled(name, defaultVal)
	}
	if pct >= 100 {
		return true
	}
	if pct == 0 {
		return false
	}
	bucket := stableHash(subject) % 100
	return int(bucket) < pct
}

// ReadFeaturePct retorna o percentual de rollout (0-100) ou -1 se não setado.
func ReadFeaturePct(name string, defaultVal int) int {
	cacheMu.RLock()
	if v, ok := pctCache[name]; ok {
		cacheMu.RUnlock()
		return v
	}
	cacheMu.RUnlock()
	envKey := "MEZ_FEATURE_" + strings.ToUpper(name) + "_PCT"
	pct := parsePctEnv(envKey, defaultVal)
	cacheMu.Lock()
	pctCache[name] = pct
	cacheMu.Unlock()
	return pct
}

// ReloadFeatures limpa o cache. Útil em testes ou após hot-reload
// de system_settings.
func ReloadFeatures() {
	cacheMu.Lock()
	boolCache = map[string]bool{}
	pctCache = map[string]int{}
	cacheMu.Unlock()
}

// readFeatureBool lê MEZ_FEATURE_<NAME> do env (case-insensitive).
// Aceita: "1", "true", "yes", "on" → true. "0", "false", "no", "off" → false.
// Default se não setada ou inválida.
func readFeatureBool(name string, defaultVal bool) bool {
	envKey := "MEZ_FEATURE_" + strings.ToUpper(name)
	raw, ok := os.LookupEnv(envKey)
	if !ok {
		return defaultVal
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func parsePctEnv(envKey string, defaultVal int) int {
	raw, ok := os.LookupEnv(envKey)
	if !ok {
		return defaultVal
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	pct, err := strconv.Atoi(raw)
	if err != nil || pct < 0 || pct > 100 {
		return defaultVal
	}
	return pct
}

// stableHash retorna uint64 determinístico de s. Usamos FNV-1a (não
// crypto, mas estável e rápido) — não é para segurança, só para
// sharding consistente de rollout.
func stableHash(s string) uint64 {
	const (
		offset uint64 = 14695981039346656037
		prime  uint64 = 1099511628211
	)
	h := offset
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}
