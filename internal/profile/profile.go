// Package profile implements BOTREAPER_HOME isolation and named profiles.
// Ports hermes_constants.py (get_hermes_home) + profiles.py.
package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// GetBotReaperHome mirrors hermes_constants.get_hermes_home:
// explicit override -> $BOTREAPER_HOME -> legacy $HERMES_HOME ->
// ~/.botreaper (%LOCALAPPDATA%/botreaper on win).
func GetBotReaperHome(override string) string {
	if override != "" {
		return override
	}
	if v := os.Getenv("BOTREAPER_HOME"); v != "" {
		return v
	}
	if v := os.Getenv("HERMES_HOME"); v != "" {
		return v // pre-rebrand installs keep working
	}
	return DefaultHome()
}

// DefaultHome is the profile-unaware default root.
func DefaultHome() string {
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "botreaper")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".botreaper")
	}
	return filepath.Join(home, ".botreaper")
}

// DisplayHome mirrors display_hermes_home for user-facing text.
func DisplayHome(home string) string {
	usr, err := os.UserHomeDir()
	if err == nil && usr != "" && strings.HasPrefix(home, usr) {
		return "~" + strings.TrimPrefix(home, usr)
	}
	return home
}

// ProfilesRoot is HOME-anchored so `botreaper -p x profile list` sees all
// profiles regardless of the active BOTREAPER_HOME override.
func ProfilesRoot() string {
	usr, err := os.UserHomeDir()
	if err != nil || usr == "" {
		return filepath.Join(DefaultHome(), "profiles")
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "hermes", "profiles")
		}
	}
	return filepath.Join(usr, ".botreaper", "profiles")
}

// NamedHome maps -p/--profile to an independent storage location.
func NamedHome(name string) string {
	if name == "" || name == "default" {
		return GetBotReaperHome("")
	}
	return filepath.Join(ProfilesRoot(), name)
}

// Ensure creates the home hierarchy with safe permissions.
func Ensure(home string) error {
	for _, sub := range []string{"", "logs", "memories", "skills", "cron", "sessions"} {
		if err := os.MkdirAll(filepath.Join(home, sub), 0o700); err != nil {
			return err
		}
	}
	return nil
}

// IsDeleted reports tombstoned profiles (.deleted/).
func IsDeleted(name string) bool {
	_, err := os.Stat(filepath.Join(ProfilesRoot(), ".deleted", name))
	return err == nil
}
