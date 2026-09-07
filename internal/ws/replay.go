// Package ws implements the full-duplex WebSocket gateway.
// Ports tui_gateway/ws.py, transport.py, server.py dispatch, event_replay.py.
// There is no HTTP polling: state sync, token streams, pty streams and
// progress all travel over these channels.
package ws

import (
	"sync"

	"github.com/nousresearch/botreaper/pkg/types"
)

const (
	// ReplayDepth mirrors the tui_gateway seq+replay ring size.
	ReplayDepth = 1000
	// PingIntervalSecs / KillAfterSecs mirror json-rpc-gateway.ts heartbeats.
	PingIntervalSecs = 15
	KillAfterSecs    = 45
	// ConnectTimeoutSecs / RPCTimeoutSecs mirror the TS client defaults.
	ConnectTimeoutSecs = 15
	RPCTimeoutSecs     = 120
	// CoalesceMillis mirrors the 33ms message/reasoning/thinking.delta merge.
	CoalesceMillis = 33
)

// Ring is a bounded per-session replay buffer of envelopes.
type Ring struct {
	mu   sync.Mutex
	buf  []types.Envelope
	next uint64
}

// Push appends e, assigning its Seq, and drops the oldest past ReplayDepth.
func (r *Ring) Push(e types.Envelope) types.Envelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	e.Seq = r.next
	if len(r.buf) >= ReplayDepth {
		copy(r.buf, r.buf[1:])
		r.buf[len(r.buf)-1] = e
	} else {
		r.buf = append(r.buf, e)
	}
	return e
}

// Since returns envelopes with Seq > last, newest-first capped at limit.
// ok=false when the ring has rotated past last (caller must full-resync).
func (r *Ring) Since(last uint64, limit int) ([]types.Envelope, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return nil, true
	}
	oldest := r.buf[0].Seq
	if last < oldest-1 && last != 0 {
		return nil, false
	}
	var out []types.Envelope
	for _, e := range r.buf {
		if e.Seq > last {
			out = append(out, e)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, true
}

// Last returns the newest sequence number, or 0 when empty.
func (r *Ring) Last() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.next
}
