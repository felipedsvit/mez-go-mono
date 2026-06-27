//go:build !integration
// +build !integration

package netutil_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/testutil"
	"github.com/felipedsvit/mez-go-mono/pkg/netutil"
)

func TestMain(m *testing.M) {
	testutil.VerifyTestMain(m)
}

func TestSafeServer_AppliesDefaultsWhenZero(t *testing.T) {
	s := &http.Server{Addr: ":8080"}
	netutil.SafeServer(s, netutil.SafeServerConfig{})

	if s.ReadHeaderTimeout != netutil.DefaultReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", s.ReadHeaderTimeout, netutil.DefaultReadHeaderTimeout)
	}
	if s.ReadTimeout != netutil.DefaultReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", s.ReadTimeout, netutil.DefaultReadTimeout)
	}
	if s.WriteTimeout != netutil.DefaultWriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", s.WriteTimeout, netutil.DefaultWriteTimeout)
	}
	if s.IdleTimeout != netutil.DefaultIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", s.IdleTimeout, netutil.DefaultIdleTimeout)
	}
	if s.MaxHeaderBytes != netutil.DefaultMaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", s.MaxHeaderBytes, netutil.DefaultMaxHeaderBytes)
	}
}

func TestSafeServer_PreservesExplicitValues(t *testing.T) {
	s := &http.Server{Addr: ":8080"}
	want := netutil.SafeServerConfig{
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      25 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    4 << 20, // 4 MiB
	}
	netutil.SafeServer(s, want)

	if s.ReadHeaderTimeout != want.ReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", s.ReadHeaderTimeout, want.ReadHeaderTimeout)
	}
	if s.ReadTimeout != want.ReadTimeout {
		t.Errorf("ReadTimeout = %v, want %v", s.ReadTimeout, want.ReadTimeout)
	}
	if s.WriteTimeout != want.WriteTimeout {
		t.Errorf("WriteTimeout = %v, want %v", s.WriteTimeout, want.WriteTimeout)
	}
	if s.IdleTimeout != want.IdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", s.IdleTimeout, want.IdleTimeout)
	}
	if s.MaxHeaderBytes != want.MaxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", s.MaxHeaderBytes, want.MaxHeaderBytes)
	}
}

// TestSafeServer_RejectsZeroAsAntiSlowLoris é o teste de regressão
// para o issue #143: garante que SafeServer nunca deixa
// ReadHeaderTimeout=0 (slow-loris defense desabilitada).
func TestSafeServer_RejectsZeroAsAntiSlowLoris(t *testing.T) {
	s := &http.Server{Addr: ":8080"}
	netutil.SafeServer(s, netutil.SafeServerConfig{
		// Caller "esqueceu" de setar ReadHeaderTimeout; SafeServer DEVE
		// aplicar default, NÃO deixar 0.
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		IdleTimeout:    30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	})
	if s.ReadHeaderTimeout == 0 {
		t.Fatal("ReadHeaderTimeout = 0 após SafeServer; slow-loris defense desabilitada (Issue #143)")
	}
	if s.ReadHeaderTimeout < time.Second {
		t.Errorf("ReadHeaderTimeout = %v, esperado >= 1s", s.ReadHeaderTimeout)
	}
}

func TestSafeServer_NilServer(t *testing.T) {
	// Não deve panic com server nil.
	if got := netutil.SafeServer(nil, netutil.SafeServerConfig{}); got != nil {
		t.Errorf("SafeServer(nil) = %v, want nil", got)
	}
}
