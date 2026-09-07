// Package tools implements the builtin tool registry and dispatch.
// Ports tools/registry.py + model_tools.py + toolsets.py. This package stays
// dependency-free apart from types so every consumer can import it.
package tools

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// CheckFunc gates tool visibility (service configured?). Results are
// TTL-cached process-wide, mirroring registry.py.
type CheckFunc func() bool

// Handler executes a tool; it returns a JSON string (never raw structs),
// mirroring the Python contract. Errors are capped by dispatch.
type Handler func(ctx context.Context, args map[string]any) (string, error)

type entry struct {
	name     string
	toolset  string
	desc     string
	handler  Handler
	check    CheckFunc
	requires []string
	lastOK   bool
	lastAt   time.Time
}

const checkTTL = 60 * time.Second

// Registry is the global tool table.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]*entry
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry { return &Registry{entries: map[string]*entry{}} }

// Register adds a tool (called at init, like Python's import-time register).
func (r *Registry) Register(name, toolset, desc string, h Handler, check CheckFunc, requires ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[name] = &entry{name: name, toolset: toolset, desc: desc, handler: h, check: check, requires: requires}
}

func (e *entry) available() bool {
	if e.check == nil {
		return true
	}
	if time.Since(e.lastAt) < checkTTL {
		return e.lastOK
	}
	e.lastOK = e.check()
	e.lastAt = time.Now()
	return e.lastOK
}

// Names returns sorted available tool names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, e := range r.entries {
		if e.available() {
			out = append(out, e.name)
		}
	}
	sort.Strings(out)
	return out
}

// ToolsetFor returns the owning toolset.
func (r *Registry) ToolsetFor(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[name]; ok {
		return e.toolset
	}
	return ""
}

// Lookup returns the handler when available.
func (r *Registry) Lookup(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[name]
	if !ok || !e.available() {
		return nil, false
	}
	return e.handler, true
}

// Definitions filters by enabled/disabled toolsets + check_fn + quiet mode,
// mirroring model_tools.get_tool_definitions.
func (r *Registry) Definitions(enabled, disabled map[string]bool, quiet bool) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []string
	for _, e := range r.entries {
		if len(enabled) > 0 && !enabled[e.toolset] && !enabled[e.name] {
			continue
		}
		if disabled[e.toolset] || disabled[e.name] {
			continue
		}
		if quiet && noisyTools[e.name] {
			continue
		}
		if !e.available() {
			continue
		}
		out = append(out, e.name)
	}
	sort.Strings(out)
	return out
}

var noisyTools = map[string]bool{"tts": true, "image_gen": true, "video_gen": true}

// ErrUnknownTool is returned for missing tools.
var ErrUnknownTool = errors.New("tools: unknown tool")
