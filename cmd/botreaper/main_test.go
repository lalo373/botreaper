package main

import (
	"strings"
	"testing"
)

func TestGatewayNeedsTokens(t *testing.T) {
	err := run([]string{"--home", t.TempDir(), "gateway", "--run"})
	if err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestGatewayStatus(t *testing.T) {
	// No tokens: status lists both channels as unconfigured, no error.
	if err := run([]string{"--home", t.TempDir(), "gateway"}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestGatewayRejectsPositional(t *testing.T) {
	err := run([]string{"--home", t.TempDir(), "gateway", "bogus"})
	if err == nil {
		t.Fatal("expected error for positional arg")
	}
}

func TestEnvForChannels(t *testing.T) {
	if envFor("nous") != "NOUS_API_KEY" {
		t.Fatal("default env mapping broken")
	}
}
