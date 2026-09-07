package provider

import "testing"

func TestResolveBuiltins(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"nous", "openrouter", "openai", "ollama", "opencode-zen", "opencode-go", "opencode-free"} {
		p, ok := r.Resolve(n)
		if !ok || p.BaseURL == "" {
			t.Fatalf("provider %s unresolvable", n)
		}
	}
	if _, ok := r.Resolve("NOUS"); !ok {
		t.Fatal("case-insensitive resolve broken")
	}
	if _, ok := r.Resolve("missing"); ok {
		t.Fatal("missing provider resolved")
	}
	if len(r.Names()) < 4 {
		t.Fatal("names incomplete")
	}
}

func TestOpenCodeProfiles(t *testing.T) {
	r := NewRegistry()
	zen, _ := r.Resolve("opencode-zen")
	if zen.BaseURL != "https://opencode.ai/zen/v1" || len(zen.EnvVars) != 1 || zen.EnvVars[0] != "OPENCODE_ZEN_API_KEY" {
		t.Fatalf("zen profile = %+v", zen)
	}
	for _, alias := range []string{"opencode", "zen", "OPENCODE_ZEN"} {
		if _, ok := r.Resolve(alias); !ok {
			t.Fatalf("zen alias %q unresolvable", alias)
		}
	}
	goProf, _ := r.Resolve("opencode-go")
	if goProf.BaseURL != "https://opencode.ai/zen/go/v1" || goProf.EnvVars[0] != "OPENCODE_GO_API_KEY" {
		t.Fatalf("go profile = %+v", goProf)
	}
	free, _ := r.Resolve("free")
	if free.Name != "opencode-free" || free.AuthType != "none" || len(free.EnvVars) != 0 {
		t.Fatalf("free profile = %+v", free)
	}
	if free.BaseURL != "https://opencode.ai/zen/v1" {
		t.Fatalf("free base = %s", free.BaseURL)
	}
	// Attribution headers must be per-profile copies, never shared maps.
	zen.DefaultHeaders["X-Test"] = "1"
	if _, ok := goProf.DefaultHeaders["X-Test"]; ok {
		t.Fatal("opencode profiles share header maps")
	}
}

func TestOverrideWins(t *testing.T) {
	r := NewRegistry(Profile{Name: "openai", BaseURL: "http://local"})
	p, _ := r.Resolve("openai")
	if p.BaseURL != "http://local" {
		t.Fatalf("override lost: %s", p.BaseURL)
	}
}
