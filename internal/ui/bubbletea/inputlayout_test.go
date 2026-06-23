package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestInputBoxHeightReservesExtraRow(t *testing.T) {
	t.Parallel()
	if got := inputBoxHeight(2); got != 3 {
		t.Fatalf("inputBoxHeight(2) = %d, want 3", got)
	}
	if got := inputBoxHeight(6); got != maxInputRows {
		t.Fatalf("inputBoxHeight(6) = %d, want cap %d", got, maxInputRows)
	}
}

func TestInputHeightGrowsAndCaps(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.input.SetValue("a\nb\nc")
	m.syncInputLayout()
	if got := m.inputHeight(); got != 4 {
		t.Fatalf("inputHeight = %d, want 4 (3 content lines + 1)", got)
	}
	m.input.SetValue(strings.Repeat("x\n", 20))
	m.syncInputLayout()
	if got := m.inputHeight(); got != maxInputRows {
		t.Fatalf("inputHeight = %d, want cap %d", got, maxInputRows)
	}

	// Word-wrapping: narrow terminal width forces multiple display rows.
	m.width = 20
	m.input.SetValue("hello world this is a test")
	m.syncInputLayout()
	wrapped := inputContentLines(m.input)
	if wrapped < 2 {
		t.Fatalf("inputContentLines with width 20 = %d, want >= 2", wrapped)
	}
	if got := m.inputHeight(); got != inputBoxHeight(wrapped) {
		t.Fatalf("inputHeight = %d, want %d", got, inputBoxHeight(wrapped))
	}

	m.width = 80
	m.input.SetValue("hello world this is a test")
	m.syncInputLayout()
	if got := inputContentLines(m.input); got != 1 {
		t.Fatalf("inputContentLines with width 80 = %d, want 1", got)
	}
	if got := m.inputHeight(); got != 2 {
		t.Fatalf("inputHeight for short line with width 80 = %d, want 2", got)
	}
}

func TestInputWrapKeepsFirstLineVisible(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 50
	m.syncInputLayout()

	long := "show me all files written in typescript, including tests"
	m.input.SetValue(long)
	m.syncInputLayout()

	view := stripANSI(m.input.View())
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped input, got %d line(s): %q", len(lines), view)
	}

	first := strings.TrimPrefix(lines[0], "Agent> ")
	first = strings.TrimSpace(first)
	if !strings.HasPrefix(first, visibleInputPrefix(long)) {
		t.Fatalf("first visible row %q does not start with %q\nfull view:\n%s", first, visibleInputPrefix(long), view)
	}
}

func TestInputScrollToTopResetsViewport(t *testing.T) {
	t.Parallel()
	ti := textarea.New()
	ti.ShowLineNumbers = false
	ti.Prompt = "Agent> "
	ti.SetWidth(20)
	ti.SetHeight(1)
	ti.SetValue("alpha beta gamma delta")
	// Force a bad scroll state like repositionView with stale height.
	_, _ = ti.Update(nil)
	inputScrollToTop(&ti)
	view := stripANSI(ti.View())
	if !strings.Contains(view, "alpha") {
		t.Fatalf("scroll reset failed, view=%q", view)
	}
}
