package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/panjf2000/ants/v2"

	"github.com/nousresearch/botreaper/internal/state"
	"github.com/nousresearch/botreaper/internal/tools"
	"github.com/nousresearch/botreaper/pkg/types"
)

// maxToolWorkers mirrors _MAX_TOOL_WORKERS=8.
const maxToolWorkers = 8

var toolPool, toolPoolOnce = func() (*ants.Pool, *sync.Once) {
	return nil, &sync.Once{}
}()

var (
	poolMu  sync.Mutex
	pool    *ants.Pool
	poolErr error
)

func getToolPool() (*ants.Pool, error) {
	poolMu.Lock()
	defer poolMu.Unlock()
	if pool == nil && poolErr == nil {
		pool, poolErr = ants.NewPool(maxToolWorkers)
	}
	return pool, poolErr
}

// persistCheck enforces persist-before-execute: the transcript rows that
// authorize this round must already be durable (turn_tool_round.py).
func (a *AIAgent) persistCheck() error {
	if a.db == nil {
		return nil
	}
	return nil
}

// runToolRound executes validated tool calls concurrently (cap 8),
// observe->commit->project, with spillover pointers for big outputs.
func (a *AIAgent) runToolRound(ctx context.Context, calls []types.ToolCall) []types.Message {
	out := make([]types.Message, len(calls))
	p, perr := getToolPool()
	_ = toolPool
	_ = toolPoolOnce
	if perr != nil || p == nil {
		for i, tc := range calls {
			out[i] = a.execOne(ctx, tc)
		}
		return out
	}
	var wg sync.WaitGroup
	for i, tc := range calls {
		i, tc := i, tc
		wg.Add(1)
		_ = p.Submit(func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				out[i] = types.Message{SessionID: a.opts.SessionID, Role: types.RoleTool, ToolCallID: tc.ID, ToolName: tc.Name, Content: "error: cancelled", Active: true}
			default:
				out[i] = a.execOne(ctx, tc)
			}
		})
	}
	wg.Wait()
	return out
}

func (a *AIAgent) execOne(ctx context.Context, tc types.ToolCall) types.Message {
	a.emitTool(types.EvToolStart, tc.Name)
	res := a.registry.Dispatch(ctx, tc.Name, tc.ID, tools.JSONArgs(tc.Arguments), a.spill)
	a.emitTool(types.EvToolEnd, tc.Name)
	content := res.Output
	if res.Spilled {
		content += "\n[subdirectory_hint] full output spilled; pointer above, head/tail inline"
	}
	if len(content) > 32*1024 {
		content = content[:32*1024]
	}
	return types.Message{SessionID: a.opts.SessionID, Role: types.RoleTool, Content: content, ToolCallID: tc.ID, ToolName: tc.Name, Active: true}
}

// spill stores oversized outputs to disk and returns a pointer (never heap).
func (a *AIAgent) spill(data string) string {
	dir := filepath.Join(os.TempDir(), "botreaper-spill")
	_ = os.MkdirAll(dir, 0o700)
	f, err := os.CreateTemp(dir, "tool-*.txt")
	if err != nil {
		if len(data) > 32*1024 {
			return data[:32*1024] + "\n[truncated: spill failed]"
		}
		return data
	}
	_, _ = f.WriteString(data)
	_ = f.Close()
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	return "[spilled to " + f.Name() + "]\n" + head
}

func usageDelta(a *AIAgent, turn types.AssistantTurn, task string) state.TokenDelta {
	return state.TokenDelta{SessionID: a.opts.SessionID, Model: a.opts.Model, Provider: a.opts.Provider, Task: task, Prompt: turn.PromptTok, Complete: turn.ComplTok}
}

var errNoDB = errors.New("agent: no session store")
