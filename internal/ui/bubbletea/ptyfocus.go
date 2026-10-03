package bubbletea

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

const shellPrompt = "shell> "

func isBangCallID(id string) bool {
	return strings.HasPrefix(id, ui.BangCallIDPrefix)
}

func (m *model) enterPtyFocus() tea.Cmd {
	m.ptyFocus = true
	m.syncInputPrompt(m.status.Mode)
	m.syncViewportContent()
	return m.setShellFocusCmd(m.ptyToolCallID, true)
}

func (m *model) leavePtyFocus() tea.Cmd {
	cmd := m.setShellFocusCmd(m.ptyToolCallID, false)
	m.ptyFocus = false
	m.syncInputPrompt(m.status.Mode)
	m.syncViewportContent()
	return cmd
}

func (m *model) clearPtyFocus() tea.Cmd {
	cmd := m.setShellFocusCmd(m.ptyToolCallID, false)
	m.ptyFocus = false
	m.ptyToolCallID = ""
	m.syncInputPrompt(m.status.Mode)
	return cmd
}

func (m *model) handlePtyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "shift+tab" {
		return m, m.leavePtyFocus()
	}
	if b := keyBytesForPTY(msg); len(b) > 0 {
		return m, m.writeShellInputCmd(b)
	}
	return m, nil
}

func (m *model) setShellFocusCmd(callID string, on bool) tea.Cmd {
	if callID == "" {
		return nil
	}
	w, ok := m.app.(ui.ShellInputWriter)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		w.SetShellFocus(callID, on)
		return nil
	}
}

func (m *model) writeShellInputCmd(b []byte) tea.Cmd {
	w, ok := m.app.(ui.ShellInputWriter)
	if !ok || m.ptyToolCallID == "" {
		return nil
	}
	callID := m.ptyToolCallID
	return func() tea.Msg {
		// The PTY may not be attached yet, or the process may have just
		// exited; the card settling is the user-visible signal.
		_ = w.WriteShellInput(callID, b)
		return nil
	}
}

func keyBytesForPTY(msg tea.KeyMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "backspace":
		return []byte{0x7f}
	case "tab":
		return []byte{'\t'}
	case "esc", "escape":
		return []byte{0x1b}
	case "ctrl+c":
		return []byte{0x03}
	case "ctrl+d":
		return []byte{0x04}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "space":
		return []byte{' '}
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
		return []byte(string(msg.Runes))
	}
	return nil
}
