package subagent

import (
	"context"
	"errors"
	"testing"
)

func TestDelegateDepthLimit(t *testing.T) {
	m := NewManager(func(ctx context.Context, sess, task string) (string, error) { return "done:" + task, nil })
	out, err := m.Delegate(context.Background(), "p", "t")
	if err != nil || out != "done:t" {
		t.Fatalf("delegate = %q %v", out, err)
	}
	deep := WithDepth(context.Background(), MaxSpawnDepth)
	if _, err := m.Delegate(deep, "p", "t"); err == nil {
		t.Fatal("depth limit not enforced")
	}
}

func TestDelegateMany(t *testing.T) {
	m := NewManager(func(ctx context.Context, sess, task string) (string, error) {
		if task == "bad" {
			return "", errors.New("boom")
		}
		return "ok", nil
	})
	errs := m.DelegateMany(context.Background(), "p", []string{"a", "bad", "c"})
	if len(errs) != 3 || errs[0] != nil || errs[1] == nil {
		t.Fatalf("errs = %v", errs)
	}
}
