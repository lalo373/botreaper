// Package provider implements LLM provider profiles and client lifecycle.
// Ports providers/base.py, provider_registry.py, client_lifecycle.py,
// auxiliary_client.py and the per-vendor adapters.
package provider

import (
	"strings"
)

// Profile mirrors providers/base.py ProviderProfile.
type Profile struct {
	Name               string
	APIMode            string
	Aliases            []string
	EnvVars            []string
	BaseURL            string
	ModelsURL          string
	AuthType           string
	SupportsVision     bool
	VisionToolMessages bool
	PromptCacheKey     bool
	FixedTemperature   *float64
	DefaultMaxTokens   int
	DefaultAuxModel    string
	DefaultHeaders     map[string]string
	SupportedEfforts   []string
}

// opencodeAttribution mirrors the Python opencode-zen/go/free plugins:
// same values as OpenRouter, sent via DefaultHeaders so they survive model
// switches and credential rotation. It returns a fresh map per caller so
// profiles never share mutable state (cf. Python's dict(...) copies).
func opencodeAttribution() map[string]string {
	return map[string]string{
		"HTTP-Referer": "https://github.com/nousresearch/botreaper",
		"X-Title":      "BotReaper",
		"User-Agent":   "BotReaper/go",
	}
}

// Builtins covers the four first-class streaming providers plus the
// aggregators the Python tree resolves most often, including the OpenCode
// Zen relay (https://opencode.ai) in its three flavors: Zen (keyed), Go
// (keyed, separate relay path), and Free (keyless).
func Builtins() []Profile {
	return []Profile{
		{Name: "nous", APIMode: "chat_completions", Aliases: []string{"nousresearch", "hermes"}, EnvVars: []string{"NOUS_API_KEY"}, BaseURL: "https://api.nousresearch.com/v1", AuthType: "bearer", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192},
		{Name: "openrouter", APIMode: "chat_completions", EnvVars: []string{"OPENROUTER_API_KEY"}, BaseURL: "https://openrouter.ai/api/v1", AuthType: "bearer", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192, DefaultHeaders: map[string]string{"HTTP-Referer": "https://nousresearch.com", "X-Title": "BotReaper"}},
		{Name: "openai", APIMode: "chat_completions", EnvVars: []string{"OPENAI_API_KEY"}, BaseURL: "https://api.openai.com/v1", AuthType: "bearer", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192, SupportedEfforts: []string{"low", "medium", "high"}},
		{Name: "ollama", APIMode: "chat_completions", EnvVars: []string{}, BaseURL: "http://localhost:11434/v1", AuthType: "none", SupportsVision: false, DefaultMaxTokens: 4096},
		{Name: "anthropic", APIMode: "anthropic_messages", EnvVars: []string{"ANTHROPIC_API_KEY"}, BaseURL: "https://api.anthropic.com/v1", AuthType: "x-api-key", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192},
		{Name: "gemini", APIMode: "gemini_native", EnvVars: []string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, BaseURL: "https://generativelanguage.googleapis.com/v1beta", AuthType: "key", SupportsVision: true, DefaultMaxTokens: 8192},
		{Name: "opencode-zen", APIMode: "chat_completions", Aliases: []string{"opencode", "opencode_zen", "zen"}, EnvVars: []string{"OPENCODE_ZEN_API_KEY"}, BaseURL: "https://opencode.ai/zen/v1", AuthType: "bearer", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192, DefaultAuxModel: "gemini-3-flash", DefaultHeaders: opencodeAttribution()},
		{Name: "opencode-go", APIMode: "chat_completions", Aliases: []string{"opencode_go", "go", "opencode-go-sub"}, EnvVars: []string{"OPENCODE_GO_API_KEY"}, BaseURL: "https://opencode.ai/zen/go/v1", AuthType: "bearer", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192, DefaultAuxModel: "glm-5", DefaultHeaders: opencodeAttribution()},
		{Name: "opencode-free", APIMode: "chat_completions", Aliases: []string{"free", "opencode_free"}, EnvVars: []string{}, BaseURL: "https://opencode.ai/zen/v1", AuthType: "none", SupportsVision: true, PromptCacheKey: true, DefaultMaxTokens: 8192, DefaultAuxModel: "laguna-s-2.1-free", DefaultHeaders: opencodeAttribution()},
	}
}

// Registry resolves provider names/aliases to profiles with last-writer-wins
// overrides, mirroring providers/__init__ discovery order.
type Registry struct {
	byName map[string]Profile
}

// NewRegistry seeds builtins then applies overrides.
func NewRegistry(overrides ...Profile) *Registry {
	r := &Registry{byName: map[string]Profile{}}
	for _, p := range Builtins() {
		r.byName[p.Name] = p
		for _, a := range p.Aliases {
			r.byName[a] = p
		}
	}
	for _, p := range overrides {
		r.byName[p.Name] = p
	}
	return r
}

// Resolve returns the profile for name/alias (case-insensitive).
func (r *Registry) Resolve(name string) (Profile, bool) {
	p, ok := r.byName[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// Names lists canonical provider names.
func (r *Registry) Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range r.byName {
		if !seen[p.Name] {
			seen[p.Name] = true
			out = append(out, p.Name)
		}
	}
	return out
}
