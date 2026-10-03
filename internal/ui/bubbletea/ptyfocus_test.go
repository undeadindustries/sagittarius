package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type recordingShellApp struct {
	quitApp
	writes map[string][]string
	focus  map[string]bool
}

func (a *recordingShellApp) WriteShellInput(callID string, p []byte) error {
	if a.writes == nil {
		a.writes = make(map[string][]string)
	}
	a.writes[callID] = append(a.writes[callID], string(p))
	return nil
}

func (a *recordingShellApp) SetShellFocus(callID string, on bool) {
	if a.focus == nil {
		a.focus = make(map[string]bool)
	}
	a.focus[callID] = on
}

func TestBangTabEntersPtyFocus(t *testing.T) {
	t.Parallel()
	app := &recordingShellApp{}
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

	_, focusCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if !m.ptyFocus {
		t.Fatal("Tab should enter PTY focus")
	}
	if focusCmd != nil {
		focusCmd()
	}
	if !app.focus[ui.BangCallIDPrefix+"1"] {
		t.Fatal("expected SetShellFocus(true) for bang call")
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("expected write cmd")
	}
	cmd()
	if len(app.writes[ui.BangCallIDPrefix+"1"]) != 1 || app.writes[ui.BangCallIDPrefix+"1"][0] != "x" {
		t.Fatalf("writes = %v, want [x]", app.writes)
	}

	_, leaveCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.ptyFocus {
		t.Fatal("Shift+Tab should leave PTY focus")
	}
	if leaveCmd != nil {
		leaveCmd()
	}
	if app.focus[ui.BangCallIDPrefix+"1"] {
		t.Fatal("expected SetShellFocus(false) after Shift+Tab")
	}

	view := strings.Join(m.renderToolCard(m.cardByID[m.ptyToolCallID], 80), "\n")
	if !strings.Contains(stripANSI(view), "Tab to interact") {
		t.Errorf("card missing Tab hint:\n%s", view)
	}
}

func TestModelShellTabEntersPtyFocus(t *testing.T) {
	t.Parallel()
	app := &recordingShellApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.busy = true
	callID := "call-model-123"
	m.handleStream(ui.StreamEvent{
		Type:       ui.StreamToolStart,
		ToolName:   "run_shell_command",
		ToolCallID: callID,
		Text:       "sudo dmidecode",
	})
	if m.ptyToolCallID != callID {
		t.Fatalf("ptyToolCallID = %q, want %q", m.ptyToolCallID, callID)
	}
	if m.ptyFocus {
		t.Fatal("focus should start off")
	}

	viewBefore := strings.Join(m.renderToolCard(m.cardByID[m.ptyToolCallID], 80), "\n")
	if !strings.Contains(stripANSI(viewBefore), "Tab to interact") {
		t.Errorf("model card missing Tab hint before focus:\n%s", viewBefore)
	}

	_, focusCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if !m.ptyFocus {
		t.Fatal("Tab should enter PTY focus for model shell")
	}
	if focusCmd != nil {
		focusCmd()
	}
	if !app.focus[callID] {
		t.Fatal("expected SetShellFocus(true) for model shell")
	}

	viewFocused := strings.Join(m.renderToolCard(m.cardByID[m.ptyToolCallID], 80), "\n")
	if !strings.Contains(stripANSI(viewFocused), "Shift+Tab to leave") {
		t.Errorf("model card missing Shift+Tab hint while focused:\n%s", viewFocused)
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if cmd == nil {
		t.Fatal("expected write cmd for model shell")
	}
	cmd()
	if len(app.writes[callID]) != 1 || app.writes[callID][0] != "p" {
		t.Fatalf("writes[%q] = %v, want [p]", callID, app.writes[callID])
	}

	_, leaveCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.ptyFocus {
		t.Fatal("Shift+Tab should leave PTY focus")
	}
	if leaveCmd != nil {
		leaveCmd()
	}
	if app.focus[callID] {
		t.Fatal("expected SetShellFocus(false) after leaving focus")
	}
}

func TestModelShellResultClearsPtyFocus(t *testing.T) {
	t.Parallel()
	app := &recordingShellApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.busy = true
	id := "call-model-456"
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
