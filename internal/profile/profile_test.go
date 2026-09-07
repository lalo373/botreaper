package profile

import (
	"testing"
)

func TestHomeResolution(t *testing.T) {
	t.Setenv("BOTREAPER_HOME", "/tmp/botreaper-test-home")
	if got := GetBotReaperHome(""); got != "/tmp/botreaper-test-home" {
		t.Fatalf("home = %q", got)
	}
	if got := GetBotReaperHome("/override"); got != "/override" {
		t.Fatalf("override = %q", got)
	}
	if NamedHome("") == "" || ProfilesRoot() == "" || DisplayHome("/x") == "" {
		t.Fatal("empty profile paths")
	}
}

func TestLegacyHomeFallback(t *testing.T) {
	t.Setenv("BOTREAPER_HOME", "")
	t.Setenv("HERMES_HOME", "/tmp/legacy-home")
	if got := GetBotReaperHome(""); got != "/tmp/legacy-home" {
		t.Fatalf("legacy fallback = %q", got)
	}
}
