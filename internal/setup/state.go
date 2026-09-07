package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nousresearch/botreaper/internal/config"
)

// State carries wizard context: home paths plus the raw config KV map
// (preserves keys the typed loader ignores, e.g. terminal_backend).
type State struct {
	Home       string
	ConfigPath string
	EnvPath    string
	raw        map[string]string
	order      []string
}

// NewState loads (or initializes) the raw config for home.
func NewState(home string) (*State, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	s := &State{
		Home:       home,
		ConfigPath: filepath.Join(home, "config.yaml"),
		EnvPath:    filepath.Join(home, ".env"),
		raw:        map[string]string{},
	}
	if data, err := os.ReadFile(s.ConfigPath); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			k, v, ok := splitKV(ln)
			if !ok {
				continue
			}
			if _, seen := s.raw[k]; !seen {
				s.order = append(s.order, k)
			}
			s.raw[k] = v
		}
	}
	return s, nil
}

func splitKV(line string) (string, string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", false
	}
	i := strings.IndexByte(t, ':')
	if i < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(t[:i])
	v := strings.Trim(strings.TrimSpace(t[i+1:]), `"'`)
	return k, v, k != ""
}

// Get returns the raw value (typed defaults applied for known keys).
func (s *State) Get(key, def string) string {
	if v, ok := s.raw[key]; ok {
		return v
	}
	return def
}

// Set stages a config value (persisted by Save).
func (s *State) Set(key, value string) {
	if _, seen := s.raw[key]; !seen {
		s.order = append(s.order, key)
	}
	s.raw[key] = value
}

// Save writes config.yaml atomically.
func (s *State) Save() error {
	var b strings.Builder
	for _, k := range s.order {
		fmt.Fprintf(&b, "%s: %s\n", k, s.raw[k])
	}
	return config.AtomicWriteText(s.ConfigPath, b.String(), 0o600)
}

// Secret reads from .env (then process env), mirroring LoadSecret.
func (s *State) Secret(key string) string { return config.LoadSecret(s.Home, key) }

// SetSecret writes one .env entry.
func (s *State) SetSecret(key, value string) error { return config.SetSecret(s.Home, key, value) }

// ClearSecret removes one .env entry.
func (s *State) ClearSecret(key string) error { return config.ClearSecret(s.Home, key) }

// Backup copies config.yaml next to itself; returns "" when absent.
// Mirrors _backup_config_file (silent when there is nothing to back up).
func (s *State) Backup() string {
	raw, err := os.ReadFile(s.ConfigPath)
	if err != nil {
		return ""
	}
	name := s.ConfigPath + ".bak." + time.Now().Format("20060102_150405")
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		return ""
	}
	return name
}

// IsTTY reports interactive stdin.
func IsTTY() bool {
	if f, ok := In.(*os.File); ok {
		if fi, err := f.Stat(); err == nil {
			return fi.Mode()&os.ModeCharDevice != 0
		}
	}
	return false
}

// NonInteractive mirrors is_noninteractive(): env gate or piped stdin.
func NonInteractive(args Args) bool {
	if v := strings.ToLower(os.Getenv("BOTREAPER_NONINTERACTIVE")); v == "1" || v == "true" || v == "yes" || v == "on" {
		return true
	}
	return args.NonInteractive || !IsTTY()
}

// PrintNonInteractiveGuidance is the verbatim no-TTY screen.
func PrintNonInteractiveGuidance(reason string) {
	emit("")
	emit(paint(cCyan, paint(cBold, "⚕ BotReaper Setup — Non-interactive mode")))
	emit("")
	if reason != "" {
		Info("%s", reason)
	}
	Info("The interactive wizard cannot be used here.")
	emit("")
	Info("Configure BotReaper using environment variables or config commands:")
	emit("  botreaper config set model.provider custom")
	emit("  botreaper config set model.base_url http://localhost:8080/v1")
	emit("  botreaper config set model.default your-model-name")
	emit("")
	Info("Or set OPENROUTER_API_KEY / OPENAI_API_KEY in your environment.")
	emit("Run 'botreaper setup' in an interactive terminal to use the full wizard.")
	emit("")
}
