package memory

import (
	"context"
	"time"
)

// Nudger runs periodic consolidation (auto-consolidation + nudge routine).
// It ticks on select + time.Ticker and stops on ctx cancel.
type Nudger struct {
	store    *Store
	interval time.Duration
	emit     func(ctx context.Context, text string)
}

// NewNudger builds the background routine (interval<=0 disables).
func NewNudger(store *Store, interval time.Duration, emit func(ctx context.Context, text string)) *Nudger {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	return &Nudger{store: store, interval: interval, emit: emit}
}

// Run blocks until ctx cancels, consolidating on every tick.
func (n *Nudger) Run(ctx context.Context) {
	t := time.NewTicker(n.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			snap := n.store.Load(ctx)
			if snap == "" {
				continue
			}
			if n.emit != nil {
				n.emit(ctx, snap)
			}
		}
	}
}
