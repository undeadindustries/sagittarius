package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
)

func TestInputBoxHeightMatchesContent(t *testing.T) {
	t.Parallel()
	if got := inputBoxHeight(0); got != 1 {
		t.Fatalf("inputBoxHeight(0) = %d, want 1", got)
	}
	if got := inputBoxHeight(1); got != 1 {
		t.Fatalf("inputBoxHeight(1) = %d, want 1", got)
	}
	if got := inputBoxHeight(2); got != 2 {
		t.Fatalf("inputBoxHeight(2) = %d, want 2", got)
	}
	if got := inputBoxHeight(maxInputRows + 5); got != maxInputRows {
		t.Fatalf("inputBoxHeight(over cap) = %d, want cap %d", got, maxInputRows)
	}
}

func TestInputHeightGrowsAndCaps(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.input.SetValue("a\nb\nc")
	m.syncInputLayout()
	if got := m.inputHeight(); got != 3 {
		t.Fatalf("inputHeight = %d, want 3 (one row per content line)", got)
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
	if got := m.inputHeight(); got != 1 {
		t.Fatalf("inputHeight for short line with width 80 = %d, want 1", got)
	}
}

func TestInputEmptyShowsSinglePrompt(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.syncInputLayout()
	view := stripANSI(m.input.View())
	if got := strings.Count(view, "Agent>"); got != 1 {
		t.Fatalf("empty input should show exactly one Agent> prompt, got %d:\n%s", got, view)
	}
}

func TestInputSingleLineNoDuplicatePrompt(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.input.SetValue("hello")
	m.syncInputLayout()
	view := stripANSI(m.input.View())
	if got := strings.Count(view, "Agent>"); got != 1 {
		t.Fatalf("single-line input should show exactly one Agent> prompt, got %d:\n%s", got, view)
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
