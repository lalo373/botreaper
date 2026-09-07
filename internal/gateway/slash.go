package gateway

import (
	"context"
	"sort"
	"strings"
)

// SlashHandler serves one slash command (cache-aware: mutations default to
// deferred invalidation unless --now, mirroring cli.py/_command_handler_table).
type SlashHandler func(ctx context.Context, args string, in Inbound) (string, error)

// SlashTable is the data-driven dispatch (never an if/elif ladder).
type SlashTable struct {
	m map[string]SlashHandler
}

// NewSlashTable seeds core commands.
func NewSlashTable() *SlashTable {
	t := &SlashTable{m: map[string]SlashHandler{}}
	t.Register("model", func(ctx context.Context, args string, in Inbound) (string, error) {
		if strings.TrimSpace(args) == "" {
			return "usage: /model <name> [--now]", nil
		}
		return "model -> " + args + " (deferred to next session unless --now)", nil
	})
	t.Register("reset", func(ctx context.Context, args string, in Inbound) (string, error) {
		return "session reset", nil
	})
	t.Register("skills", func(ctx context.Context, args string, in Inbound) (string, error) {
		return "skills: " + args, nil
	})
	t.Register("status", func(ctx context.Context, args string, in Inbound) (string, error) {
		return "ok", nil
	})
	t.Register("sessions", func(ctx context.Context, args string, in Inbound) (string, error) {
		return "sessions", nil
	})
	return t
}

// Register adds a command (name without slash, lowercase).
func (t *SlashTable) Register(name string, h SlashHandler) {
	t.m[strings.ToLower(name)] = h
}

// Execute dispatches "/cmd args" or returns ("", false) for plain text.
func (t *SlashTable) Execute(ctx context.Context, text string, in Inbound) (string, bool, error) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "/") {
		return "", false, nil
	}
	parts := strings.SplitN(strings.TrimPrefix(trimmed, "/"), " ", 2)
	name := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	h, ok := t.m[name]
	if !ok {
		return "unknown command: /" + name, true, nil
	}
	out, err := h(ctx, args, in)
	return out, true, err
}

// Handles reports whether text is a slash invocation (known or not).
// The busy guard uses it to let plain commands bypass a running turn:
// Execute answers every "/..." locally, so none of them need the turn lock.
func (t *SlashTable) Handles(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/")
}

// Names lists commands.
func (t *SlashTable) Names() []string {
	out := make([]string, 0, len(t.m))
	for k := range t.m {
		out = append(out, "/"+k)
	}
	sort.Strings(out)
	return out
}
