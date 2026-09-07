package setup

import (
	"regexp"
	"strings"
)

var tgTokenRe = regexp.MustCompile(`^\d+:[\w-]+$`)

// SetupGateway is the Messaging Platforms section: per-platform token,
// allowlist, and home channel for Telegram and Discord.
func SetupGateway(s *State) {
	Header("Messaging Platforms", false)
	emit("Connect to messaging platforms to chat with BotReaper from anywhere.")
	emit("")

	configured := []string{}
	if PromptYesNo("Configure Telegram?", s.Secret("TELEGRAM_BOT_TOKEN") == "") {
		if setupTelegram(s) {
			configured = append(configured, "Telegram")
		}
	}
	if PromptYesNo("Configure Discord?", s.Secret("DISCORD_BOT_TOKEN") == "") {
		if setupDiscord(s) {
			configured = append(configured, "Discord")
		}
	}
	if len(configured) == 0 {
		emit("No platforms selected. Run 'botreaper setup gateway' later to configure.")
		return
	}

	emit("")
	Rule()
	emit("Messaging platforms configured!")

	var noHome []string
	for _, p := range configured {
		key := strings.ToUpper(p) + "_HOME_CHANNEL"
		if s.Secret(key) == "" {
			noHome = append(noHome, p)
		}
	}
	if len(noHome) > 0 {
		Warning("No home channel set for: %s", strings.Join(noHome, ", "))
		emit("   Without a home channel, cron jobs and cross-platform")
		emit("   messages can't be delivered to those platforms.")
	}
}

func setupTelegram(s *State) bool {
	Header("Telegram", false)
	if cur := s.Secret("TELEGRAM_BOT_TOKEN"); cur != "" {
		emit("Telegram: already configured")
		if !PromptYesNo("Reconfigure Telegram?", false) {
			ensureAllowlist(s, "Telegram", "TELEGRAM_ALLOWED_USERS")
			return true
		}
	}
	emit("Create a bot via @BotFather on Telegram")
	for {
		tok := PromptSecret("Telegram bot token")
		if tok == "" {
			emit("No token provided. Cancelled.")
			return false
		}
		if !tgTokenRe.MatchString(tok) {
			Error("Invalid token format. Expected: <numeric_id>:<alphanumeric_hash> (e.g., 123456789:ABCdefGHI-jklMNOpqrSTUvwxYZ)")
			continue
		}
		if err := s.SetSecret("TELEGRAM_BOT_TOKEN", tok); err != nil {
			Error("Could not save token: %v", err)
			return false
		}
		break
	}
	Success("Telegram token saved")
	askAllowlist(s, "Telegram", "TELEGRAM_ALLOWED_USERS")
	askHomeChannel(s, "Telegram", "TELEGRAM_HOME_CHANNEL")
	return true
}

func setupDiscord(s *State) bool {
	Header("Discord", false)
	if cur := s.Secret("DISCORD_BOT_TOKEN"); cur != "" {
		emit("Discord: already configured")
		if !PromptYesNo("Reconfigure Discord?", false) {
			ensureAllowlist(s, "Discord", "DISCORD_ALLOWED_USERS")
			return true
		}
	}
	emit("Create an application at https://discord.com/developers and copy the bot token.")
	emit("Enable the Message Content Intent on the Bot page or the bot stays silent in servers.")
	tok := PromptSecret("Discord bot token")
	if tok == "" {
		emit("No token provided. Cancelled.")
		return false
	}
	if err := s.SetSecret("DISCORD_BOT_TOKEN", tok); err != nil {
		Error("Could not save token: %v", err)
		return false
	}
	Success("Discord token saved")
	askAllowlist(s, "Discord", "DISCORD_ALLOWED_USERS")
	askHomeChannel(s, "Discord", "DISCORD_HOME_CHANNEL")
	return true
}

// askAllowlist mirrors the Python allowlist prompts verbatim.
func askAllowlist(s *State, platform, key string) {
	emit("🔒 Security: Restrict who can use your bot to avoid strangers running up your bill.")
	emit("1. Your numeric user ID (Telegram: message @userinfobot — it will reply with it).")
	v := Prompt("Additional allowed user IDs (comma-separated, optional)", "")
	if v == "" {
		Warning("⚠️  No allowlist set - anyone who finds your bot can use it!")
		return
	}
	_ = s.SetSecret(key, v)
	emit("%s allowlist configured - only listed users can use the bot", platform)
}

// ensureAllowlist nudges when reconfiguring past an empty allowlist.
func ensureAllowlist(s *State, platform, key string) {
	if s.Secret(key) != "" {
		return
	}
	Warning("⚠️  %s has no user allowlist - anyone can use your bot!", platform)
	if PromptYesNo("Add allowed users now?", true) {
		emit("   To find your user ID: message @userinfobot (Telegram) or enable Developer Mode (Discord).")
		if v := Prompt("Allowed user IDs (comma-separated, leave empty for open access)", ""); v != "" {
			_ = s.SetSecret(key, v)
			emit("%s allowlist configured - only listed users can use the bot", platform)
		}
	}
}

func askHomeChannel(s *State, platform, key string) {
	emit("📬 Home Channel: where BotReaper delivers cron job results and cross-platform messages.")
	v := Prompt("Home channel ID (leave empty to set later)", "")
	if v != "" {
		_ = s.SetSecret(key, v)
		emit("%s home channel set to %s", platform, v)
	}
}
