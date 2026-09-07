package ws

import (
	"sync"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Hub is the single owner of connection tables, replay rings and sequence
// counters. Read/write loops never touch each other's maps; all fan-out goes
// through Hub so the hot path needs no per-connection mutex.
type Hub struct {
	mu     sync.RWMutex
	epoch  uint64
	rings  map[string]*Ring
	subs   map[string]map[*Conn]struct{}
	conns  map[*Conn]struct{}
	pool   *sync.Pool
	closed bool
}

// NewHub creates a hub with the given replay epoch (incremented on restart).
func NewHub(epoch uint64) *Hub {
	return &Hub{
		epoch: epoch,
		rings: map[string]*Ring{},
		subs:  map[string]map[*Conn]struct{}{},
		conns: map[*Conn]struct{}{},
		pool:  &sync.Pool{New: func() any { return make([]byte, 0, 4096) }},
	}
}

// Epoch returns the replay epoch.
func (h *Hub) Epoch() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.epoch
}

func (h *Hub) ringFor(sess string) *Ring {
	r, ok := h.rings[sess]
	if !ok {
		r = &Ring{}
		h.rings[sess] = r
	}
	return r
}

// Publish stores e in the session ring and fans out to subscribers.
// It assigns V/Seq/Epoch/Sess. Delivery is non-blocking: slow readers drop
// (they recover via session.events.since replay).
func (h *Hub) Publish(sess, typ string, kind types.FrameKind, data []byte) types.Envelope {
	h.mu.Lock()
	e := types.Envelope{V: types.ProtocolVersion, Kind: kind, Epoch: h.epoch, Sess: sess, Type: typ, Data: data}
	e = h.ringFor(sess).Push(e)
	subs := h.subs[sess]
	all := h.subs["*"]
	h.mu.Unlock()

	for c := range subs {
		c.emit(e)
	}
	for c := range all {
		if _, dup := subs[c]; !dup {
			c.emit(e)
		}
	}
	return e
}

// Subscribe registers c for sess ("*" = all sessions).
func (h *Hub) Subscribe(c *Conn, sess string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[c] = struct{}{}
	m, ok := h.subs[sess]
	if !ok {
		m = map[*Conn]struct{}{}
		h.subs[sess] = m
	}
	m[c] = struct{}{}
}

// Unsubscribe removes all registrations for c.
func (h *Hub) Unsubscribe(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	for _, m := range h.subs {
		delete(m, c)
	}
}

// Replay fetches missed envelopes for a reconnecting client.
func (h *Hub) Replay(sess string, last uint64, limit int) ([]types.Envelope, bool) {
	h.mu.Lock()
	r, ok := h.rings[sess]
	h.mu.Unlock()
	if !ok {
		return nil, true
	}
	return r.Since(last, limit)
}

// ConnCount reports live connections (gateway.status).
func (h *Hub) ConnCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns)
}
