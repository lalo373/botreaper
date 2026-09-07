// Package agent implements the AIAgent event-driven state machine.
// Ports run_agent.py + agent/conversation_loop.py + turn_*.py phases.
package agent

import (
	"context"
	"sync"

	"github.com/nousresearch/botreaper/internal/provider"
	"github.com/nousresearch/botreaper/internal/state"
	"github.com/nousresearch/botreaper/internal/tools"
	"github.com/nousresearch/botreaper/internal/ws"
	"github.com/nousresearch/botreaper/pkg/types"
)

// Options mirrors AIAgent.__init__ knobs (trimmed to load-bearing fields).
type Options struct {
	BaseURL          string
	APIKey           string
	Provider         string
	Model            string
	MaxIterations    int
	EnabledToolsets  []string
	DisabledToolsets []string
	QuietMode        bool
	Platform         string
	SessionID        string
	CWD              string
	SystemPrompt     string
}

// AIAgent is the central runtime. The SystemPrompt snapshot is immutable for
// the life of a session (KV-cache sacred; only compression forks it).
type AIAgent struct {
	opts     Options
	prov     *provider.Client
	profile  provider.Profile
	registry *tools.Registry
	db       *state.DB
	usage    *state.UsageWriter
	hub      *ws.Hub
	coalesce *ws.Coalescer

	mu        sync.Mutex
	interrupt bool
}

// New wires an agent. Registry must already carry builtin tools; db may be
// nil for stateless use (tests/evals).
func New(opts Options, prov *provider.Client, prof provider.Profile, reg *tools.Registry, db *state.DB, hub *ws.Hub) *AIAgent {
	if opts.MaxIterations <= 0 {
		opts.MaxIterations = 500
	}
	if opts.SessionID == "" {
		opts.SessionID = "default"
	}
	a := &AIAgent{opts: opts, prov: prov, profile: prof, registry: reg, db: db, hub: hub}
	if db != nil {
		a.usage = state.NewUsageWriter(db)
	}
	if hub != nil {
		a.coalesce = ws.NewCoalescer(hub, opts.SessionID)
	}
	return a
}

// RequestInterrupt mirrors interrupt_control.py (instant Ctrl+C teardown).
func (a *AIAgent) RequestInterrupt() {
	a.mu.Lock()
	a.interrupt = true
	a.mu.Unlock()
}

func (a *AIAgent) interrupted() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.interrupt
}

// ClearInterrupt rearms the agent for the next turn.
func (a *AIAgent) ClearInterrupt() {
	a.mu.Lock()
	a.interrupt = false
	a.mu.Unlock()
}

// Close drains the usage writer.
func (a *AIAgent) Close() {
	if a.usage != nil {
		a.usage.Close()
	}
}

// SessionID returns the bound session.
func (a *AIAgent) SessionID() string { return a.opts.SessionID }

// emitToken streams one token delta (coalesced 33ms when hub present).
func (a *AIAgent) emitToken(text string) {
	if a.coalesce != nil {
		a.coalesce.Add(types.EvTokenDelta, []byte(text))
		return
	}
	if a.hub != nil {
		a.hub.Publish(a.opts.SessionID, types.EvTokenDelta, types.KindEvent, []byte(text))
	}
}

// emitTool broadcasts tool lifecycle events (spinner/progress contract).
func (a *AIAgent) emitTool(typ, payload string) {
	if a.hub != nil {
		a.hub.Publish(a.opts.SessionID, typ, types.KindEvent, []byte(payload))
	}
}

// Chat is the single-turn facade (run_agent.chat): appends user msg,
// runs the loop, returns final text.
func (a *AIAgent) Chat(ctx context.Context, msg string) (string, error) {
	res, err := a.RunConversation(ctx, []types.Message{
		{SessionID: a.opts.SessionID, Role: types.RoleUser, Content: msg, Active: true},
	})
	if err != nil {
		return "", err
	}
	return res.FinalResponse, nil
}

var _ = context.Background
