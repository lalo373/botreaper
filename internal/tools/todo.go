package tools

import (
	"context"
	"encoding/json"
	"sync"
)

// TodoItem mirrors tools/todo_tool.py rows.
type TodoItem struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

// TodoStore is the in-session todo list (agent-state tool, bypasses heavy
// registry paths like Python's INLINE_TOOL_EXECUTORS).
type TodoStore struct {
	mu    sync.Mutex
	items []TodoItem
}

// NewTodoStore creates an empty list.
func NewTodoStore() *TodoStore { return &TodoStore{} }

// RegisterTodoTools wires the todo tool against store.
func RegisterTodoTools(r *Registry, store *TodoStore) {
	r.Register("todo", "todo", "Manage the session todo list", func(ctx context.Context, args map[string]any) (string, error) {
		op := StrArg(args, "op", "list")
		store.mu.Lock()
		defer store.mu.Unlock()
		switch op {
		case "add":
			text := StrArg(args, "text", "")
			id := StrArg(args, "id", text)
			store.items = append(store.items, TodoItem{ID: id, Text: text, Status: "pending"})
		case "done":
			id := StrArg(args, "id", "")
			for i := range store.items {
				if store.items[i].ID == id {
					store.items[i].Status = "done"
				}
			}
		case "clear":
			store.items = nil
		}
		raw, _ := json.Marshal(store.items)
		return string(raw), nil
	}, nil)
}
