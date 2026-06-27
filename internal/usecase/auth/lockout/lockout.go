// Package lockout implements login-attempt rate-limiting per email. After
// MEZ_LOGIN_MAX_FAILS consecutive failures within MEZ_LOGIN_LOCKOUT_WINDOW,
// the email is locked out and LoginLocal returns admin.ErrTooManyAttempts
// without consulting the password store. This is per-email (not per-IP) —
// mez-go's spec: behind a proxy the IP can be shared; the email is the
// stable identity.
//
// The tracker is in-memory (sync.Map). On restart, counters reset — this
// is acceptable for a defense-in-depth layer; the DB lookup still gates
// on the password hash.
package lockout

import (
	"sync"
	"time"
)

// Clock is a pluggable time source for tests. Production uses time.Now.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// Tracker records failed login attempts per email and locks out emails that
// exceed the configured maximum within the configured window.
type Tracker struct {
	maxFails int
	window   time.Duration
	clock    Clock

	mu       sync.Mutex
	failures map[string]*bucket
}

type bucket struct {
	count   int
	firstAt time.Time
}

// New returns a Tracker with the given threshold and window. If maxFails <= 0
// the tracker is a no-op (never locks anyone out — useful for tests). If
// clock is nil, a real clock is used.
func New(maxFails int, window time.Duration, clock Clock) *Tracker {
	if clock == nil {
		clock = realClock{}
	}
	return &Tracker{
		maxFails: maxFails,
		window:   window,
		clock:    clock,
		failures: make(map[string]*bucket),
	}
}

// RecordFailure increments the counter for email. Returns true if the email
// is now locked out (caller should refuse to even check the password).
func (t *Tracker) RecordFailure(email string) bool {
	if t.maxFails <= 0 {
		return false
	}
	email = normalize(email)
	now := t.clock.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	b, ok := t.failures[email]
	if !ok || now.Sub(b.firstAt) > t.window {
		t.failures[email] = &bucket{count: 1, firstAt: now}
		return false
	}
	b.count++
	return b.count > t.maxFails
}

// IsLockedOut returns true if email is currently locked. Read-only (no
// counter change). Useful for /login to short-circuit before the password
// lookup.
func (t *Tracker) IsLockedOut(email string) bool {
	if t.maxFails <= 0 {
		return false
	}
	email = normalize(email)
	now := t.clock.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	b, ok := t.failures[email]
	if !ok {
		return false
	}
	if now.Sub(b.firstAt) > t.window {
		delete(t.failures, email)
		return false
	}
	return b.count > t.maxFails
}

// RecordSuccess clears the failure counter for email (legitimate user
// managed to log in — give them a fresh window).
func (t *Tracker) RecordSuccess(email string) {
	email = normalize(email)
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, email)
}

// Reset clears all state. Intended for ops/CLI ("unlock everyone").
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = make(map[string]*bucket)
}

func normalize(email string) string {
	// Lowercase + trim to match admin.NormalizeEmail semantics.
	out := make([]byte, 0, len(email))
	for i := 0; i < len(email); i++ {
		c := email[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	// Trim
	start, end := 0, len(out)
	for start < end && (out[start] == ' ' || out[start] == '\t') {
		start++
	}
	for end > start && (out[end-1] == ' ' || out[end-1] == '\t') {
		end--
	}
	return string(out[start:end])
}
