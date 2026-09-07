// Package memory implements the three-layer memory engine.
// Ports tools/memory_tool*.py (MEMORY.md/USER.md) + SOUL.md handling.
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Caps mirror MemoryStore limits.
const (
	MemoryCap = 2200
	UserCap   = 1375
)

// Store is the file-backed memory (dollar-BOTREAPER_HOME/memories/).
type Store struct {
	home string
	mu   sync.Mutex
}

// NewStore binds to a BOTREAPER_HOME.
func NewStore(home string) *Store { return &Store{home: home} }

func (s *Store) memPath() string  { return filepath.Join(s.home, "memories", "MEMORY.md") }
func (s *Store) userPath() string { return filepath.Join(s.home, "memories", "USER.md") }
func (s *Store) soulPath() string { return filepath.Join(s.home, "SOUL.md") }

// Load returns the frozen prompt snapshot (cache-safe): MEMORY + USER head.
func (s *Store) Load(ctx context.Context) string {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	var sb strings.Builder
	if raw, err := os.ReadFile(s.memPath()); err == nil {
		sb.WriteString("## MEMORY\n")
		sb.WriteString(truncate(string(raw), MemoryCap))
		sb.WriteString("\n")
	}
	if raw, err := os.ReadFile(s.userPath()); err == nil {
		sb.WriteString("## USER\n")
		sb.WriteString(truncate(string(raw), UserCap))
		sb.WriteString("\n")
	}
	return sb.String()
}

// Save appends one §-delimited memory line with threat scan + drift guard.
func (s *Store) Save(ctx context.Context, text string) string {
	_ = ctx
	if bad := scan(text); bad != "" {
		return "refused: " + bad
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.memPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	prev, _ := os.ReadFile(p)
	if drifted(string(prev), text) {
		_ = os.WriteFile(p+".bak", prev, 0o600)
	}
	line := "§ " + time.Now().UTC().Format(time.RFC3339) + " " + strings.TrimSpace(text) + "\n"
	out := truncate(string(prev)+line, MemoryCap*4)
	if err := atomicWrite(p, out); err != nil {
		return "error: " + err.Error()
	}
	return "saved"
}

// Soul reads the per-profile identity file.
func (s *Store) Soul() string {
	raw, err := os.ReadFile(s.soulPath())
	if err != nil {
		return ""
	}
	return string(raw)
}

// SaveSoul writes SOUL.md with 0644 (readable, like the dashboard PUT).
func (s *Store) SaveSoul(text string) error { return atomicWriteMode(s.soulPath(), text, 0o644) }

func truncate(s string, cap int) string {
	if len(s) <= cap {
		return s
	}
	return s[len(s)-cap:]
}

func drifted(prev, next string) bool {
	return len(prev) > 0 && !strings.Contains(next, strings.Fields(prev)[0]) && len(strings.Fields(prev)) > 0
}

func scan(text string) string {
	l := strings.ToLower(text)
	for _, pat := range []string{"ignore previous", "system prompt", "exfiltrate", "<script"} {
		if strings.Contains(l, pat) {
			return "threat pattern: " + pat
		}
	}
	return ""
}

func atomicWrite(path, content string) error { return atomicWriteMode(path, content, 0o600) }

func atomicWriteMode(path, content string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mem-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	_ = tmp.Close()
	_ = os.Chmod(tmp.Name(), perm)
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
