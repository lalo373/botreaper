package config

import (
	"os"
	"path/filepath"
	"strings"
)

func envPath(home string) string { return filepath.Join(home, ".env") }

// SetSecret mirrors save_env_value: upserts one KEY=value line in .env
// (0600), preserving other entries and comments.
func SetSecret(home, key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, "=#\n") {
		return errBadKey
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	var out []string
	found := false
	if raw, err := os.ReadFile(envPath(home)); err == nil {
		for _, line := range splitLines(string(raw)) {
			if k, _, ok := cutEnv(line); ok && k == key {
				if !found {
					out = append(out, key+"="+value)
				}
				found = true
				continue
			}
			out = append(out, line)
		}
	}
	if !found {
		out = append(out, key+"="+value)
	}
	joined := strings.Join(out, "\n")
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	tmp, err := os.CreateTemp(home, ".env-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(joined); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	if err := os.Rename(tmpName, envPath(home)); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// ClearSecret mirrors remove_env_value.
func ClearSecret(home, key string) error {
	raw, err := os.ReadFile(envPath(home))
	if err != nil {
		return nil // nothing to clear
	}
	var out []string
	for _, line := range splitLines(string(raw)) {
		if k, _, ok := cutEnv(line); ok && k == key {
			continue
		}
		out = append(out, line)
	}
	joined := strings.Join(out, "\n")
	if joined != "" && !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	return AtomicWriteText(envPath(home), joined, 0o600)
}

type errStr string

func (e errStr) Error() string { return string(e) }

const errBadKey errStr = "config: invalid secret key"
