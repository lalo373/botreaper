package setup

import (
	"bytes"
	"strings"
	"testing"
)

func harness(t *testing.T, input string) (*State, *bytes.Buffer) {
	t.Helper()
	oldIn, oldOut := In, Out
	var out bytes.Buffer
	In = strings.NewReader(input)
	Out = &out
	t.Cleanup(func() { In, Out = oldIn, oldOut })
	s, err := NewState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s, &out
}

func TestPromptChoiceDefault(t *testing.T) {
	_, out := harness(t, "\n")
	if got := PromptChoice("Pick:", []string{"a", "b"}, 0); got != 0 {
		t.Fatalf("got %d", got)
	}
	if !strings.Contains(out.String(), "Skipped (keeping current)") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPromptChoiceValidation(t *testing.T) {
	_, out := harness(t, "99\nxx\n2\n")
	if got := PromptChoice("Pick:", []string{"a", "b"}, 0); got != 1 {
		t.Fatalf("got %d", got)
	}
	o := out.String()
	if !strings.Contains(o, "Please enter 1-2") || !strings.Contains(o, "Please enter a number") {
		t.Fatalf("output = %q", o)
	}
}

func TestPromptYesNoLoop(t *testing.T) {
	_, out := harness(t, "maybe\nn\n")
	if PromptYesNo("Sure?", true) {
		t.Fatal("expected false")
	}
	if !strings.Contains(out.String(), "Please enter 'y' or 'n'") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPromptEOFDefaults(t *testing.T) {
	harness(t, "")
	if got := Prompt("Q", "dflt"); got != "dflt" {
		t.Fatalf("prompt = %q", got)
	}
	if !PromptYesNo("Q?", true) {
		t.Fatal("yes/no EOF must return default")
	}
	if got := PromptChoice("Q:", []string{"a"}, 0); got != 0 {
		t.Fatal("choice EOF must return default")
	}
}

func TestModelSectionFreeTier(t *testing.T) {
	s, _ := harness(t, "9\n\nlaguna-s-2.1-free\n")
	SetupModelProvider(s)
	if s.Get("provider", "") != "opencode-free" {
		t.Fatalf("provider = %q", s.Get("provider", ""))
	}
	if s.Get("model", "") != "laguna-s-2.1-free" {
		t.Fatalf("model = %q", s.Get("model", ""))
	}
	if s.Get("base_url", "") != "https://opencode.ai/zen/v1" {
		t.Fatalf("base = %q", s.Get("base_url", ""))
	}
}

func TestModelSectionKeyedSavesSecret(t *testing.T) {
	s, _ := harness(t, "3\nsk-test-123\n\nmy-model\n")
	SetupModelProvider(s)
	if got := s.Secret("OPENAI_API_KEY"); got != "sk-test-123" {
		t.Fatalf("key = %q", got)
	}
	if s.Get("model", "") != "my-model" {
		t.Fatalf("model = %q", s.Get("model", ""))
	}
}

func TestTerminalSectionLocal(t *testing.T) {
	s, _ := harness(t, "\n")
	SetupTerminalBackend(s)
	if s.Get("terminal_backend", "") != "local" {
		t.Fatalf("backend = %q", s.Get("terminal_backend", ""))
	}
}

func TestGatewayTelegram(t *testing.T) {
	s, _ := harness(t, "\n123:abc\n\n\nn\n")
	SetupGateway(s)
	if got := s.Secret("TELEGRAM_BOT_TOKEN"); got != "123:abc" {
		t.Fatalf("token = %q", got)
	}
	if got := s.Secret("DISCORD_BOT_TOKEN"); got != "" {
		t.Fatalf("discord should be skipped, got %q", got)
	}
}

func TestGatewayTelegramRejectsBadToken(t *testing.T) {
	s, out := harness(t, "\nnot-a-token\n123:abc\n\n\nn\n")
	SetupGateway(s)
	if !strings.Contains(out.String(), "Invalid token format") {
		t.Fatalf("output = %q", out.String())
	}
	if got := s.Secret("TELEGRAM_BOT_TOKEN"); got != "123:abc" {
		t.Fatalf("token = %q", got)
	}
}

func TestAgentSection(t *testing.T) {
	s, _ := harness(t, "150\n")
	SetupAgentSettings(s)
	if s.Get("max_iterations", "") != "150" {
		t.Fatalf("iterations = %q", s.Get("max_iterations", ""))
	}
}

func TestAgentSectionBadNumber(t *testing.T) {
	s, out := harness(t, "abc\n")
	s.Set("max_iterations", "500")
	SetupAgentSettings(s)
	if !strings.Contains(out.String(), "Invalid number, keeping current value") {
		t.Fatalf("output = %q", out.String())
	}
	if s.Get("max_iterations", "") != "500" {
		t.Fatal("bad input must not overwrite")
	}
}

func TestBackup(t *testing.T) {
	s, _ := harness(t, "")
	s.Set("provider", "nous")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	bak := s.Backup()
	if bak == "" {
		t.Fatal("no backup path")
	}
}

func TestNonInteractive(t *testing.T) {
	s, out := harness(t, "")
	if err := Run(s.Home, Args{NonInteractive: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Non-interactive mode") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunSectionUnknown(t *testing.T) {
	s, _ := harness(t, "")
	if err := RunSection(s, "telemetry"); err != nil {
		t.Fatalf("telemetry should note-and-continue: %v", err)
	}
	if err := RunSection(s, "bogus"); err == nil {
		t.Fatal("unknown section must error")
	}
}

func TestWizardPipedIsNonInteractive(t *testing.T) {
	// Python parity: piped stdin (no TTY) takes the guidance path.
	s, out := harness(t, "0\n1\n")
	if err := Run(s.Home, Args{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Non-interactive mode") {
		t.Fatalf("output = %q", out.String())
	}
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "" {
		t.Fatal("non-interactive run must not write config")
	}
}

func TestRunFullFresh(t *testing.T) {
	// model(free) + base + model + terminal(local) + telegram(n) +
	// discord(n) + tools + agent.
	s, out := harness(t, "9\n\nlaguna\n\nn\nn\n\n\n")
	runFull(s, false, "")
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "opencode-free" || ns.Get("terminal_backend", "") != "local" {
		t.Fatalf("provider=%q backend=%q", ns.Get("provider", ""), ns.Get("terminal_backend", ""))
	}
	for _, want := range []string{"Configuration Location", "Setup Complete!", "Tool Availability Summary", "Ready to go!"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestRunBlankSlate(t *testing.T) {
	s, out := harness(t, "9\n\nlaguna\n\n")
	runBlankSlate(s, "")
	if !strings.Contains(out.String(), "Minimal baseline applied:") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunSectionModel(t *testing.T) {
	s, out := harness(t, "9\n\nlaguna\n")
	if err := RunSection(s, "model"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "configuration complete!") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestResetRestoresDefaults(t *testing.T) {
	s, _ := harness(t, "")
	s.Set("provider", "nous")
	s.Set("model", "x")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Run(s.Home, Args{Reset: true, NonInteractive: true}); err != nil {
		t.Fatal(err)
	}
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "nous" || ns.Get("model", "") != "nous-hermes-2" {
		t.Fatalf("reset broke: %q %q", ns.Get("provider", ""), ns.Get("model", ""))
	}
}

func TestQuickFillsMissing(t *testing.T) {
	s, out := harness(t, "9\n\nlaguna\n")
	s.Set("terminal_backend", "local")
	runQuick(s)
	if !strings.Contains(out.String(), "Missing Items Only") {
		t.Fatalf("output = %q", out.String())
	}
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "opencode-free" {
		t.Fatal("quick did not fill provider")
	}
}

func TestQuickClean(t *testing.T) {
	s, out := harness(t, "")
	s.Set("provider", "nous")
	s.Set("model", "m")
	runQuick(s)
	if !strings.Contains(out.String(), "Nothing to do") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestGuidedSetupPath(t *testing.T) {
	// provider(free) + base + model + gateway(skip).
	s, out := harness(t, "9\n\nlaguna-s-2.1-free\n4\n")
	runGuided(s, "")
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "opencode-free" {
		t.Fatalf("provider = %q", ns.Get("provider", ""))
	}
	if ns.Get("model", "") != "laguna-s-2.1-free" {
		t.Fatalf("model = %q", ns.Get("model", ""))
	}
	for _, want := range []string{"Setup Complete!", "botreaper gateway --run"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q:\n%s", want, out.String())
		}
	}
}

func TestGuidedSetupDiscord(t *testing.T) {
	// provider(openai) + key + base + model + gateway(discord) with
	// bot token, user ID allowlist and home channel.
	s, _ := harness(t, "3\nsk-test\n\nmy-model\n2\ntok-123\n42\n99\n")
	runGuided(s, "")
	ns, err := NewState(s.Home)
	if err != nil {
		t.Fatal(err)
	}
	if ns.Get("provider", "") != "openai" || ns.Get("model", "") != "my-model" {
		t.Fatalf("provider=%q model=%q", ns.Get("provider", ""), ns.Get("model", ""))
	}
	if got := ns.Secret("OPENAI_API_KEY"); got != "sk-test" {
		t.Fatalf("provider key = %q", got)
	}
	if got := ns.Secret("DISCORD_BOT_TOKEN"); got != "tok-123" {
		t.Fatalf("discord token = %q", got)
	}
	if got := ns.Secret("DISCORD_ALLOWED_USERS"); got != "42" {
		t.Fatalf("discord user allowlist = %q", got)
	}
	if got := ns.Secret("DISCORD_HOME_CHANNEL"); got != "99" {
		t.Fatalf("discord home channel = %q", got)
	}
}
