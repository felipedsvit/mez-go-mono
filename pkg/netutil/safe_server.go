// Package netutil — helpers de rede compartilhados.
//
// SafeServer (Issue #143, Security H14b, CWE-400): normaliza
// timeouts do http.Server para evitar slow-loris defense desabilitada
// por configuração errada. Se algum timeout vier 0 (unset / parse
// falhou), SafeServer aplica o default seguro. Idem para
// MaxHeaderBytes <= 0.
package netutil

import (
	"net/http"
	"time"
)

// Defaults seguros para http.Server. Aplicados quando cfg não setou
// (== 0). Documentados em `pkg/config` (H14b).
const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 15 * time.Second
	DefaultWriteTimeout      = 15 * time.Second
	DefaultIdleTimeout       = 60 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20 // 1 MiB
)

// SafeServerConfig carrega os timeouts (strings Go duration) para
// aplicação no http.Server. Cada campo 0/vazio → default seguro.
type SafeServerConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

// SafeServer normaliza um http.Server aplicando defaults onde o
// caller passou 0. Não muta Addr, Handler, TLSConfig.
//
// CWE-400 (Uncontrolled Resource Consumption) — sem
// ReadHeaderTimeout, slow-loris (1 byte a cada 14s) mantém a conexão
// viva indefinidamente. SafeServer garante que o slow-loris defense
// nunca é desabilitado por bug de configuração.
//
// Retorna o mesmo ponteiro para chaining. Pure-function: não tem
// efeitos colaterais além de mutar o *http.Server in-place.
func SafeServer(s *http.Server, c SafeServerConfig) *http.Server {
	if s == nil {
		return nil
	}
	if c.ReadHeaderTimeout <= 0 {
		s.ReadHeaderTimeout = DefaultReadHeaderTimeout
	} else {
		s.ReadHeaderTimeout = c.ReadHeaderTimeout
	}
	if c.ReadTimeout <= 0 {
		s.ReadTimeout = DefaultReadTimeout
	} else {
		s.ReadTimeout = c.ReadTimeout
	}
	if c.WriteTimeout <= 0 {
		s.WriteTimeout = DefaultWriteTimeout
	} else {
		s.WriteTimeout = c.WriteTimeout
	}
	if c.IdleTimeout <= 0 {
		s.IdleTimeout = DefaultIdleTimeout
	} else {
		s.IdleTimeout = c.IdleTimeout
	}
	if c.MaxHeaderBytes <= 0 {
		s.MaxHeaderBytes = DefaultMaxHeaderBytes
	} else {
		s.MaxHeaderBytes = c.MaxHeaderBytes
	}
	return s
}
