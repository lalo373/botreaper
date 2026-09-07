package ws

import (
	"testing"
	"time"

	"github.com/nousresearch/botreaper/pkg/types"
)

func TestRingSince(t *testing.T) {
	var r Ring
	for i := 0; i < 5; i++ {
		r.Push(types.Envelope{Type: "x"})
	}
	got, ok := r.Since(2, 10)
	if !ok || len(got) != 3 || got[0].Seq != 3 {
		t.Fatalf("since(2) = %d, ok=%v", len(got), ok)
	}
	if last := r.Last(); last != 5 {
		t.Fatalf("last = %d", last)
	}
}

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub(3)
	c := &Conn{send: make(chan types.Envelope, 8), closed: make(chan struct{})}
	h.Subscribe(c, "s1")
	e := h.Publish("s1", types.EvTokenDelta, types.KindEvent, []byte("hi"))
	if e.Seq != 1 || e.Epoch != 3 {
		t.Fatalf("envelope = %+v", e)
	}
	select {
	case got := <-c.send:
		if string(got.Data) != "hi" {
			t.Fatalf("data = %q", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("no fan-out")
	}
	got, ok := h.Replay("s1", 0, 10)
	if !ok || len(got) != 1 {
		t.Fatalf("replay = %d, ok=%v", len(got), ok)
	}
	if n := h.ConnCount(); n != 1 {
		t.Fatalf("conns = %d", n)
	}
	h.Unsubscribe(c)
	if n := h.ConnCount(); n != 0 {
		t.Fatalf("conns after unsubscribe = %d", n)
	}
}

func TestCoalescerBatchesDeltas(t *testing.T) {
	h := NewHub(1)
	c := &Conn{send: make(chan types.Envelope, 8), closed: make(chan struct{})}
	h.Subscribe(c, "s")
	co := NewCoalescer(h, "s")
	co.Add(types.EvTokenDelta, []byte("a"))
	co.Add(types.EvTokenDelta, []byte("b"))
	select {
	case <-c.send:
		t.Fatal("delta flushed before timer")
	case <-time.After(10 * time.Millisecond):
	}
	co.Flush()
	select {
	case got := <-c.send:
		if string(got.Data) != "ab" {
			t.Fatalf("coalesced = %q", got.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("no flush")
	}
}

func TestCoalescerBypass(t *testing.T) {
	h := NewHub(1)
	c := &Conn{send: make(chan types.Envelope, 8), closed: make(chan struct{})}
	h.Subscribe(c, "s")
	co := NewCoalescer(h, "s")
	if co.Add(types.EvToolStart, []byte("t")) {
		t.Fatal("tool frame must bypass coalescing")
	}
	select {
	case <-c.send:
	case <-time.After(time.Second):
		t.Fatal("bypass frame not published")
	}
}

func BenchmarkPublish(b *testing.B) {
	h := NewHub(1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Publish("s", types.EvTokenDelta, types.KindEvent, []byte("x"))
	}
}
