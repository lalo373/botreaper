// Package tui implements the bubbletea terminal UI over a WS client.
// Ports ui-tui/ + cli.py TUI mixins. The model never calls the engine
// directly; all traffic crosses the local WebSocket gateway.
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nousresearch/botreaper/pkg/types"
)

// Config wires the TUI to a session.
type Config struct {
	SessionID string
	Slash     []string
	Send      func(ctx context.Context, text string) error
	Cancel    func()
}

// InboundMsg carries one WS envelope into Update.
type InboundMsg types.Envelope

// Model is the Elm state: transcript + input + ghost completion.
type Model struct {
	cfg      Config
	messages []string
	input    string
	ghost    string
	spinner  int
	quitting bool
}

// New builds the model.
func New(cfg Config) Model {
	if cfg.SessionID == "" {
		cfg.SessionID = "default"
	}
	return Model{cfg: cfg}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model: printable runes append, enter submits,
// ctrl+c cancels (Goroutine ctx teardown), tab accepts ghost text.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			if m.cfg.Cancel != nil {
				m.cfg.Cancel()
			}
			m.messages = append(m.messages, "[cancelled]")
			m.input = ""
			m.ghost = ""
			return m, nil
		case "enter":
			text := strings.TrimRight(m.input, "\n")
			if strings.TrimSpace(text) == "" {
				return m, nil
			}
			m.messages = append(m.messages, "> "+text)
			m.input = ""
			m.ghost = ""
			if m.cfg.Send != nil {
				_ = m.cfg.Send(context.Background(), text)
			}
			return m, nil
		case "tab":
			if m.ghost != "" {
				m.input += m.ghost
				m.ghost = ""
			}
			return m, nil
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
				m.ghost = complete(m.input, m.cfg.Slash)
			}
			return m, nil
		default:
			if len(v.Runes) > 0 {
				m.input += string(v.Runes)
				m.ghost = complete(m.input, m.cfg.Slash)
			}
			return m, nil
		}
	case InboundMsg:
		body := summarizeEnvelope(types.Envelope(v))
		if body != "" {
			m.messages = append(m.messages, body)
		}
		return m, nil
	case tea.QuitMsg:
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	var sb strings.Builder
	for _, line := range m.messages {
		sb.WriteString(line + "\n")
	}
	sb.WriteString("> " + m.input)
	if m.ghost != "" {
		sb.WriteString("[" + m.ghost + "]")
	}
	sb.WriteString("\n")
	return sb.String()
}

// Messages exposes transcript for tests.
func (m Model) Messages() []string { return append([]string{}, m.messages...) }

// Input exposes the buffer for tests.
func (m Model) Input() string { return m.input }

// Ghost exposes completion for tests.
func (m Model) Ghost() string { return m.ghost }

func complete(input string, slash []string) string {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, " ") {
		return ""
	}
	for _, c := range slash {
		if strings.HasPrefix(c, input) {
			return strings.TrimPrefix(c, input)
		}
	}
	return ""
}

func summarizeEnvelope(e types.Envelope) string {
	switch e.Type {
	case types.EvTokenDelta:
		return string(e.Data)
	case types.EvToolStart:
		return "[tool] " + string(e.Data)
	case types.EvToolEnd:
		return "[done] " + string(e.Data)
	case types.EvSessionPatch, types.EvMessageAppended:
		return string(e.Data)
	default:
		if len(e.Data) > 0 && len(e.Data) < 4096 {
			return string(e.Data)
		}
		return ""
	}
}
