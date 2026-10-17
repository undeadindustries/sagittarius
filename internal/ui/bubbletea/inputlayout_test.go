package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui"
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

func TestRenderInputSpansTerminalWidth(t *testing.T) {
	t.Parallel()
	// Other tests set the process color profile. Pin it so the placeholder
	// tail is checked in the same mode as a real terminal.
	lipgloss.SetColorProfile(termenv.TrueColor)
	cases := []struct {
		name        string
		value       string
		placeholder string
	}{
		{name: "idle", placeholder: inputPlaceholderIdle},
		{name: "busy", placeholder: inputPlaceholderBusy},
		{name: "wait", placeholder: inputPlaceholderWait},
		{name: "typed", value: "Test", placeholder: inputPlaceholderIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newColorInputModel()
			m.input.Placeholder = tc.placeholder
			if tc.value != "" {
				m.input.SetValue(tc.value)
			}
			m.syncInputLayout()

			raw := m.input.View()
			view := m.renderInput()
			lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
			if len(lines) == 0 {
				t.Fatal("renderInput returned no lines")
			}
			for i, line := range lines {
				if got := lipgloss.Width(line); got != m.width {
					t.Errorf("line %d width = %d, want %d", i, got, m.width)
				}
			}
			if !strings.Contains(view, inputBackgroundOpen(m)) {
				t.Errorf("composer background missing from %q", view)
			}
			if tc.value != "" {
				return
			}
			// The textarea viewport pads the empty row with plain spaces, so
			// the background stops at the placeholder. The helper must be
			// what paints that tail, or this test stops covering the bug.
			rawLine := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")[0]
			gap := m.width - ansi.StringWidth(strings.TrimRight(ansi.Strip(rawLine), " "))
			if gap <= 0 {
				t.Fatalf("placeholder already filled, plain width %d", m.width-gap)
			}
			wantTail := m.input.FocusedStyle.Base.Render(strings.Repeat(" ", gap))
			if !strings.HasSuffix(lines[0], wantTail) {
				t.Errorf("placeholder row missing background tail\nline: %q\ntail: %q", lines[0], wantTail)
			}
		})
	}
}

func newColorInputModel() *model {
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	return m
}

// inputBackgroundOpen is the SGR prefix of one input-background cell.
func inputBackgroundOpen(m *model) string {
	sample := m.input.FocusedStyle.Base.Render(" ")
	i := strings.Index(sample, " ")
	if i <= 0 {
		return sample
	}
	return sample[:i]
}

func TestVisibleInputWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		line string
		want int
	}{
		{name: "plain trailing spaces", line: "abc   ", want: 3},
		{name: "styled trailing spaces", line: "\x1b[40mabc\x1b[0m\x1b[40m   \x1b[0m", want: 3},
		{name: "reverse cursor block at end", line: "abc\x1b[7m \x1b[0m   ", want: 4},
		{name: "reverse cleared by 27", line: "abc\x1b[7m\x1b[27m   ", want: 3},
		{name: "reverse cleared by bare reset", line: "abc\x1b[7m\x1b[m   ", want: 3},
		{name: "palette index 7 is not reverse", line: "abc\x1b[38;5;7m   ", want: 3},
		{name: "colon palette index 7 is not reverse", line: "abc\x1b[38:5:7m   ", want: 3},
		{name: "rgb with 7 is not reverse", line: "abc\x1b[48;2;7;7;7m   ", want: 3},
		{name: "reverse after extended color", line: "abc\x1b[38;5;1;7m \x1b[0m ", want: 4},
		{name: "wide rune", line: "日本 ", want: 4},
		{name: "empty", line: "", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := visibleInputWidth(tc.line); got != tc.want {
				t.Errorf("visibleInputWidth(%q) = %d, want %d", tc.line, got, tc.want)
			}
		})
	}
}

// TestRenderInputKeepsCursorAfterText guards the cursor block at the end of
// typed text, which padding used to trim as trailing whitespace.
func TestRenderInputKeepsCursorAfterText(t *testing.T) {
	// Not parallel: pins the process-wide color profile.
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	for _, themeName := range []string{"default", "greyscale"} {
		t.Run(themeName, func(t *testing.T) {
			m := newModel(ui.Options{ThemeName: themeName}, quitApp{}, NewTerminal(ui.Options{}))
			m.width, m.height = 80, 24
			m.input.SetValue("hello")
			m.syncInputLayout()
			m.input.Cursor.Blink = false

			cursorCell := m.input.Cursor.Style.Inline(true).Reverse(true).Render(" ")
			if view := m.renderInput(); !strings.Contains(view, cursorCell) {
				t.Fatalf("cursor cell %q missing from composer %q", cursorCell, view)
			}
		})
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
