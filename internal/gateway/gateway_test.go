package gateway

import (
	"context"
	"testing"
	"time"
)

func TestSlashDispatch(t *testing.T) {
	tbl := NewSlashTable()
	out, handled, err := tbl.Execute(context.Background(), "/model foo", Inbound{Platform: "cli"})
	if err != nil || !handled || out == "" {
		t.Fatalf("slash = %q %v %v", out, handled, err)
	}
	if _, handled, _ := tbl.Execute(context.Background(), "plain text", Inbound{}); handled {
		t.Fatal("plain text treated as command")
	}
	out, handled, _ = tbl.Execute(context.Background(), "/nope", Inbound{})
	if !handled || out == "" {
		t.Fatal("unknown command swallowed")
	}
}

func TestSlashHandles(t *testing.T) {
	tbl := NewSlashTable()
	if !tbl.Handles("/status") || !tbl.Handles("/nope") {
		t.Fatal("known and unknown slash commands must bypass busy turns")
	}
	if tbl.Handles("plain text") || tbl.Handles("") {
		t.Fatal("plain text must not bypass")
	}
}

func TestBusyGuard(t *testing.T) {
	b := NewBusyGuard()
	if !b.TryAcquire("s", "run", nil) {
		t.Fatal("first acquire denied")
	}
	if b.TryAcquire("s", "run", nil) {
		t.Fatal("double acquire allowed")
	}
	if !b.TryAcquire("s", "/status", func(s string) bool { return true }) {
		t.Fatal("plain command must bypass")
	}
	b.Release("s")
	if !b.TryAcquire("s", "run", nil) {
		t.Fatal("reacquire after release denied")
	}
}

func TestSessionTableOrphans(t *testing.T) {
	tbl := NewSessionTable()
	tbl.Bind("a", Session{Key: "k1", Platform: "telegram"})
	olds := Session{Key: "k2", Platform: "telegram", Updated: time.Now().Add(-time.Hour)}
	tbl.mu.Lock()
	tbl.rows["b"] = olds
	tbl.mu.Unlock()
	reaped := tbl.AdoptOrphaned(time.Minute)
	if len(reaped) != 1 || reaped[0].Key != "k2" {
		t.Fatalf("reaped = %+v", reaped)
	}
	if _, ok := tbl.Lookup("a"); !ok {
		t.Fatal("live session reaped")
	}
}

func TestAllowlist(t *testing.T) {
	if !UserAllowed(nil, "anyone") || !UserAllowed([]string{}, "anyone") {
		t.Fatal("empty list must allow all")
	}
	if !UserAllowed([]string{"42", "7"}, "7") {
		t.Fatal("listed user denied")
	}
	if UserAllowed([]string{"42"}, "7") {
		t.Fatal("unlisted user allowed")
	}
	if got := ParseAllowlist(" 42, 7 ,,"); len(got) != 2 || got[0] != "42" || got[1] != "7" {
		t.Fatalf("parsed = %q", got)
	}
}

func TestChannelNames(t *testing.T) {
	names := ChannelNames()
	if len(names) < 10 {
		t.Fatalf("channels = %v", names)
	}
}
