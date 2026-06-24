package bubbletea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputHistoryNavigateUpDown(t *testing.T) {
	t.Parallel()
	h := newInputHistory()
	h.record("first")
	h.record("second")

	if got, ok := h.up(""); !ok || got != "second" {
		t.Fatalf("up #1 = %q ok=%v, want second", got, ok)
	}
	if got, ok := h.up("second"); !ok || got != "first" {
		t.Fatalf("up #2 = %q ok=%v, want first", got, ok)
	}
	if _, ok := h.up("first"); ok {
		t.Fatal("up past oldest should report not-ok")
	}
	if got, ok := h.down("first"); !ok || got != "second" {
		t.Fatalf("down #1 = %q ok=%v, want second", got, ok)
	}
	if got, ok := h.down("second"); !ok || got != "" {
		t.Fatalf("down to draft = %q ok=%v, want empty draft", got, ok)
	}
	if _, ok := h.down(""); ok {
		t.Fatal("down past draft should report not-ok")
	}
}

func TestInputHistoryRestoresDraftAndEdits(t *testing.T) {
	t.Parallel()
	h := newInputHistory()
	h.record("old")

	// Browsing up caches the in-progress draft so coming back restores it.
	if got, ok := h.up("my draft"); !ok || got != "old" {
		t.Fatalf("up = %q ok=%v, want old", got, ok)
	}
	if got, ok := h.down("old (edited)"); !ok || got != "my draft" {
		t.Fatalf("down should restore the cached draft, got %q ok=%v", got, ok)
	}
	// The edit made at the history level is cached too.
	if got, ok := h.up("my draft again"); !ok || got != "old (edited)" {
		t.Fatalf("up should restore the cached edit, got %q ok=%v", got, ok)
	}
}

func TestInputHistoryIgnoresBlankAndDuplicates(t *testing.T) {
	t.Parallel()
	h := newInputHistory()
	h.record("dup")
	h.record("dup")
	h.record("   ")
	if len(h.messages) != 1 {
		t.Fatalf("messages = %v, want one entry", h.messages)
	}
}

func TestModelUpRecallsHistoryWhenEmpty(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.history.record("hello there")

	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "hello there" {
		t.Fatalf("Up on empty input = %q, want recalled prompt", got)
	}
	// The recalled prompt loads with the cursor at the start, so the first Down
	// moves the cursor to the line end; the second Down returns to the draft.
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.input.Value(); got != "hello there" {
		t.Fatalf("first Down should keep the recalled prompt, got %q", got)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.input.Value(); got != "" {
		t.Fatalf("Down back to draft = %q, want empty", got)
	}
}

func TestModelUpMovesToLineStartBeforeHistory(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.history.record("prev")
	m.input.SetValue("typing")
	m.syncInputLayout()

	// First Up: single line with cursor at end -> move to line start, no recall.
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "typing" {
		t.Fatalf("first Up should not recall history, got %q", got)
	}
	if off := m.input.LineInfo().ColumnOffset; off != 0 {
		t.Fatalf("first Up should move cursor to start, ColumnOffset=%d", off)
	}
	// Second Up: now at column 0 -> recall history.
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.input.Value(); got != "prev" {
		t.Fatalf("second Up should recall history, got %q", got)
	}
}

func TestModelDownMovesToLineEnd(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.input.SetValue("hello")
	m.syncInputLayout()
	m.inputCursorToBegin()

	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.input.LineInfo().ColumnOffset; got != len([]rune("hello")) {
		t.Fatalf("Down should move cursor to line end (col %d), got ColumnOffset=%d", len([]rune("hello")), got)
	}
}

func TestModelMultilineArrowsMoveBetweenLines(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.input.SetValue("line1\nline2\nline3")
	m.syncInputLayout()
	// Cursor starts at end (line index 2). Up should move to an earlier line
	// without recalling history.
	startLine := m.input.Line()
	m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Line() >= startLine {
		t.Fatalf("Up in multi-line text should move up a line: %d -> %d", startLine, m.input.Line())
	}
	if got := m.input.Value(); got != "line1\nline2\nline3" {
		t.Fatalf("multi-line Up should not change the text, got %q", got)
	}
}
