package screendialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDialogDefaultsToKeep(t *testing.T) {
	t.Parallel()
	m := New("Option").SetSize(80, 24)
	view := m.View()
	if !strings.Contains(view, "Copying over SSH") || !strings.Contains(view, "select with Option") {
		t.Fatalf("view missing title or hint:\n%s", view)
	}
	if strings.Contains(view, "—") || strings.Contains(view, "“") {
		t.Fatal("dialog copy uses a banned character")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.Done() || next.Choice() != ChoiceKeepFullscreen {
		t.Fatalf("enter on first row = done %v choice %q", next.Done(), next.Choice())
	}
}

func TestDialogInlineChoiceAndEsc(t *testing.T) {
	t.Parallel()
	m := New("Fn, or Cmd-R")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.Done() || m.Choice() != ChoiceInline {
		t.Fatalf("down+enter = done %v choice %q", m.Done(), m.Choice())
	}

	esc := New("Option")
	esc, _ = esc.Update(tea.KeyMsg{Type: tea.KeyDown})
	esc, _ = esc.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !esc.Done() || esc.Choice() != ChoiceKeepFullscreen {
		t.Fatalf("esc = done %v choice %q", esc.Done(), esc.Choice())
	}
}

func TestDialogWrapsNarrow(t *testing.T) {
	t.Parallel()
	m := New("Fn in Terminal.app, Option in iTerm2, Shift in most others").SetSize(40, 24)
	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("line too long (%d runes): %q", len([]rune(line)), line)
		}
	}
}
