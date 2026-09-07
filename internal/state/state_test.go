package state

import (
	"context"
	"testing"

	"github.com/nousresearch/botreaper/pkg/types"
)

func openTest(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSessionMessageRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	if err := db.CreateSession(ctx, types.Session{ID: "s1", Source: "cli", SessionKey: "s1", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSession(ctx, "s1")
	if err != nil || got.ID != "s1" {
		t.Fatalf("get = %+v, err=%v", got, err)
	}
	if _, err := db.AppendMessage(ctx, types.Message{SessionID: "s1", Role: types.RoleUser, Content: "hello", Active: true}); err != nil {
		t.Fatal(err)
	}
	msgs, err := db.GetMessages(ctx, "s1")
	if err != nil || len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Fatalf("msgs = %+v, err=%v", msgs, err)
	}
}

func TestSearchFTSAndFallback(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	_ = db.CreateSession(ctx, types.Session{ID: "s1", SessionKey: "s1"})
	_, _ = db.AppendMessage(ctx, types.Message{SessionID: "s1", Role: types.RoleUser, Content: "the quick brown fox", Active: true})
	hits, err := db.SearchMessages(ctx, "quick brown", "", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("hits = %+v, err=%v", hits, err)
	}
	// Hostile input must never error.
	if _, err := db.SearchMessages(ctx, `"(unclosed`, "", 10); err != nil {
		t.Fatalf("hostile query errored: %v", err)
	}
}

func TestUsageWriterCoalesces(t *testing.T) {
	ctx := context.Background()
	db := openTest(t)
	u := NewUsageWriter(db)
	defer u.Close()
	u.Queue(TokenDelta{SessionID: "s1", Model: "m", Task: "main", Prompt: 10, Complete: 5})
	u.Queue(TokenDelta{SessionID: "s1", Model: "m", Task: "main", Prompt: 3, Complete: 2})
	if err := u.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	p, c, err := db.UsageTotals(ctx, "s1")
	if err != nil || p != 13 || c != 7 {
		t.Fatalf("totals = %d/%d, err=%v", p, c, err)
	}
}

func BenchmarkAppendMessage(b *testing.B) {
	ctx := context.Background()
	db, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	_ = db.CreateSession(ctx, types.Session{ID: "s", SessionKey: "s"})
	m := types.Message{SessionID: "s", Role: types.RoleUser, Content: "bench", Active: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.AppendMessage(ctx, m); err != nil {
			b.Fatal(err)
		}
	}
}
