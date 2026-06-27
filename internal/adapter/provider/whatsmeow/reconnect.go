// Package whatsmeow — reconnect.go: AutoReconnect (backoff E5) + Disconnect (D10) + Warmup (E6).
//
// ReconnectThrottle: após 429 (rate limit) ou 515 (restart), backoff
// exponencial de 60s até 30min. Reset em Connected.
//
// Warmup: 10-day ramp (E6 do pai). Cota diária: 0/0/0/0/10/30/50/80/100/100 →
// 200 steady. Estado persistido em whatsapp_account_state (migration 0004).
//
// Disconnect: D10 — graceful. Manager.DisconnectAll() no shutdown
// coordenado.
package whatsmeow

import (
	"context"
	"strings"
	"sync"
	"time"
)

// ReconnectThrottle espalha as reconexões num intervalo mínimo e aplica
// backoff exponencial para 429/515.
type ReconnectThrottle struct {
	minInterval time.Duration
	maxBackoff  time.Duration

	mu      sync.Mutex
	backoff time.Duration
	last    time.Time
}

// NewReconnectThrottle cria o throttle. minInterval<=0 usa 60s.
func NewReconnectThrottle(minInterval time.Duration) *ReconnectThrottle {
	if minInterval <= 0 {
		minInterval = 60 * time.Second
	}
	return &ReconnectThrottle{minInterval: minInterval, maxBackoff: 30 * time.Minute}
}

// Next calcula o atraso até a próxima reconexão dado o erro.
// Devolve (wait, reason): reason ∈ {"backoff", "throttled"}.
func (t *ReconnectThrottle) Next(err error, now time.Time) (time.Duration, string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	reason := "throttled"
	if isBackoffError(err) {
		if t.backoff == 0 {
			t.backoff = t.minInterval
		} else {
			t.backoff *= 2
			if t.backoff > t.maxBackoff {
				t.backoff = t.maxBackoff
			}
		}
		reason = "backoff"
	} else {
		t.backoff = 0
	}

	base := t.minInterval
	if t.backoff > base {
		base = t.backoff
	}

	var wait time.Duration
	if t.last.IsZero() {
		wait = base
	} else if elapsed := now.Sub(t.last); elapsed < base {
		wait = base - elapsed
	}
	t.last = now.Add(wait)
	return wait, reason
}

// Reset zera o backoff após uma reconexão bem-sucedida.
func (t *ReconnectThrottle) Reset() {
	t.mu.Lock()
	t.backoff = 0
	t.mu.Unlock()
}

// isBackoffError reconhece 429 (rate limit) e 515 (restart).
func isBackoffError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "515")
}

// warmupQuota é a cota diária por dia de aquecimento.
var warmupQuota = [10]int{0, 0, 0, 0, 10, 30, 50, 80, 100, 100}

// warmupSteady é o teto após dia 10.
const warmupSteady = 200

// Warmup aplica o programa de 10 dias (E6). Decidese se um envio é permitido
// conforme a cota do dia + timelocks.
type Warmup struct {
	start     time.Time
	tenant    string
	jid       string
	stateR    whatsappStateSaver
	mu        sync.Mutex
	daySent   int
	dayAnchor time.Time
	health    int
	timelock  time.Time
}

// whatsappStateSaver é o subset do repo que Warmup precisa (interface
// desacoplada para testes).
type whatsappStateSaver interface {
	LoadWarmupState(ctx context.Context, tenant string, jid string) (daySent int, dayAnchor, timelock time.Time, health int)
	SaveWarmupState(ctx context.Context, tenant string, jid string, daySent int, dayAnchor, timelock time.Time, health int) error
}

// NewWarmup cria o controller.
func NewWarmup(tenant, jid string, store whatsappStateSaver) *Warmup {
	return &Warmup{
		start:     time.Now().UTC(),
		tenant:    tenant,
		jid:       jid,
		stateR:    store,
		dayAnchor: time.Now().UTC().Truncate(24 * time.Hour),
		health:    100,
	}
}

// Load restaura o estado persistido.
func (w *Warmup) Load(ctx context.Context) {
	if w.stateR == nil {
		return
	}
	ds, da, tl, h := w.stateR.LoadWarmupState(ctx, w.tenant, w.jid)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.daySent = ds
	w.dayAnchor = da
	w.timelock = tl
	if h > 0 {
		w.health = h
	}
}

// AllowSend decide se o envio é permitido.
func (w *Warmup) AllowSend(now time.Time, unknownContact bool) (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if unknownContact && now.Before(w.timelock) {
		return false, "timelock"
	}

	w.rollDay(now)
	q := w.quota(now)
	if w.daySent >= q {
		return false, "quota"
	}
	return true, ""
}

// RecordSent contabiliza um envio.
func (w *Warmup) RecordSent(ctx context.Context, now time.Time) {
	w.mu.Lock()
	w.rollDay(now)
	w.daySent++
	ds, da, tl, h := w.daySent, w.dayAnchor, w.timelock, w.health
	w.mu.Unlock()
	if w.stateR != nil {
		_ = w.stateR.SaveWarmupState(ctx, w.tenant, w.jid, ds, da, tl, h)
	}
}

// TriggerTimelock ativa 24h de timelock para desconhecidos.
func (w *Warmup) TriggerTimelock(ctx context.Context, now time.Time) {
	until := now.Add(24 * time.Hour)
	w.mu.Lock()
	if until.After(w.timelock) {
		w.timelock = until
	}
	ds, da, tl, h := w.daySent, w.dayAnchor, w.timelock, w.health
	w.mu.Unlock()
	if w.stateR != nil {
		_ = w.stateR.SaveWarmupState(ctx, w.tenant, w.jid, ds, da, tl, h)
	}
}

// Penalize reduz health score.
func (w *Warmup) Penalize(ctx context.Context, delta int) {
	w.mu.Lock()
	w.health -= delta
	if w.health < 0 {
		w.health = 0
	}
	ds, da, tl, h := w.daySent, w.dayAnchor, w.timelock, w.health
	w.mu.Unlock()
	if w.stateR != nil {
		_ = w.stateR.SaveWarmupState(ctx, w.tenant, w.jid, ds, da, tl, h)
	}
}

// HealthScore retorna o score atual.
func (w *Warmup) HealthScore() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.health
}

func (w *Warmup) rollDay(now time.Time) {
	today := now.UTC().Truncate(24 * time.Hour)
	if !w.dayAnchor.Equal(today) {
		w.dayAnchor = today
		w.daySent = 0
	}
}

func (w *Warmup) quota(now time.Time) int {
	d := int(now.UTC().Sub(w.start).Hours() / 24)
	if d < 0 {
		return 0
	}
	if d >= len(warmupQuota) {
		return warmupSteady
	}
	return warmupQuota[d]
}
