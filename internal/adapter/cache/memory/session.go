// Package memory provides an in-memory implementation of admin.SessionStore
// and admin.StateStore for Phase 1. Phase 7 replaces it with a Redis-backed
// implementation. The reaper goroutine is started on New and is tied to the
// lifecycle of the context passed to StartReaper; cancel the context to stop
// the reaper (no leak).
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/felipedsvit/mez-go-mono/internal/core/admin"
)

// Clock returns the current time. The default uses time.Now; tests inject
// a fake clock to make TTL assertions deterministic.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// SessionStore is an in-memory session store with a background reaper.
// It is safe for concurrent use.
type SessionStore struct {
	mu       sync.RWMutex
	clock    Clock
	sessions map[admin.SessionID]sessionEntry
	state    map[string]stateEntry

	reaperCancel context.CancelFunc
}

// NewSessionStore returns a new store with a real clock. The reaper is NOT
// started automatically — call StartReaper to start it (and cancel the
// returned context to stop). Tests can call NewSessionStoreWithoutReaper
// to get a store without a background goroutine.
func NewSessionStore(clock Clock) *SessionStore {
	if clock == nil {
		clock = realClock{}
	}
	return &SessionStore{
		clock:    clock,
		sessions: make(map[admin.SessionID]sessionEntry),
		state:    make(map[string]stateEntry),
	}
}

// NewSessionStoreWithoutReaper is an alias kept for tests; reaper never
// starts in this mode (also reachable via NewSessionStore + no StartReaper).
func NewSessionStoreWithoutReaper(clock Clock) *SessionStore {
	return NewSessionStore(clock)
}

// StartReaper launches the background goroutine that removes expired
// sessions and state entries. The interval determines how often the sweep
// runs. Cancelling ctx stops the goroutine; the reaper respects ctx.Done().
func (s *SessionStore) StartReaper(ctx context.Context, interval time.Duration) {
	c, cancel := context.WithCancel(ctx)
	s.reaperCancel = cancel
	go s.reapLoop(c, interval)
}

func (s *SessionStore) reapLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reapOnce()
		}
	}
}

// StopReaper cancels the reaper goroutine. Safe to call multiple times.
func (s *SessionStore) StopReaper() {
	if s.reaperCancel != nil {
		s.reaperCancel()
		s.reaperCancel = nil
	}
}

func (s *SessionStore) reapOnce() {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.sessions {
		if !now.Before(e.expires) {
			delete(s.sessions, id)
		}
	}
	for k, e := range s.state {
		if !now.Before(e.expires) {
			delete(s.state, k)
		}
	}
}

type sessionEntry struct {
	session admin.Session
	expires time.Time
}

type stateEntry struct {
	state   admin.OIDCState
	expires time.Time
}

// Save stores a session with the given TTL.
func (s *SessionStore) Save(ctx context.Context, session admin.Session, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = sessionEntry{
		session: session,
		expires: s.clock.Now().Add(ttl),
	}
	return nil
}

// Get returns the session or admin.ErrSessionExpired if missing/expired.
// Expired entries are reaped on read.
func (s *SessionStore) Get(ctx context.Context, id admin.SessionID) (admin.Session, error) {
	s.mu.RLock()
	entry, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return admin.Session{}, admin.ErrSessionExpired
	}
	if !s.clock.Now().Before(entry.expires) {
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		return admin.Session{}, admin.ErrSessionExpired
	}
	return entry.session, nil
}

// Delete removes a session. No-op if not present.
func (s *SessionStore) Delete(ctx context.Context, id admin.SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

// SaveState stores an OIDC state with TTL in seconds.
func (s *SessionStore) SaveState(ctx context.Context, key string, state admin.OIDCState, ttlSeconds int) error {
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = stateEntry{
		state:   state,
		expires: s.clock.Now().Add(time.Duration(ttlSeconds) * time.Second),
	}
	return nil
}

// LoadState retrieves an OIDC state or admin.ErrNotFound if missing/expired.
func (s *SessionStore) LoadState(ctx context.Context, key string) (admin.OIDCState, error) {
	s.mu.RLock()
	entry, ok := s.state[key]
	s.mu.RUnlock()
	if !ok {
		return admin.OIDCState{}, admin.ErrNotFound
	}
	if !s.clock.Now().Before(entry.expires) {
		s.mu.Lock()
		delete(s.state, key)
		s.mu.Unlock()
		return admin.OIDCState{}, admin.ErrNotFound
	}
	return entry.state, nil
}

// DeleteState removes an OIDC state. No-op if not present.
func (s *SessionStore) DeleteState(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state, key)
	return nil
}
