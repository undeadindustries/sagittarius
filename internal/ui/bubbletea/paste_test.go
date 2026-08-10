package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestModelPasteCollapse(t *testing.T) {
	m := newTestModel()
	pasteText := "1\n2\n3\n4\n5\n6\n7"

	// Send bracketed paste
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasteText), Paste: true})

	// Should be collapsed in input
	if val := m.input.Value(); val != "[Pasted Text: 7 lines]" {
		t.Fatalf("expected placeholder, got %q", val)
	}

	// Submit it
	_, cmd := m.handleEnter()
	msg := cmd().(submitMsg)

	// Display should be placeholder, line should be expanded
	if msg.display != "[Pasted Text: 7 lines]" {
		t.Errorf("expected display %q, got %q", "[Pasted Text: 7 lines]", msg.display)
	}
	if msg.line != pasteText {
		t.Errorf("expected line %q, got %q", pasteText, msg.line)
	}

	// Submit should reset paste store
	if len(m.pastes.content) != 0 {
		t.Errorf("expected paste store to be empty after submit")
	}
}

func TestModelPasteToggleAndEdit(t *testing.T) {
	m := newTestModel()
	pasteText := "1\n2\n3\n4\n5\n6\n7"

	// Paste
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasteText), Paste: true})
	placeholder := "[Pasted Text: 7 lines]"

	// Cursor is at end of placeholder. Hit Ctrl+O
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})

	// Should be expanded
	if m.input.Value() != pasteText {
		t.Fatalf("expected expanded %q, got %q", pasteText, m.input.Value())
	}

	// Hit Ctrl+O again
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})

	// Should be collapsed
	if m.input.Value() != placeholder {
		t.Fatalf("expected collapsed %q, got %q", placeholder, m.input.Value())
	}

	// Expand again
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})

	// Edit the expanded text by deleting a character inside it.
	// Cursor is at end of expanded text (len). Move left 5 times.
	for i := 0; i < 5; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	// Hit Ctrl+O
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})

	// Should NOT collapse because it was edited inside
	if m.input.Value() == placeholder || strings.Contains(m.input.Value(), placeholder) {
		t.Fatalf("should not have collapsed edited paste: %q", m.input.Value())
	}
}

func TestModelPasteAtomicDelete(t *testing.T) {
	m := newTestModel()
	pasteText := "1\n2\n3\n4\n5\n6\n7"

	// Prepend some text
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Prefix ")})

	// Paste
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasteText), Paste: true})

	// Append text
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" Suffix")})

	// Cursor is at end. Move left 7 times to be right after placeholder.
	// " Suffix" is 7 chars.
	for i := 0; i < 7; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}

	// Hit backspace
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	// Placeholder should be deleted atomically
	want := "Prefix  Suffix"
	if m.input.Value() != want {
		t.Fatalf("expected %q, got %q", want, m.input.Value())
	}

	// And paste store should prune it on next edit
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if len(m.pastes.content) != 0 {
		t.Fatalf("paste store should have pruned deleted placeholder")
	}
}
