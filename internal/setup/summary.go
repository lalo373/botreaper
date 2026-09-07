package setup

import (
	"fmt"
	"os"
	"strings"
)

// toolRow is one availability line: name + ok + missing-hint.
type toolRow struct {
	name    string
	ok      bool
	missing string
}

// PrintSetupSummary mirrors _print_setup_summary: provider warning,
// Tool Availability Summary, green Setup Complete box, file paths,
// edit commands, and ready commands — adapted to the subcommands this
// runtime implements.
func PrintSetupSummary(s *State) {
	provider := s.Get("provider", "")
	model := s.Get("model", "")
	if provider == "" || model == "" {
		emit("")
		Warning("No inference provider is configured — BotReaper cannot chat yet.")
		emit("  Finish this one step with:")
		emit("    botreaper setup            (pick any provider/model)")
		emit("")
	}

	rows := availability(s)
	avail := 0
	for _, r := range rows {
		if r.ok {
			avail++
		}
	}
	emit("")
	emit(paint(cCyan, paint(cBold, "◆ Tool Availability Summary")))
	emit("%d/%d tool categories available:", avail, len(rows))
	emit("")
	for _, r := range rows {
		if r.ok {
			emit("   " + paint(cGreen, "✓ "+r.name))
		} else {
			emit("   " + paint(cRed, "✗ "+r.name) + paint(cDim, " (missing "+r.missing+")"))
		}
	}
	emit("")
	if avail < len(rows) {
		Warning("Some tools are disabled. Run 'botreaper setup tools' to configure them,")
		emit("or edit %s/.env directly to add the missing API keys.", displayHome(s.Home))
		emit("")
	}

	emit(paint(cGreen, "┌─────────────────────────────────────────────────────────┐"))
	emit(paint(cGreen, "│              ✓ Setup Complete!                          │"))
	emit(paint(cGreen, "└─────────────────────────────────────────────────────────┘"))
	emit("")
	emit(paint(cCyan, paint(cBold, fmt.Sprintf("📁 All your files are in %s/:", displayHome(s.Home)))))
	emit("   %s  %s", paint(cYellow, "Settings:"), s.ConfigPath)
	emit("   %s  %s", paint(cYellow, "API Keys:"), s.EnvPath)
	emit("   %s  %s/cron/, sessions/, logs/", paint(cYellow, "Data:"), s.Home)
	emit("")
	Rule()
	emit("")
	emit(paint(cCyan, paint(cBold, "📝 To edit your configuration:")))
	emit("")
	emit("   %s  Re-run the full wizard", paint(cGreen, "botreaper setup"))
	emit("   %s  Change model/provider", paint(cGreen, "botreaper setup model"))
	emit("   %s  Change terminal backend", paint(cGreen, "botreaper setup terminal"))
	emit("   %s  Configure messaging", paint(cGreen, "botreaper setup gateway"))
	emit("   %s  Configure tool providers", paint(cGreen, "botreaper setup tools"))
	emit("")
	emit("   Or edit the files directly:")
	emit("   %s", paint(cDim, "nano "+s.ConfigPath))
	emit("   %s", paint(cDim, "nano "+s.EnvPath))
	emit("")
	Rule()
	emit("")
	emit(paint(cCyan, paint(cBold, "🚀 Ready to go!")))
	emit("")
	emit("   %s  Start chatting", paint(cGreen, "hermes"))
	emit("   %s  Start messaging gateway", paint(cGreen, "botreaper gateway --run"))
	emit("   %s  Full-screen chat", paint(cGreen, "botreaper --tui"))
	emit("")
}

// availability computes the Go tool surface (always-on builtins plus the
// key-gated web search).
func availability(s *State) []toolRow {
	webOK := s.Get("search_base_url", "") != "" && s.Secret("SEARCH_API_KEY") != ""
	searchHint := "SEARCH_API_KEY"
	if s.Get("search_base_url", "") == "" {
		searchHint = "search_base_url"
	}
	return []toolRow{
		{"File Operations (read, write, patch, search)", true, ""},
		{"Terminal/Commands", true, ""},
		{"Task Planning (todo)", true, ""},
		{"Skills (view, create, edit)", true, ""},
		{"Memory (recall, persist)", true, ""},
		{"Session Search (full-text transcripts)", true, ""},
		{"Delegation (subagents)", true, ""},
		{"Scheduled Jobs (cron)", true, ""},
		{"Web Search & Extract", webOK, searchHint},
	}
}

func displayHome(home string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(home, h) {
		return "~" + strings.TrimPrefix(home, h)
	}
	return home
}
