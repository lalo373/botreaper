package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestLocalExecStreams(t *testing.T) {
	b := &LocalBackend{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch := make(chan Chunk, 16)
	code, err := b.Exec(ctx, "echo hello", ch)
	close(ch)
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	got := ""
	for c := range ch {
		got += string(c.Data)
	}
	if got == "" || len(got) > 1<<20 {
		t.Fatalf("output = %q", got)
	}
}

func TestScanner(t *testing.T) {
	if ScanCommand("ls -la").Verdict != Allow {
		t.Fatal("ls denied")
	}
	if ScanCommand("").Verdict != Deny {
		t.Fatal("empty allowed")
	}
	if ScanCommand("rm -rf /").Verdict != Deny {
		t.Fatal("rm -rf / not denied")
	}
	if ScanCommand("sudo reboot").Verdict != NeedsApproval {
		t.Fatal("sudo not gated")
	}
	if err := CheckExec("rm -rf /"); err == nil {
		t.Fatal("deny not enforced")
	}
}

func TestRegistrySlots(t *testing.T) {
	r := NewRegistry(nil)
	for _, n := range []string{"local", "docker", "ssh", "singularity", "modal", "daytona"} {
		if r.Get(n) == nil || r.Get(n).Name() != n {
			t.Fatalf("slot %s broken", n)
		}
	}
	if r.Get("") == nil {
		t.Fatal("default slot broken")
	}
}

func BenchmarkScan(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ScanCommand("ls -la /tmp && echo done")
	}
}
