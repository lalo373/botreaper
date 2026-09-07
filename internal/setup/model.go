package setup

import (
	"strings"

	"github.com/nousresearch/botreaper/internal/provider"
)

// providerBlurb describes each registry provider in the picker (short,
// capability-honest; OpenRouter's line is verbatim Python).
var providerBlurb = map[string]string{
	"nous":          "Nous Portal (BotReaper models, API key)",
	"openrouter":    "OpenRouter (Pay-per-use API aggregator)",
	"openai":        "OpenAI (direct API)",
	"ollama":        "Ollama (local models, no key)",
	"anthropic":     "Anthropic (Claude API)",
	"gemini":        "Google Gemini (AI Studio API key)",
	"opencode-zen":  "OpenCode Zen (pay-as-you-go relay)",
	"opencode-go":   "OpenCode Go (subscription relay)",
	"opencode-free": "OpenCode Free (keyless free tier)",
}

// providerLabel is the display name used in confirmations.
var providerLabel = map[string]string{
	"nous": "Nous Portal", "openrouter": "OpenRouter", "openai": "OpenAI",
	"ollama": "Ollama", "anthropic": "Anthropic", "gemini": "Gemini",
	"opencode-zen": "OpenCode Zen", "opencode-go": "OpenCode Go",
	"opencode-free": "OpenCode Free", "custom": "Custom endpoint",
}

func labelOf(name string) string {
	if l, ok := providerLabel[name]; ok {
		return l
	}
	return name
}

// SetupModelProvider is section 1 (verbatim chrome): current model/provider,
// provider picker, key → base URL → model, persist + confirm.
func SetupModelProvider(s *State) {
	Header("Inference Provider", false)
	emit("Choose how to connect to your main chat model.")
	emit("   Guide: https://hermes-agent.nousresearch.com/docs/integrations/providers")
	emit("")

	curModel := s.Get("model", "")
	if curModel == "" {
		curModel = "(not set)"
	}
	active := s.Get("provider", "")
	activeLabel := "none"
	if active != "" {
		activeLabel = labelOf(active)
	}
	emit("")
	emit("  Current model:    %s", curModel)
	emit("  Active provider:  %s", activeLabel)
	emit("")

	reg := provider.NewRegistry()
	names := []string{"nous", "openrouter", "openai", "ollama", "anthropic", "gemini", "opencode-zen", "opencode-go", "opencode-free"}
	var rows []string
	var keys []string
	def := 0
	for _, n := range names {
		p, ok := reg.Resolve(n)
		if !ok {
			continue
		}
		row := n + ": " + providerBlurb[n]
		if n == active {
			row += "  ← currently active"
			def = len(rows)
		}
		rows = append(rows, row)
		keys = append(keys, p.Name)
	}
	rows = append(rows, "Custom endpoint (enter URL manually)")
	keys = append(keys, "custom")
	rows = append(rows, "Leave unchanged")
	keys = append(keys, "")

	idx := PromptChoice("Select provider:", rows, def)
	if keys[idx] == "" {
		emit("No change.")
		return
	}
	if keys[idx] == "custom" {
		setupCustomEndpoint(s)
		return
	}
	p, _ := reg.Resolve(keys[idx])
	setupKeyedProvider(s, p)
}

// setupKeyedProvider runs key → base URL → model for one registry provider.
func setupKeyedProvider(s *State, p provider.Profile) {
	name := labelOf(p.Name)
	if !promptAPIKey(s, p, name) {
		return
	}
	promptBaseURL(s, p)
	promptModel(s, p, name)
}

func firstEnv(p provider.Profile) string {
	if len(p.EnvVars) > 0 {
		return p.EnvVars[0]
	}
	return ""
}

// promptAPIKey mirrors _prompt_api_key. Returns false when the flow aborts.
func promptAPIKey(s *State, p provider.Profile, name string) bool {
	key := firstEnv(p)
	if key == "" {
		return true // keyless (ollama, opencode-free)
	}
	cur := s.Secret(key)
	if cur == "" {
		emit("No %s API key configured.", name)
		v := PromptSecret(key + " (or Enter to cancel)")
		if v == "" {
			emit("Cancelled.")
			return false
		}
		if err := s.SetSecret(key, v); err != nil {
			Error("Could not save key: %v", err)
			return false
		}
		emit("API key saved.")
		emit("")
		return true
	}
	suffix := ""
	if len(cur) > 8 {
		suffix = cur[:8] + "... ✓"
	} else {
		suffix = "✓"
	}
	emit("  %s API key: %s", name, suffix)
	emit("")
	for {
		v := Prompt("  [K]eep / [R]eplace / [C]lear (default K)", "")
		switch strings.ToLower(v) {
		case "", "k", "keep":
			emit("")
			return true
		case "r", "replace":
			nv := PromptSecret(key)
			if nv == "" {
				emit("Cancelled.")
				return false
			}
			if err := s.SetSecret(key, nv); err != nil {
				Error("Could not save key: %v", err)
				return false
			}
			emit("  API key updated.")
			return true
		case "c", "clear":
			_ = s.ClearSecret(key)
			emit("  API key cleared.  Re-run `botreaper setup` to configure %s again.", name)
			return false
		default:
			Error("Please enter 'k', 'r' or 'c'")
		}
	}
}

// promptBaseURL mirrors the base-URL step with identical validation text.
func promptBaseURL(s *State, p provider.Profile) {
	effective := s.Get("base_url", "")
	if effective == "" {
		effective = p.BaseURL
	}
	v := Prompt("Base URL ["+effective+"]", "")
	if v == "" {
		if s.Get("base_url", "") == "" && p.BaseURL != "" {
			s.Set("base_url", p.BaseURL)
		}
		return
	}
	if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
		emit("  Invalid URL — must start with http:// or https://. Keeping current value.")
		return
	}
	s.Set("base_url", v)
}

// promptModel asks the default model and confirms verbatim.
func promptModel(s *State, p provider.Profile, name string) {
	cur := s.Get("model", "")
	q := "Default model"
	if cur != "" {
		q += " [" + cur + "]"
	}
	v := Prompt(q, "")
	if v == "" {
		v = cur
	}
	if v == "" {
		emit("No change.")
		return
	}
	s.Set("provider", p.Name)
	s.Set("model", v)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("Default model set to: %s (via %s)", v, name)
}

// setupCustomEndpoint mirrors the ad-hoc custom flow (condensed to what the
// flat config supports): URL → key → model, persisted as provider=custom.
func setupCustomEndpoint(s *State) {
	emit("Custom OpenAI-compatible endpoint configuration:")
	curURL := s.Get("base_url", "")
	u := Prompt("API base URL ["+curURL+"]", "")
	if u == "" {
		u = curURL
	}
	if u == "" {
		emit("No URL provided. Cancelled.")
		return
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		emit("Invalid URL: %s (must start with http:// or https://)", u)
		return
	}
	k := PromptSecret("API key (or Enter for none)")
	if k != "" {
		if err := s.SetSecret("CUSTOM_API_KEY", k); err != nil {
			Error("Could not save key: %v", err)
			return
		}
	}
	s.Set("provider", "custom")
	s.Set("base_url", u)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	promptModelCustom(s)
}

func promptModelCustom(s *State) {
	cur := s.Get("model", "")
	q := "Model name"
	if cur != "" {
		q += " [" + cur + "]"
	}
	v := Prompt(q, "")
	if v == "" {
		v = cur
	}
	if v == "" {
		emit("No change.")
		return
	}
	s.Set("model", v)
	if err := s.Save(); err != nil {
		Error("Could not save config: %v", err)
		return
	}
	emit("Default model set to: %s (via Custom endpoint)", v)
}
