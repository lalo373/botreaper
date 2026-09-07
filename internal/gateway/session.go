package gateway

import (
	"context"
	"sync"
	"time"
)

// Session tracks one gateway peer binding (session_state.py +
// session_lifecycle.py + session_persistence.py condensed).
type Session struct {
	Key      string
	Peer     string
	Platform string
	Profile  string
	Updated  time.Time
}

// SessionTable is the in-memory peer->session index; durable routing rows
// live in internal/state (gateway_routing).
type SessionTable struct {
	mu   sync.RWMutex
	rows map[string]Session // scope -> session
}

// NewSessionTable creates the index.
func NewSessionTable() *SessionTable { return &SessionTable{rows: map[string]Session{}} }

// Bind records scope -> session.
func (t *SessionTable) Bind(scope string, s Session) {
	t.mu.Lock()
	s.Updated = time.Now()
	t.rows[scope] = s
	t.mu.Unlock()
}

// Lookup resolves scope.
func (t *SessionTable) Lookup(scope string) (Session, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.rows[scope]
	return s, ok
}

// AdoptOrphaned reaps sessions whose backend heartbeat died (startup orphan
// reap + heartbeat gate, mirrors sweep_orphaned_sessions).
func (t *SessionTable) AdoptOrphaned(maxAge time.Duration) []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []Session
	now := time.Now()
	for scope, s := range t.rows {
		if now.Sub(s.Updated) > maxAge {
			out = append(out, s)
			delete(t.rows, scope)
		}
	}
	return out
}

var _ = context.Background
