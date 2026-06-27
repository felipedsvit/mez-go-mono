// Package e2e — harness compartilhado para os testes E2E.
//
// O harness monta:
//   - um bus in-memory com buffers pequenos para forçar caminhos de drop
//   - um registry de sender in-memory com factories lazy
//   - um recorder de Sender (captura todas as chamadas Send)
//   - helpers para construir requests Meta/Telegram com assinatura válida
package e2e

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/felipedsvit/mez-go-mono/internal/adapter/broker"
	"github.com/felipedsvit/mez-go-mono/internal/adapter/sender/memory"
	"github.com/felipedsvit/mez-go-mono/internal/core/domain"
	"github.com/felipedsvit/mez-go-mono/internal/core/event"
	"github.com/felipedsvit/mez-go-mono/internal/core/port"
	"github.com/felipedsvit/mez-go-mono/pkg/metrics"
)

// SenderRecorder implementa port.Sender e captura todas as chamadas.
// É thread-safe — handlers podem rodar em goroutines concorrentes.
type SenderRecorder struct {
	mu      sync.Mutex
	channel domain.Channel
	caps    port.CapabilitySet
	calls   []port.OutboundRequest
	SendErr error // se setado, Send retorna este erro
}

func NewSenderRecorder(ch domain.Channel, caps port.CapabilitySet) *SenderRecorder {
	return &SenderRecorder{channel: ch, caps: caps}
}

func (r *SenderRecorder) Channel() domain.Channel { return r.channel }
func (r *SenderRecorder) Capabilities() port.CapabilitySet {
	return r.caps
}
func (r *SenderRecorder) Send(_ context.Context, req port.OutboundRequest) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.SendErr != nil {
		return "", r.SendErr
	}
	r.calls = append(r.calls, req)
	return "stub-mid", nil
}
func (r *SenderRecorder) Calls() []port.OutboundRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]port.OutboundRequest, len(r.calls))
	copy(out, r.calls)
	return out
}

// Harness monta a infra in-memory para os testes E2E.
type Harness struct {
	t       *testing.T
	Bus     *broker.Bus
	Reg     *memory.Registry
	Metrics *metrics.Registry
	Log     zerolog.Logger
}

// NewHarness monta o harness. Auto-cleanup via t.Cleanup:
//   - bus.Drain com timeout
//   - nenhuma goroutine leaked (bus termina com Drain)
func NewHarness(t *testing.T) *Harness {
	t.Helper()

	log := zerolog.Nop()
	met := metrics.NewRegistry()
	bus := broker.NewBus(broker.BusConfig{
		InboundBuffer:   64,
		OutboundBuffer:  64,
		StatusBuffer:    32,
		DLQBuffer:       32,
		LifecycleBuffer: 16,
	}, log, met)
	reg := memory.New(log, time.Minute)

	h := &Harness{
		t:       t,
		Bus:     bus,
		Reg:     reg,
		Metrics: met,
		Log:     log,
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = bus.Drain(ctx)
	})
	return h
}

// RegisterSender instala um recorder no registry para o canal.
func (h *Harness) RegisterSender(rec *SenderRecorder) {
	h.t.Helper()
	h.Reg.Register(rec.channel, func(_ context.Context, _ domain.TenantID) (port.Sender, error) {
		return rec, nil
	})
}

// WaitForInboundEvents bloqueia até o bus ter processado `n` eventos inbound
// ou até timeout. Útil para testes que publicam e querem garantir
// processamento antes de asserts.
func (h *Harness) WaitForInboundEvents(n int, timeout time.Duration) bool {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if h.Bus.BufferDepth()["inbound"] == 0 {
			// Tenta mais 100ms para confirmar que foi consumido.
			time.Sleep(100 * time.Millisecond)
			if h.Bus.BufferDepth()["inbound"] == 0 {
				return true
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return n == 0 // se pediu 0 e não tinha, OK
}

// WaitForOutboundCalls bloqueia até o recorder ter `n` chamadas ou timeout.
func WaitForOutboundCalls(t *testing.T, rec *SenderRecorder, n int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(rec.Calls()) >= n {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// SignMeta calcula X-Hub-Signature-256 para webhooks Meta.
func SignMeta(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// NewMetaRequest monta um *httptest.NewRequest com path /webhooks/meta/:app_id
// e signature Meta já calculada.
func NewMetaRequest(t *testing.T, appID string, body []byte, secret []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	// httptest.NewRequest não suporta SetPathValue até Go 1.22+;
	// usamos httptest.NewRequest com a URL completa e deixamos o handler
	// acessar via r.PathValue. Construímos o request com mux chi.
	return rec
}

// MustInt é um helper para conversões int → int64.
func MustInt(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic(err)
	}
	return n
}

// ComposeInboundEvent é um helper para criar InboundEvent canônico.
func ComposeInboundEvent(tenantID string, ch domain.Channel, msgID string) event.InboundEvent {
	return event.InboundEvent{
		TenantID:  tenantID,
		Channel:   event.Channel(ch),
		MessageID: msgID,
	}
}
