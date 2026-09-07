package tools

import (
	"context"
	"strings"
)

// StreamChunk mirrors sandbox.Chunk without importing sandbox (keeps the
// registry dependency-free; adapters live in internal/agent).
type StreamChunk struct {
	Stream string
	Data   []byte
}

// TerminalRunner executes cmd and streams chunks; returns exit code.
type TerminalRunner func(ctx context.Context, cmd string, out chan<- StreamChunk) (int, error)

// RegisterTerminal wires the terminal tool over an injected runner.
// Mirrors terminal_tool*.py + environments selection.
func RegisterTerminal(r *Registry, run TerminalRunner, scan func(string) error) {
	r.Register("terminal", "terminal", "Run a shell command with streamed output", func(ctx context.Context, args map[string]any) (string, error) {
		cmd := StrArg(args, "command", "")
		if cmd == "" {
			return "", errString("terminal: empty command")
		}
		if scan != nil {
			if err := scan(cmd); err != nil {
				return "", err
			}
		}
		ch := make(chan StreamChunk, 64)
		done := make(chan struct{})
		var sb strings.Builder
		go func() {
			defer close(done)
			for c := range ch {
				sb.Write(c.Data)
				if sb.Len() > 256*1024 {
					break
				}
			}
		}()
		code, err := run(ctx, cmd, ch)
		close(ch)
		<-done
		_ = code
		if err != nil {
			return sb.String(), err
		}
		return sb.String(), nil
	}, nil)
}

// RegisterMemory wires the memory tool over injected load/save funcs.
func RegisterMemory(r *Registry, load func(ctx context.Context) string, save func(ctx context.Context, text string) string) {
	r.Register("memory", "memory", "Recall or persist long-term memory", func(ctx context.Context, args map[string]any) (string, error) {
		if t := StrArg(args, "save", ""); t != "" {
			return save(ctx, t), nil
		}
		return load(ctx), nil
	}, nil)
}

// SearchFunc runs full-text search; returns JSON string.
type SearchFunc func(ctx context.Context, query string) (string, error)

// RegisterSessionSearch wires session_search over an injected func.
func RegisterSessionSearch(r *Registry, fn SearchFunc) {
	r.Register("session_search", "session_search", "Search session transcripts", func(ctx context.Context, args map[string]any) (string, error) {
		return fn(ctx, StrArg(args, "query", ""))
	}, nil)
}

// DelegateFunc spawns a child agent; returns its final text.
type DelegateFunc func(ctx context.Context, task string) (string, error)

// RegisterDelegate wires delegate_task over an injected func.
func RegisterDelegate(r *Registry, fn DelegateFunc) {
	r.Register("delegate_task", "delegation", "Delegate a subtask to a child agent", func(ctx context.Context, args map[string]any) (string, error) {
		return fn(ctx, StrArg(args, "task", ""))
	}, nil)
}

// RegisterCronTools wires cron_create/list/delete over injected funcs.
func RegisterCronTools(r *Registry, create func(ctx context.Context, schedule, prompt string) string, list func(ctx context.Context) string, del func(ctx context.Context, id string) string) {
	r.Register("cron_create", "cronjob", "Create a scheduled job", func(ctx context.Context, args map[string]any) (string, error) {
		return create(ctx, StrArg(args, "schedule", ""), StrArg(args, "prompt", "")), nil
	}, nil)
	r.Register("cron_list", "cronjob", "List scheduled jobs", func(ctx context.Context, args map[string]any) (string, error) {
		return list(ctx), nil
	}, nil)
	r.Register("cron_delete", "cronjob", "Delete a scheduled job", func(ctx context.Context, args map[string]any) (string, error) {
		return del(ctx, StrArg(args, "id", "")), nil
	}, nil)
}

// RegisterSkills wires the skills tool over an injected describer.
func RegisterSkills(r *Registry, describe func(ctx context.Context) string) {
	r.Register("skills", "skills", "List and describe available skills", func(ctx context.Context, args map[string]any) (string, error) {
		return describe(ctx), nil
	}, nil)
}
