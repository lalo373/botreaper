package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretRoundTrip(t *testing.T) {
	home := t.TempDir()
	// Tokens routinely contain colons — dotenv `=` splitting must survive them.
	if err := SetSecret(home, "TELEGRAM_BOT_TOKEN", "123456:ABC-def_123"); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecret(home, "TELEGRAM_BOT_TOKEN"); got != "123456:ABC-def_123" {
		t.Fatalf("round trip = %q", got)
	}
	// Upsert preserves neighbors.
	if err := SetSecret(home, "OTHER", "1"); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecret(home, "TELEGRAM_BOT_TOKEN"); got != "123456:ABC-def_123" {
		t.Fatalf("neighbor clobbered: %q", got)
	}
	// Clear drops only the key.
	if err := ClearSecret(home, "OTHER"); err != nil {
		t.Fatal(err)
	}
	if got := LoadSecret(home, "OTHER"); got != "" {
		t.Fatalf("clear missed: %q", got)
	}
	if got := LoadSecret(home, "TELEGRAM_BOT_TOKEN"); got == "" {
		t.Fatal("clear removed neighbor")
	}
	if fi, err := os.Stat(filepath.Join(home, ".env")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("perms = %v %v", fi, err)
	}
}

func TestCutEnvForms(t *testing.T) {
	for line, want := range map[string]string{
		`FOO=bar`:           "bar",
		`FOO="bar baz"`:     "bar baz",
		`FOO='q'`:           "q",
		`export FOO=bar`:    "bar",
		`  FOO = spaced  `:  "spaced",
		`URL=https://x/y:1`: "https://x/y:1",
	} {
		_, got, ok := cutEnv(line)
		if !ok || got != want {
			t.Fatalf("%q -> %q,%v want %q", line, got, ok, want)
		}
	}
	for _, bad := range []string{"", "# comment", "NOEQUALS", "HAS SPACE=x", "KE:Y=x"} {
		if _, _, ok := cutEnv(bad); ok {
			t.Fatalf("%q parsed", bad)
		}
	}
}
