package gateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// Adapter is one messaging channel (telegram/discord/slack/...).
// Mirrors gateway/platforms/base.py + plugins/platforms/*/adapter.py.
type Adapter interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, chatID, text string) error
	// SendReply answers one inbound message, preserving thread + reply
	// anchors (message_thread_id / message_reference). Send is the
	// anchor-less convenience form.
	SendReply(ctx context.Context, in Inbound, text string) error
	// Typing posts a best-effort typing indicator for the inbound chat.
	Typing(ctx context.Context, in Inbound)
}

// BusyGuard mirrors run_busy.py: two guards (turn + plain commands) with
// idle/plain-command bypass.
type BusyGuard struct {
	mu   sync.Mutex
	busy map[string]time.Time
}

// NewBusyGuard creates the guard.
func NewBusyGuard() *BusyGuard { return &BusyGuard{busy: map[string]time.Time{}} }

// TryAcquire claims a session turn; idle and plain slash commands bypass.
func (b *BusyGuard) TryAcquire(session, text string, plain func(string) bool) bool {
	if plain != nil && plain(text) {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if since, ok := b.busy[session]; ok && time.Since(since) < 10*time.Minute {
		return false
	}
	b.busy[session] = time.Now()
	return true
}

// Release frees the session.
func (b *BusyGuard) Release(session string) {
	b.mu.Lock()
	delete(b.busy, session)
	b.mu.Unlock()
}

// Inbound is one normalized incoming message from any platform.
type Inbound struct {
	Platform  string
	ChatID    string
	UserID    string
	UserName  string
	MessageID string
	Text      string
	ThreadID  string
}

// ErrBusy is returned when a session already runs a turn.
var ErrBusy = errors.New("gateway: session busy")

// UserAllowed enforces per-platform allowlists (written by `botreaper setup
// gateway`). An empty list means open access.
func UserAllowed(allowed []string, id string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if a == id {
			return true
		}
	}
	return false
}

// ParseAllowlist splits comma-separated user IDs.
func ParseAllowlist(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
