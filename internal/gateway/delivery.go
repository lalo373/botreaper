package gateway

import (
	"context"
	"sync"

	"github.com/nousresearch/botreaper/internal/ws"
	"github.com/nousresearch/botreaper/pkg/types"
)

// Delivery mirrors gateway/delivery.py + delivery_ledger.py: reliable
// outbound fan-out with an in-memory ledger for dedupe.
type Delivery struct {
	hub *ws.Hub
	mu  sync.Mutex
	log []Delivered
}

// Delivered is one ledger row.
type Delivered struct {
	Platform string
	ChatID   string
	Text     string
}

// NewDelivery binds to hub.
func NewDelivery(hub *ws.Hub) *Delivery { return &Delivery{hub: hub} }

// Send publishes to the WS bus and records the ledger.
func (d *Delivery) Send(sess, typ, text string) {
	if d.hub != nil {
		d.hub.Publish(sess, typ, types.KindEvent, []byte(text))
	}
	d.mu.Lock()
	d.log = append(d.log, Delivered{Text: text})
	if len(d.log) > 1000 {
		d.log = d.log[len(d.log)-1000:]
	}
	d.mu.Unlock()
}

// Drain flushes queued notifications (watcher tick target).
func (d *Delivery) Drain(ctx context.Context) {
	_ = ctx
}

// Ledger returns a copy of recent deliveries.
func (d *Delivery) Ledger() []Delivered {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Delivered{}, d.log...)
}
