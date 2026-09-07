// Package subagent implements async child-agent delegation.
// Ports tools/delegate_tool*.py + async_delegation.py
// (max_concurrent_children=3, max_spawn_depth=2).
package subagent

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

// Limits mirror delegate_tool.py.
const (
	MaxConcurrentChildren = 3
	MaxSpawnDepth         = 2
)

// RunFunc executes one child agent turn with a distinct session context.
type RunFunc func(ctx context.Context, sessionID, task string) (string, error)

// Manager spawns bounded child agents.
type Manager struct {
	run RunFunc
	sem chan struct{}
}

// NewManager builds a manager around run.
func NewManager(run RunFunc) *Manager {
	return &Manager{run: run, sem: make(chan struct{}, MaxConcurrentChildren)}
}

type childKey struct{}

// DepthOf reads the delegation depth from ctx.
func DepthOf(ctx context.Context) int {
	if v, ok := ctx.Value(childKey{}).(int); ok {
		return v
	}
	return 0
}

// WithDepth stamps ctx with depth.
func WithDepth(ctx context.Context, d int) context.Context {
	return context.WithValue(ctx, childKey{}, d)
}

// Delegate spawns one child (leaf or orchestrator) for task.
func (m *Manager) Delegate(ctx context.Context, parentSession, task string) (string, error) {
	if DepthOf(ctx) >= MaxSpawnDepth {
		return "", errors.New("subagent: max spawn depth exceeded")
	}
	select {
	case m.sem <- struct{}{}:
		defer func() { <-m.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	childCtx := WithDepth(ctx, DepthOf(ctx)+1)
	return m.run(childCtx, parentSession+"/child", task)
}

// DelegateMany fans out tasks concurrently, preserving order.
func (m *Manager) DelegateMany(ctx context.Context, parentSession string, tasks []string) []error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(MaxConcurrentChildren)
	out := make([]string, len(tasks))
	errs := make([]error, len(tasks))
	for i, t := range tasks {
		i, t := i, t
		g.Go(func() error {
			res, err := m.Delegate(gctx, parentSession, t)
			out[i] = res
			errs[i] = err
			return nil
		})
	}
	_ = g.Wait()
	return errs
}
