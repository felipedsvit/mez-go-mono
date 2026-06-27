package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// fakeClock returns a Clock whose Now() returns the most recently Set value.
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

func makeSession(id string, userID admin.AdminUserID) admin.Session {
	return admin.Session{
		ID:        admin.SessionID(id),
		UserID:    userID,
		Email:     "u@example.com",
		ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestSession_SaveGet(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	sess := makeSession("sid-1", "u1")

	if err := store.Save(ctx, sess, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.Get(ctx, admin.SessionID("sid-1"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("got id %q want %q", got.ID, sess.ID)
	}
}

func TestSession_Expired_Removed(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	sess := makeSession("sid-exp", "u1")
	if err := store.Save(ctx, sess, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Advance past TTL
	clk.Set(clk.Now().Add(2 * time.Hour))

	_, err := store.Get(ctx, admin.SessionID("sid-exp"))
	if err != admin.ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired, got %v", err)
	}

	// After Get removed it, the store is empty.
	if _, ok := store.sessions[admin.SessionID("sid-exp")]; ok {
		t.Errorf("expected entry removed after Get-on-expired")
	}
}

func TestSession_Reaper_NoGoroutineLeak(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStore(clk)
	t.Cleanup(store.StopReaper)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store.StartReaper(ctx, 50*time.Millisecond)

	// Let the reaper run a few ticks
	time.Sleep(200 * time.Millisecond)

	// Stop and let the goroutine exit. goleak.VerifyTestMain will catch any
	// remaining background goroutines tied to this package.
	store.StopReaper()

	// Give the reaper goroutine a moment to exit
	time.Sleep(50 * time.Millisecond)
}

func TestSession_Reaper_RemovesExpired(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	sess := makeSession("sid-r", "u1")
	if err := store.Save(ctx, sess, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Manually trigger reap after advancing the clock
	clk.Set(clk.Now().Add(2 * time.Hour))
	store.reapOnce()

	store.mu.RLock()
	_, present := store.sessions[admin.SessionID("sid-r")]
	store.mu.RUnlock()
	if present {
		t.Errorf("reapOnce did not remove expired session")
	}
}

func TestSession_Delete(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	sess := makeSession("sid-d", "u1")
	if err := store.Save(ctx, sess, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := store.Delete(ctx, admin.SessionID("sid-d")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, admin.SessionID("sid-d")); err != admin.ErrSessionExpired {
		t.Errorf("expected ErrSessionExpired after delete, got %v", err)
	}
}

func TestState_SaveLoad(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	state := admin.OIDCState{
		State:         "abc",
		CodeVerifier:  "verifier",
		RedirectAfter: "/admin/",
	}
	if err := store.SaveState(ctx, "abc", state, 300); err != nil {
		t.Fatalf("save state: %v", err)
	}
	got, err := store.LoadState(ctx, "abc")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got.CodeVerifier != "verifier" {
		t.Errorf("got verifier %q want %q", got.CodeVerifier, "verifier")
	}
}

func TestState_Expired_Removed(t *testing.T) {
	clk := newFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
	store := NewSessionStoreWithoutReaper(clk)
	t.Cleanup(store.StopReaper)

	ctx := context.Background()
	state := admin.OIDCState{State: "abc", CodeVerifier: "v"}
	if err := store.SaveState(ctx, "abc", state, 60); err != nil {
		t.Fatalf("save: %v", err)
	}

	clk.Set(clk.Now().Add(2 * time.Minute))
	_, err := store.LoadState(ctx, "abc")
	if err != admin.ErrNotFound {
		t.Errorf("expected ErrNotFound for expired state, got %v", err)
	}
}
