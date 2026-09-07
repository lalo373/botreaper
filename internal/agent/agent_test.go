package agent

import (
	"context"
	"testing"

	"github.com/nousresearch/botreaper/internal/provider"
	"github.com/nousresearch/botreaper/internal/tools"
	"github.com/nousresearch/botreaper/pkg/types"
)

func TestBudgetGrace(t *testing.T) {
	b := NewBudget(2)
	if !b.Admit() {
		t.Fatal("first admit denied")
	}
	b.Spend()
	b.Spend()
	if !b.Admit() {
		t.Fatal("grace call denied")
	}
	if b.Admit() {
		t.Fatal("over-grace admitted")
	}
}

func TestPruneMiddle(t *testing.T) {
	var h []types.Message
	for i := 0; i < 20; i++ {
		h = append(h, types.Message{Role: types.RoleUser, Content: "m"})
	}
	pruned := PruneMiddle(h)
	if len(pruned) >= len(h) {
		t.Fatalf("not pruned: %d", len(pruned))
	}
	if pruned[len(pruned)-1].Content != "m" {
		t.Fatal("tail not preserved")
	}
}

func testAgent() *AIAgent {
	reg := tools.NewRegistry()
	tools.RegisterTodoTools(reg, tools.NewTodoStore())
	return New(Options{Model: "test", SessionID: "t", SystemPrompt: "sys"}, nil, provider.Profile{Name: "test"}, reg, nil, nil)
}

func TestLoopTextFinish(t *testing.T) {
	a := testAgent()
	res, err := a.RunConversation(context.Background(), []types.Message{{Role: types.RoleUser, Content: "hi", Active: true}})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalResponse == "" || res.APICalls != 1 {
		t.Fatalf("result = %+v", res)
	}
}

func TestLoopToolRound(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register("ping", "test", "ping", func(ctx context.Context, args map[string]any) (string, error) { return "pong", nil }, nil)
	a := New(Options{Model: "t", SessionID: "t2"}, nil, provider.Profile{}, reg, nil, nil)
	msgs := a.runToolRound(context.Background(), []types.ToolCall{{ID: "c1", Name: "ping"}})
	if len(msgs) != 1 || msgs[0].Content != "pong" {
		t.Fatalf("tool round = %+v", msgs)
	}
	if !types.ValidateAlternation([]types.Message{{Role: types.RoleAssistant}, {Role: types.RoleTool}}) {
		t.Fatal("alternation helper broken")
	}
}

func TestInterrupt(t *testing.T) {
	a := testAgent()
	a.RequestInterrupt()
	res, err := a.RunConversation(context.Background(), []types.Message{{Role: types.RoleUser, Content: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.APICalls != 0 {
		t.Fatalf("interrupted loop ran %d calls", res.APICalls)
	}
	a.ClearInterrupt()
}

func BenchmarkBuildPrompt(b *testing.B) {
	hist := []string{"user: hello", "assistant: hi"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildPrompt("system snapshot that stays immutable", hist, "")
	}
}
