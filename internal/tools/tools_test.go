package tools

import (
	"context"
	"testing"
)

func TestRegistryDispatch(t *testing.T) {
	r := NewRegistry()
	r.Register("echo", "test", "echo", func(ctx context.Context, args map[string]any) (string, error) {
		return StrArg(args, "v", ""), nil
	}, nil)
	res := r.Dispatch(context.Background(), "echo", "c1", map[string]any{"v": "hi"}, nil)
	if res.Output != "hi" || res.Name != "echo" || res.CallID != "c1" {
		t.Fatalf("result = %+v", res)
	}
	miss := r.Dispatch(context.Background(), "nope", "c2", nil, nil)
	if miss.Output == "" {
		t.Fatal("missing tool must explain")
	}
}

func TestDefinitionsFilter(t *testing.T) {
	r := NewRegistry()
	r.Register("a", "ta", "a", func(ctx context.Context, args map[string]any) (string, error) { return "a", nil }, nil)
	r.Register("b", "tb", "b", func(ctx context.Context, args map[string]any) (string, error) { return "b", nil }, nil)
	defs := r.Definitions(map[string]bool{"ta": true}, nil, false)
	if len(defs) != 1 || defs[0] != "a" {
		t.Fatalf("defs = %v", defs)
	}
	if got := ResolveToolset("file"); len(got) == 0 {
		t.Fatal("file toolset empty")
	}
	if got := CoreTools(); len(got) == 0 {
		t.Fatal("core tools empty")
	}
}

func TestFileToolsRoundTrip(t *testing.T) {
	r := NewRegistry()
	dir := t.TempDir()
	RegisterFileTools(r, dir)
	h, _ := r.Lookup("write_file")
	if _, err := h(context.Background(), map[string]any{"path": "a.txt", "content": "hello"}); err != nil {
		t.Fatal(err)
	}
	rh, _ := r.Lookup("read_file")
	out, err := rh(context.Background(), map[string]any{"path": "a.txt"})
	if err != nil || out != "hello" {
		t.Fatalf("read = %q, err=%v", out, err)
	}
	ph, _ := r.Lookup("patch")
	if _, err := ph(context.Background(), map[string]any{"path": "a.txt", "search": "hello", "replace": "bye"}); err != nil {
		t.Fatal(err)
	}
	out, _ = rh(context.Background(), map[string]any{"path": "a.txt"})
	if out != "bye" {
		t.Fatalf("patched = %q", out)
	}
}

func TestTodoStore(t *testing.T) {
	r := NewRegistry()
	RegisterTodoTools(r, NewTodoStore())
	h, _ := r.Lookup("todo")
	out, err := h(context.Background(), map[string]any{"op": "add", "id": "1", "text": "x"})
	if err != nil || out == "" {
		t.Fatalf("todo add = %q, err=%v", out, err)
	}
}

func TestTerminalStreams(t *testing.T) {
	r := NewRegistry()
	RegisterTerminal(r, func(ctx context.Context, cmd string, out chan<- StreamChunk) (int, error) {
		out <- StreamChunk{Stream: "stdout", Data: []byte("part")}
		return 0, nil
	}, nil)
	h, _ := r.Lookup("terminal")
	out, err := h(context.Background(), map[string]any{"command": "echo hi"})
	if err != nil || out != "part" {
		t.Fatalf("terminal = %q, err=%v", out, err)
	}
}

func BenchmarkDispatch(b *testing.B) {
	r := NewRegistry()
	r.Register("noop", "test", "noop", func(ctx context.Context, args map[string]any) (string, error) { return "ok", nil }, nil)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Dispatch(ctx, "noop", "c", nil, nil)
	}
}
