package tools

import (
	"context"
	"encoding/json"
	"time"
)

// CallResult is the normalized outcome of one tool invocation.
type CallResult struct {
	Name     string `json:"name"`
	CallID   string `json:"call_id"`
	Output   string `json:"output"`
	Spilled  bool   `json:"spilled,omitempty"`
	Duration string `json:"duration_ms,omitempty"`
}

// errorCap mirrors the 2048ch tool error cap.
const errorCap = 2048

// Dispatch runs name with args under ctx, applying the error cap and
// spilling oversized outputs (mirrors tool_result_storage.py: pass a
// pointer, not the heap).
func (r *Registry) Dispatch(ctx context.Context, name, callID string, args map[string]any, spill func(string) string) CallResult {
	start := time.Now()
	h, ok := r.Lookup(name)
	res := CallResult{Name: name, CallID: callID}
	if !ok {
		res.Output = "unknown tool: " + name
		return res
	}
	tctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := h(tctx, args)
	if err != nil {
		msg := err.Error()
		if len(msg) > errorCap {
			msg = msg[:errorCap]
		}
		res.Output = "error: " + msg
		return res
	}
	if len(out) > 32*1024 && spill != nil {
		res.Output = spill(out)
		res.Spilled = true
	} else {
		res.Output = out
	}
	res.Duration = time.Since(start).String()
	return res
}

// JSONArgs decodes raw JSON object args with string-key normalization.
func JSONArgs(raw string) map[string]any {
	m := map[string]any{}
	if raw == "" {
		return m
	}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

// StrArg reads an optional string arg.
func StrArg(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}
