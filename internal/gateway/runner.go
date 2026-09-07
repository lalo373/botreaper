// Package gateway implements the multi-channel event router.
// Ports gateway/run*.py, session*.py, slash_commands*.py, delivery.py,
// stream_*.py, relay/, pairing.py.
package gateway

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/nousresearch/botreaper/internal/ws"
)

// Runner is the gateway facade (gateway/run.py): owns adapters, turn
// dispatch, watchers and shutdown.
type Runner struct {
	hub      *ws.Hub
	adapters []Adapter
	busy     *BusyGuard
	slash    *SlashTable
	delivery *Delivery
	mu       sync.Mutex
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewRunner builds a gateway over hub.
func NewRunner(hub *ws.Hub) *Runner {
	return &Runner{hub: hub, busy: NewBusyGuard(), slash: NewSlashTable(), delivery: NewDelivery(hub)}
}

// Use registers a channel adapter.
func (r *Runner) Use(a Adapter) { r.adapters = append(r.adapters, a) }

// Slash exposes the command table.
func (r *Runner) Slash() *SlashTable { return r.slash }

// Start launches adapter supervisors + watchers; blocks until ctx cancels.
func (r *Runner) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	g, gctx := errgroup.WithContext(ctx)
	for _, a := range r.adapters {
		a := a
		g.Go(func() error {
			t := time.NewTicker(60 * time.Second)
			defer t.Stop()
			if err := a.Start(gctx); err != nil {
				return err
			}
			<-gctx.Done()
			_ = t
			return a.Stop()
		})
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				r.delivery.Drain(ctx)
			}
		}
	}()
	err := g.Wait()
	r.wg.Wait()
	return err
}

// Stop tears the gateway down.
func (r *Runner) Stop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}
