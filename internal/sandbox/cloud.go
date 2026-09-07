package sandbox

import (
	"context"
	"errors"
)

// cloudBackend covers Modal/Daytona API-backed sandboxes. The Python tree
// shells to their CLIs/SDKs; the Go port keeps the same boundary: an
// injectable runner (CLI or HTTP) so tests never need cloud credentials.
type cloudBackend struct {
	name    string
	runner  func(ctx context.Context, cmd string, out chan<- Chunk) (int, error)
	enabled bool
}

func (b *cloudBackend) Name() string    { return b.name }
func (b *cloudBackend) Available() bool { return b.enabled && b.runner != nil }
func (b *cloudBackend) Exec(ctx context.Context, cmd string, out chan<- Chunk) (int, error) {
	if !b.Available() {
		return 127, errors.New("sandbox: backend " + b.name + " not configured")
	}
	return b.runner(ctx, cmd, out)
}

// NewModal mirrors environments/modal.py (managed_modal + modal_utils).
func NewModal(runner func(ctx context.Context, cmd string, out chan<- Chunk) (int, error)) Backend {
	return &cloudBackend{name: "modal", runner: runner, enabled: runner != nil}
}

// NewDaytona mirrors environments/daytona.py.
func NewDaytona(runner func(ctx context.Context, cmd string, out chan<- Chunk) (int, error)) Backend {
	return &cloudBackend{name: "daytona", runner: runner, enabled: runner != nil}
}
