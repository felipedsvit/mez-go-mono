package lockout

import (
	"sync"
	"testing"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}
func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func TestTracker_BelowThreshold_NotLocked(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(3, 15*time.Minute, clk)

	// Issue #156 (M10): maxFails=3 means lock on the 3rd attempt.
	for i := 0; i < 2; i++ {
		if tr.RecordFailure("user@example.com") {
			t.Errorf("attempt %d should not lock", i+1)
		}
	}
	if tr.IsLockedOut("user@example.com") {
		t.Errorf("2 failures in window should not lock (maxFails=3 means lock on 3rd)")
	}
}

func TestTracker_AtThreshold_Locked(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(3, 15*time.Minute, clk)

	// Issue #156 (M10): the 3rd attempt triggers lockout (>= maxFails).
	var locked bool
	for i := 0; i < 3; i++ {
		locked = tr.RecordFailure("user@example.com")
	}
	if !locked {
		t.Errorf("3rd attempt should lock (maxFails=3)")
	}
	if !tr.IsLockedOut("user@example.com") {
		t.Errorf("IsLockedOut should be true after 3 failures")
	}
}

func TestTracker_WindowExpiry_Resets(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(3, 15*time.Minute, clk)

	// 2 fails (below threshold).
	for i := 0; i < 2; i++ {
		tr.RecordFailure("user@example.com")
	}

	// Advance past window.
	clk.Set(clk.Now().Add(20 * time.Minute))

	// After window expiry, the next 2 failures should NOT lock.
	if tr.RecordFailure("user@example.com") {
		t.Errorf("after window expiry, first failure should not lock")
	}
	if tr.RecordFailure("user@example.com") {
		t.Errorf("after window expiry, 2nd failure should not lock")
	}
}

func TestTracker_Success_Clears(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(3, 15*time.Minute, clk)

	// 2 fails.
	for i := 0; i < 2; i++ {
		tr.RecordFailure("user@example.com")
	}
	tr.RecordSuccess("user@example.com")
	// After success, counter is cleared; 2 more failures should not lock.
	for i := 0; i < 2; i++ {
		if tr.RecordFailure("user@example.com") {
			t.Errorf("after success, fresh count should not lock at attempt %d", i+1)
		}
	}
}

func TestTracker_Normalization(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(2, 15*time.Minute, clk)

	tr.RecordFailure("  User@Example.COM  ")
	// 2nd attempt with same email (any case/whitespace) should lock.
	if !tr.RecordFailure("USER@example.com") {
		t.Errorf("normalization failed: same email with different case should accumulate to lock on 2nd")
	}
}

func TestTracker_DifferentEmails_Independent(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(2, 15*time.Minute, clk)

	// 2 fails para a@... (locks).
	tr.RecordFailure("a@example.com")
	tr.RecordFailure("a@example.com")
	if !tr.IsLockedOut("a@example.com") {
		t.Fatalf("a@ should be locked")
	}
	if tr.IsLockedOut("b@example.com") {
		t.Errorf("different email should not be locked")
	}
}

func TestTracker_DisabledThreshold_NoLockout(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(0, 15*time.Minute, clk) // disabled

	for i := 0; i < 100; i++ {
		if tr.RecordFailure("user@example.com") {
			t.Errorf("disabled tracker should never lock")
		}
	}
}

func TestTracker_Reset(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	tr := New(2, 15*time.Minute, clk)

	tr.RecordFailure("a@example.com")
	tr.RecordFailure("a@example.com") // 2nd triggers lock
	if !tr.IsLockedOut("a@example.com") {
		t.Fatalf("expected lockout")
	}

	tr.Reset()
	if tr.IsLockedOut("a@example.com") {
		t.Errorf("Reset should clear lockout")
	}
}

// Sanity: ensure admin.NormalizeEmail semantics are compatible with ours.
func TestTracker_MatchesAdminNormalize(t *testing.T) {
	tests := []string{
		"  User@Example.COM  ",
		"u@e.co",
		"",
	}
	for _, e := range tests {
		ours := normalize(e)
		admin := admin.NormalizeEmail(e)
		if ours != admin {
			t.Errorf("normalize(%q): ours=%q, admin=%q (must match)", e, ours, admin)
		}
	}
}
