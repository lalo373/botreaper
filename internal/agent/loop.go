package agent

import (
	"context"
	"strings"

	"github.com/nousresearch/botreaper/internal/provider"
	"github.com/nousresearch/botreaper/pkg/types"
)

// RunConversation is the turn loop port of conversation_loop.py +
// turn_facade.py: preflight -> assemble -> api call -> intake -> tool round
// until text finish, budget exhaustion, or interrupt.
func (a *AIAgent) RunConversation(ctx context.Context, seed []types.Message) (types.TurnResult, error) {
	budget := NewBudget(a.opts.MaxIterations)
	history := append([]types.Message{}, seed...)
	if a.db != nil && len(seed) > 0 {
		_ = a.db.AppendMessagesBatch(ctx, seed)
	}
	var toolCalls int
	for budget.Admit() {
		if a.interrupted() {
			break
		}
		if err := ctx.Err(); err != nil {
			return types.TurnResult{Messages: history, APICalls: budget.Used(), ToolCalls: toolCalls}, err
		}
		if err := a.persistCheck(); err != nil {
			return types.TurnResult{Messages: history}, err
		}
		turn, err := a.apiCall(ctx, history)
		if err != nil {
			if isEmptyResponse(err) {
				continue
			}
			return types.TurnResult{Messages: history, APICalls: budget.Used(), ToolCalls: toolCalls}, err
		}
		budget.Spend()
		if a.db != nil {
			_ = a.db.BumpAPICalls(ctx, a.opts.SessionID, 1)
		}
		if len(turn.ToolCalls) == 0 {
			finish := turn.Content
			amsg := types.Message{SessionID: a.opts.SessionID, Role: types.RoleAssistant, Content: finish, Active: true}
			history = append(history, amsg)
			if a.db != nil {
				_, _ = a.db.AppendMessage(ctx, amsg)
			}
			if a.coalesce != nil {
				a.coalesce.Flush()
			}
			if a.usage != nil && (turn.PromptTok > 0 || turn.ComplTok > 0) {
				a.usage.Queue(usageDelta(a, turn, "main"))
			}
			return types.TurnResult{FinalResponse: finish, Messages: history, APICalls: budget.Used(), ToolCalls: toolCalls}, nil
		}
		toolCalls += len(turn.ToolCalls)
		tmsgs := a.runToolRound(ctx, turn.ToolCalls)
		history = append(history, tmsgs...)
		if a.db != nil {
			_ = a.db.AppendMessagesBatch(ctx, tmsgs)
		}
	}
	// Budget/interrupt exit: summarize rather than error (turn_summary.py).
	return types.TurnResult{FinalResponse: summarize(history), Messages: history, APICalls: budget.Used(), ToolCalls: toolCalls}, nil
}

func (a *AIAgent) apiCall(ctx context.Context, history []types.Message) (types.AssistantTurn, error) {
	wire := make([]provider.WireMessage, 0, len(history)+1)
	if sys := a.systemSnapshot(); sys != "" {
		wire = append(wire, provider.WireMessage{Role: "system", Content: sys})
	}
	for _, m := range history {
		role := string(m.Role)
		content := m.Content
		if role == "tool" {
			wire = append(wire, provider.WireMessage{Role: "tool", Content: content, ToolCallID: m.ToolCallID, Name: m.ToolName})
			continue
		}
		wire = append(wire, provider.WireMessage{Role: role, Content: content})
	}
	defs := a.registry.Definitions(toSet(a.opts.EnabledToolsets), toSet(a.opts.DisabledToolsets), a.opts.QuietMode)
	wtools := make([]provider.WireTool, 0, len(defs))
	for _, name := range defs {
		wtools = append(wtools, provider.WireTool{Type: "function", Function: struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		}{Name: name, Description: string(a.registry.ToolsetFor(name)), Parameters: map[string]any{"type": "object"}}})
	}
	req := provider.ChatRequest{Model: a.opts.Model, Messages: wire, Tools: wtools}
	if a.prov == nil {
		return types.AssistantTurn{Content: echoFallback(history)}, nil
	}
	return a.prov.Stream(ctx, req, a.emitToken)
}

func toSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

func summarize(history []types.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == types.RoleAssistant && history[i].Content != "" {
			return history[i].Content
		}
	}
	return ""
}

func echoFallback(history []types.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == types.RoleUser {
			return "ack: " + history[i].Content
		}
	}
	return "ack"
}

func isEmptyResponse(err error) bool {
	return err != nil && strings.Contains(err.Error(), "empty response")
}
