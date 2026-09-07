package setup

import (
	"github.com/nousresearch/botreaper/internal/config"
)

// Args mirrors the `botreaper setup` flags. (--portal has no Go equivalent:
// OAuth device flows don't exist in this runtime.)
type Args struct {
	Section        string
	NonInteractive bool
	Reset          bool
	Quick          bool
}

// Sections lists the runnable section names.
func Sections() []string { return []string{"model", "terminal", "gateway", "tools", "agent"} }

// sectionLabel mirrors SETUP_SECTIONS labels.
func sectionLabel(key string) string {
	switch key {
	case "model":
		return "Model & Provider"
	case "terminal":
		return "Terminal Backend"
	case "gateway":
		return "Messaging Platforms (Gateway)"
	case "tools":
		return "Tools"
	case "agent":
		return "Agent Settings"
	}
	return key
}

// Run executes the wizard (or one section) against home.
func Run(home string, args Args) error {
	s, err := NewState(home)
	if err != nil {
		return err
	}
	backup := s.Backup()

	// --reset is non-interactive-safe and runs before the TTY gate.
	if args.Reset {
		if err := config.Save(home, config.Defaults()); err != nil {
			return err
		}
		ns, err := NewState(home)
		if err != nil {
			return err
		}
		s = ns
		Success("Configuration reset to defaults.")
	}

	if NonInteractive(args) {
		PrintNonInteractiveGuidance("Running in a non-interactive environment (no TTY detected).")
		return nil
	}

	// Sections are interactive by nature (Python parity: the wizard needs
	// a TTY); the gate above already diverted piped runs to guidance.
	if args.Section != "" {
		return RunSection(s, args.Section)
	}

	existing := s.Get("provider", "") != ""
	if existing && args.Quick {
		runQuick(s)
		return nil
	}
	if existing {
		Header("Reconfigure", true)
		Success("You already have BotReaper configured.")
		Info("Running the full wizard — each prompt shows your current value.")
		emit("Press Enter to keep it, or type a new value to change it.")
		emit("")
		emit("Tip: jump straight to a section with 'botreaper setup model|terminal|")
		emit("     gateway|tools|agent', or fill only missing items with --quick.")
		runFull(s, true, backup)
		return nil
	}

	Banner("⚕ BotReaper Setup Wizard",
		"Let's configure your BotReaper installation.",
		"Press Ctrl+C at any time to exit.",
	)
	switch PromptChoice("How would you like to set up BotReaper?", []string{
		"Guided setup — provider, keys and messaging, step by step (recommended)",
		"Full setup — configure every provider, tool & option yourself (bring your own keys)",
		"Blank Slate — provider + terminal only, everything else later",
	}, 0) {
	case 0:
		runGuided(s, backup)
	case 2:
		runBlankSlate(s, backup)
	default:
		runFull(s, false, backup)
	}
	return nil
}

// RunSection runs one section with the magenta section banner.
func RunSection(s *State, section string) error {
	switch section {
	case "model", "terminal", "gateway", "tools", "agent":
		Banner("⚕ BotReaper Setup — " + sectionLabel(section))
		runOne(s, section)
		if err := s.Save(); err != nil {
			return err
		}
		emit("")
		Success("%s configuration complete!", sectionLabel(section))
		return nil
	case "tts", "telemetry":
		emit("The %s section has no equivalent in this runtime yet.", section)
		return nil
	}
	Error("Unknown section %q. Choose from: model|terminal|gateway|tools|agent", section)
	return errUnknownSection
}

var errUnknownSection = errorString("unknown setup section")

type errorString string

func (e errorString) Error() string { return string(e) }

func runOne(s *State, section string) {
	switch section {
	case "model":
		SetupModelProvider(s)
	case "terminal":
		SetupTerminalBackend(s)
	case "gateway":
		SetupGateway(s)
	case "tools":
		SetupTools(s)
	case "agent":
		SetupAgentSettings(s)
	}
}

// runFull mirrors _run_full_setup: location block, defaults on fresh
// installs, then model → terminal → gateway → tools → agent.
func runFull(s *State, existing bool, backup string) {
	Header("Configuration Location", false)
	emit("Config file:  %s", s.ConfigPath)
	emit("Secrets file: %s", s.EnvPath)
	emit("Data folder:  %s", s.Home)
	emit("")
	emit("You can edit these files directly or with a text editor")
	emit("")

	if !existing {
		s.Set("max_iterations", "150")
		Success("Applied recommended defaults:")
		emit("  Max iterations: 150")
		emit("  Run `botreaper setup agent` later to customize.")
	}

	for _, section := range []string{"model", "terminal", "gateway", "tools", "agent"} {
		runOne(s, section)
	}
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	if backup != "" {
		emit("")
		emit("Previous config backed up to: %s", backup)
		emit("If setup changed a value you customized, restore it with:")
		emit("  cp %s %s", backup, s.ConfigPath)
	}
	PrintSetupSummary(s)
}

// runQuick mirrors --quick: only fill missing items.
func runQuick(s *State) {
	Header("Quick Setup — Missing Items Only", true)
	missing := []string{}
	if s.Get("provider", "") == "" || s.Get("model", "") == "" {
		missing = append(missing, "inference provider/model")
	}
	if len(missing) == 0 {
		emit("Everything is configured! Nothing to do.")
		return
	}
	emit("%d required setting(s) missing:", len(missing))
	for _, m := range missing {
		emit("      • %s", m)
	}
	SetupModelProvider(s)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	PrintSetupSummary(s)
}

// runGuided walks a fresh install through provider → token → model,
// then messaging platform → token (+ Discord user ID and home channel),
// saving each answer to config.yaml / .env as it goes.
func runGuided(s *State, backup string) {
	Header("Guided Setup", true)
	emit("Answer a few questions; each answer is saved as you go.")
	emit("")

	Header("Step 1 — Model provider", false)
	SetupModelProvider(s)

	s.Set("terminal_backend", "local")
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}

	emit("")
	Header("Step 2 — Messaging gateway", false)
	switch PromptChoice("Which messaging platform?", []string{
		"Telegram",
		"Discord",
		"Both (Telegram + Discord)",
		"Skip — set up later with 'botreaper setup gateway'",
	}, 0) {
	case 0:
		setupTelegram(s)
	case 1:
		setupDiscord(s)
	case 2:
		setupTelegram(s)
		setupDiscord(s)
	}
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("")
	emit("Setup complete! You're ready to go.")
	emit("Start the gateway any time with:  botreaper gateway --run")
	PrintSetupSummary(s)
	_ = backup
}

// runBlankSlate mirrors the blank-slate path: provider + terminal, then
// pointers for everything else.
func runBlankSlate(s *State, backup string) {
	Header("Blank Slate Setup", true)
	emit("Everything starts minimal. First the required provider & model,")
	emit("then the terminal backend — the rest stays one command away.")
	emit("")
	Header("Step 1 — Provider & Model (required)", false)
	SetupModelProvider(s)
	Header("Step 2 — Terminal Backend", false)
	SetupTerminalBackend(s)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("")
	Success("Minimal baseline applied:")
	Info("  Provider + terminal configured; everything else on demand.")
	emit("  Add messaging:     botreaper setup gateway")
	emit("  Tune agent:        botreaper setup agent")
	emit("")
	PrintSetupSummary(s)
	_ = backup
}
