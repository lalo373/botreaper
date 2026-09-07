// Package config implements config.yaml + secrets + logging + time.
// Ports hermes_cli/config.py, hermes_logging.py, hermes_time.py, utils.py.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Defaults mirrors hermes_cli config_defaults.py.
func Defaults() types.Config {
	return types.Config{
		Timezone:      "local",
		MaxIterations: 500,
		Model:         "nous-hermes-2",
		Provider:      "nous",
		MaxResume:     50,
	}
}

// Load reads <home>/config.yaml when present, else returns Defaults.
// The file format is a small key: value subset; unknown keys are ignored so
// older binaries tolerate newer configs.
func Load(home string) types.Config {
	cfg := Defaults()
	raw, err := os.ReadFile(filepath.Join(home, "config.yaml"))
	if err != nil {
		return cfg
	}
	for _, line := range splitLines(string(raw)) {
		k, v, ok := cutKV(line)
		if !ok {
			continue
		}
		switch k {
		case "timezone":
			cfg.Timezone = v
		case "model":
			cfg.Model = v
		case "provider":
			cfg.Provider = v
		case "base_url":
			cfg.BaseURL = v
		case "terminal_home_mode":
			cfg.TerminalHome = v
		case "max_iterations":
			if n := atoi(v, cfg.MaxIterations); n > 0 {
				cfg.MaxIterations = n
			}
		case "max_resume":
			if n := atoi(v, cfg.MaxResume); n >= 0 {
				cfg.MaxResume = n
			}
		case "cjk_fts":
			cfg.CJKFTS = v == "true" || v == "1" || v == "yes"
		case "terminal_backend":
			cfg.TerminalBackend = v
		case "terminal_docker_container":
			cfg.DockerContainer = v
		case "terminal_singularity_image":
			cfg.SingularityImage = v
		case "terminal_ssh_host":
			cfg.SSHHost = v
		case "terminal_ssh_user":
			cfg.SSHUser = v
		case "terminal_ssh_port":
			cfg.SSHPort = v
		case "terminal_ssh_key":
			cfg.SSHKey = v
		case "search_base_url":
			cfg.SearchBase = v
		}
	}
	return cfg
}

// Save writes cfg atomically (write-temp + rename, EXDEV-safe fallback).
func Save(home string, cfg types.Config) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(home, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	body := "timezone: " + cfg.Timezone + "\n" +
		"model: " + cfg.Model + "\n" +
		"provider: " + cfg.Provider + "\n" +
		"base_url: " + cfg.BaseURL + "\n" +
		"terminal_home_mode: " + cfg.TerminalHome + "\n" +
		"max_iterations: " + itoa(cfg.MaxIterations) + "\n" +
		"max_resume: " + itoa(cfg.MaxResume) + "\n" +
		"cjk_fts: " + boolStr(cfg.CJKFTS) + "\n"
	for _, kv := range [][2]string{
		{"terminal_backend", cfg.TerminalBackend},
		{"terminal_docker_container", cfg.DockerContainer},
		{"terminal_singularity_image", cfg.SingularityImage},
		{"terminal_ssh_host", cfg.SSHHost},
		{"terminal_ssh_user", cfg.SSHUser},
		{"terminal_ssh_port", cfg.SSHPort},
		{"terminal_ssh_key", cfg.SSHKey},
		{"search_base_url", cfg.SearchBase},
	} {
		if kv[1] != "" {
			body += kv[0] + ": " + kv[1] + "\n"
		}
	}
	if _, err := tmp.WriteString(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(home, "config.yaml")); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// LoadSecret reads a credential from <home>/.env (secrets only; behavior
// belongs in config.yaml). Mirrors the .env bridge in config_env.py.
func LoadSecret(home, key string) string {
	raw, err := os.ReadFile(filepath.Join(home, ".env"))
	if err != nil {
		return os.Getenv(key)
	}
	for _, line := range splitLines(string(raw)) {
		k, v, ok := cutEnv(line)
		if !ok {
			continue
		}
		if k == key {
			return v
		}
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return ""
}

var (
	tzMu    sync.RWMutex
	tzCache = map[string]*time.Location{}
	tzCfg   string
)

// Now mirrors hermes_time.now(): BOTREAPER_TIMEZONE env -> config timezone ->
// local. Results are cached per timezone value.
func Now() time.Time {
	tzMu.RLock()
	name := tzCfg
	tzMu.RUnlock()
	if v := os.Getenv("BOTREAPER_TIMEZONE"); v != "" {
		name = v
	}
	if name == "" || name == "local" {
		return time.Now()
	}
	tzMu.RLock()
	loc, ok := tzCache[name]
	tzMu.RUnlock()
	if ok {
		return time.Now().In(loc)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Now()
	}
	tzMu.Lock()
	tzCache[name] = loc
	tzMu.Unlock()
	return time.Now().In(loc)
}

// SetTimezoneCacheName records the config timezone for Now().
func SetTimezoneCacheName(name string) {
	tzMu.Lock()
	tzCfg = name
	tzMu.Unlock()
}

// ResetTimezoneCache clears cached locations (tests).
func ResetTimezoneCache() {
	tzMu.Lock()
	tzCache = map[string]*time.Location{}
	tzCfg = ""
	tzMu.Unlock()
}

// AtomicWriteText mirrors utils.atomic_write_text: temp + rename.
func AtomicWriteText(path, content string, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".atomic-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return errors.New("config: atomic rename failed: " + err.Error())
	}
	return nil
}
