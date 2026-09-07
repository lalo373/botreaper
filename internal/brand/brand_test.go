package brand

import (
	"bytes"
	"strings"
	"testing"
)

func TestIdentity(t *testing.T) {
	if Name != "BotReaper" {
		t.Fatalf("Name = %q", Name)
	}
	lines := strings.Split(strings.TrimRight(Art, "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("art lines = %d, want 10", len(lines))
	}
	if !strings.Contains(Art, "██▓███") {
		t.Fatal("art body mangled")
	}
	var buf bytes.Buffer
	Print(&buf)
	if buf.String() != Art+"\n" {
		t.Fatal("Print output mismatch")
	}
}
