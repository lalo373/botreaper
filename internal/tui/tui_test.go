package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	// Build via string parsing path used by tests: emulate rune keys.
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC, Runes: []rune{}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestGhostCompletion(t *testing.T) {
	m := New(Config{Slash: []string{"/model", "/reset"}})
	nm, _ := m.Update(keyMsg("/mod"))
	m = nm.(Model)
	if m.Ghost() != "el" {
		t.Fatalf("ghost = %q", m.Ghost())
	}
	nm, _ = m.Update(keyMsg("tab"))
	if nm.(Model).Input() != "/model" {
		t.Fatalf("tab accepted = %q", nm.(Model).Input())
	}
}

func TestEnterSubmits(t *testing.T) {
	var sent string
	m := New(Config{Send: func(ctx context.Context, text string) error { return nil }})
	_ = sent
	nm, _ := m.Update(keyMsg("hi"))
	m = nm.(Model)
	nm, _ = m.Update(keyMsg("enter"))
	m = nm.(Model)
	if len(m.Messages()) != 1 || m.Input() != "" {
		t.Fatalf("after enter: %+v", m.Messages())
	}
}

func TestInboundAppends(t *testing.T) {
	m := New(Config{})
	nm, _ := m.Update(InboundMsg{Type: "token.delta", Data: []byte("tok")})
	if len(nm.(Model).Messages()) != 1 {
		t.Fatal("inbound not appended")
	}
}
