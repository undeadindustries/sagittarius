package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type recordingBangApp struct {
	quitApp
	writes []string
}

func (a *recordingBangApp) WriteBangInput(p []byte) error {
	a.writes = append(a.writes, string(p))
	return nil
}

func TestBangTabEntersPtyFocus(t *testing.T) {
	t.Parallel()
	app := &recordingBangApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.busy = true
	m.handleStream(ui.StreamEvent{
		Type:       ui.StreamToolStart,
		ToolName:   "run_shell_command",
		ToolCallID: ui.BangCallIDPrefix + "1",
		Text:       "sudo true",
	})
	if m.ptyToolCallID == "" {
		t.Fatal("ptyToolCallID unset after bang StreamToolStart")
	}
	if m.ptyFocus {
		t.Fatal("focus should start off")
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if !m.ptyFocus {
		t.Fatal("Tab should enter PTY focus")
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("expected write cmd")
	}
	cmd()
	if len(app.writes) != 1 || app.writes[0] != "x" {
		t.Fatalf("writes = %v, want [x]", app.writes)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.ptyFocus {
		t.Fatal("Shift+Tab should leave PTY focus")
	}

	view := strings.Join(m.renderToolCard(m.cardByID[m.ptyToolCallID], 80), "\n")
	if !strings.Contains(stripANSI(view), "Tab to interact") {
		t.Errorf("card missing Tab hint:\n%s", view)
	}
}

func TestBangResultClearsPtyFocus(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	id := ui.BangCallIDPrefix + "done"
	m.handleStream(ui.StreamEvent{
		Type:       ui.StreamToolStart,
		ToolName:   "run_shell_command",
		ToolCallID: id,
		Text:       "echo hi",
	})
	m.enterPtyFocus()
	m.handleStream(ui.StreamEvent{
		Type:       ui.StreamToolResult,
		ToolName:   "run_shell_command",
		ToolCallID: id,
		Text:       "hi",
	})
	if m.ptyFocus || m.ptyToolCallID != "" {
		t.Fatalf("focus=%v id=%q, want cleared", m.ptyFocus, m.ptyToolCallID)
	}
}

func TestKeyBytesForPTY(t *testing.T) {
	t.Parallel()
	if got := string(keyBytesForPTY(tea.KeyMsg{Type: tea.KeyEnter})); got != "\r" {
		t.Errorf("enter = %q", got)
	}
	if got := string(keyBytesForPTY(tea.KeyMsg{Type: tea.KeyCtrlC})); got != "\x03" {
		t.Errorf("ctrl+c = %q", got)
	}
	if got := string(keyBytesForPTY(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})); got != "a" {
		t.Errorf("rune = %q", got)
	}
}
