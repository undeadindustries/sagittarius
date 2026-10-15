package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestMouseDefaultOn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts ui.Options
		want bool
	}{
		{"interactive", ui.Options{}, true},
		{"headless", ui.Options{Headless: true}, false},
		{"screen reader", ui.Options{ScreenReader: true}, false},
	}
	for _, tc := range tests {
		if got := mouseDefaultOn(tc.opts); got != tc.want {
			t.Errorf("%s: mouseDefaultOn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestWrapJoins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
		rows []string
		want []string
	}{
		{"space consumed", "hello world foo", []string{"hello", "world foo"}, []string{"", " "}},
		{"mid-token break", "abcdefgh", []string{"abcd", "efgh"}, []string{"", ""}},
		{"double space kept", "a  b", []string{"a", "b"}, []string{"", "  "}},
		{"styled row", "one two", []string{"\x1b[1mone\x1b[0m", "\x1b[1mtwo\x1b[0m"}, []string{"", " "}},
		{"unmatched falls back", "one two", []string{"one", "zzz"}, []string{"", " "}},
	}
	for _, tc := range tests {
		got := wrapJoins(tc.src, tc.rows)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("%s: wrapJoins = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestExtractSelection(t *testing.T) {
	t.Parallel()
	lines := []scrollLine{
		{text: "● first paragraph", indent: 2},
		{text: "  continues here", cont: true, join: " ", indent: 2},
		{text: "╭─ card ─╮", chrome: true},
		{text: "│ inside │", indent: 2, clip: 8},
		{text: "╰────────╯", chrome: true},
		{text: "  abcd", indent: 2},
		{text: "  efgh", cont: true, indent: 2},
	}
	tests := []struct {
		name string
		a, b selPos
		want string
	}{
		{"rejoins wrapped prose", selPos{0, 0}, selPos{1, 99}, "first paragraph continues here"},
		{"reverse order", selPos{1, 99}, selPos{0, 0}, "first paragraph continues here"},
		{"partial columns", selPos{0, 2}, selPos{0, 6}, "first"},
		{"skips chrome and borders", selPos{2, 0}, selPos{4, 99}, "inside"},
		{"mid-token wrap has no join", selPos{5, 0}, selPos{6, 99}, "abcdefgh"},
		{"across blocks", selPos{1, 2}, selPos{3, 99}, "continues here\ninside"},
		{"past the end clamps", selPos{6, 0}, selPos{40, 0}, "efgh"},
	}
	for _, tc := range tests {
		if got := extractSelection(lines, tc.a, tc.b); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// newSelectionModel returns a 40x20 model whose scrollback holds one response.
func newSelectionModel(t *testing.T, response string) *model {
	t.Helper()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.blocks = append(m.blocks, scrollBlock{role: roleResponse, text: response})
	m.syncViewportContent()
	return m
}

// rowsContaining returns the first and last scrollLine indexes whose plain
// text contains any of the needles.
func rowsContaining(t *testing.T, m *model, first, last string) (int, int) {
	t.Helper()
	lo, hi := -1, -1
	for i, l := range m.scrollLines {
		plain := ansi.Strip(l.text)
		if lo < 0 && strings.Contains(plain, first) {
			lo = i
		}
		if strings.Contains(plain, last) {
			hi = i
		}
	}
	if lo < 0 || hi < 0 {
		t.Fatalf("rows %q..%q not found in %d lines", first, last, len(m.scrollLines))
	}
	return lo, hi
}

func TestExtractSelectionRejoinsRenderedProse(t *testing.T) {
	t.Parallel()
	para := "The quick brown fox jumps over the lazy dog while the band plays loudly."
	m := newSelectionModel(t, para)
	lo, hi := rowsContaining(t, m, "The quick", "loudly")
	if lo == hi {
		t.Fatal("paragraph did not wrap; widen the text")
	}
	if got := extractSelection(m.scrollLines, selPos{lo, 0}, selPos{hi, 999}); got != para {
		t.Fatalf("got %q, want %q", got, para)
	}
}

func TestExtractSelectionRejoinsWrappedCode(t *testing.T) {
	t.Parallel()
	cmd := "curl -fsSL https://example.com/a/very/long/path/to/install.sh | sh -s -- --yes"
	m := newSelectionModel(t, "Run:\n\n```sh\n"+cmd+"\n```")
	lo, hi := rowsContaining(t, m, "curl", "--yes")
	if lo == hi {
		t.Fatal("code line did not wrap")
	}
	if got := extractSelection(m.scrollLines, selPos{lo, 0}, selPos{hi, 999}); got != cmd {
		t.Fatalf("got %q, want %q", got, cmd)
	}
}

func TestExtractSelectionUserAndToolBlocks(t *testing.T) {
	t.Parallel()
	prompt := "please refactor the selection handler so that it rejoins wrapped rows"
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 40, Height: 30})
	m.blocks = append(m.blocks,
		scrollBlock{role: roleUser, text: prompt},
		scrollBlock{role: roleInfo, text: "first info line\nsecond info line"},
	)
	m.syncViewportContent()
	lo, hi := rowsContaining(t, m, "please", "rows")
	if got := extractSelection(m.scrollLines, selPos{lo, 0}, selPos{hi, 999}); got != prompt {
		t.Fatalf("user block: got %q, want %q", got, prompt)
	}
	lo, hi = rowsContaining(t, m, "first info", "second info")
	if got := extractSelection(m.scrollLines, selPos{lo, 0}, selPos{hi, 999}); got != "first info line\nsecond info line" {
		t.Fatalf("info block: got %q", got)
	}
}

func mouse(x, y int, action tea.MouseAction, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: action, Button: button}
}

// runCopy executes a copy command against a recording sink.
func runCopy(t *testing.T, m *model, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a copy command")
	}
	msg, ok := cmd().(clipboardResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want clipboardResultMsg", msg)
	}
	if !msg.silent {
		t.Fatal("selection copy should be silent")
	}
	return msg.text
}

func newDragModel(t *testing.T) *model {
	t.Helper()
	var b strings.Builder
	for i := range 60 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString("\n\n")
	}
	m := newSelectionModel(t, b.String())
	m.localCopy = func(string) error { return nil }
	m.getenv = func(string) string { return "" }
	m.viewport.GotoTop()
	return m
}

func TestDragSelectsAndCopiesOnRelease(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	top := m.viewportTop()
	m.handleMouse(mouse(2, top+1, tea.MouseActionPress, tea.MouseButtonLeft))
	if m.followBottom {
		t.Fatal("a drag should pause following the bottom")
	}
	m.handleMouse(mouse(10, top+3, tea.MouseActionMotion, tea.MouseButtonLeft))
	if !m.sel.moved {
		t.Fatal("motion should mark the selection moved")
	}
	_, cmd := m.handleMouse(mouse(10, top+3, tea.MouseActionRelease, tea.MouseButtonNone))
	text := runCopy(t, m, cmd)
	want := extractSelection(m.scrollLines, selPos{m.viewport.YOffset + 1, 2}, selPos{m.viewport.YOffset + 3, 10})
	if text != want || text == "" {
		t.Fatalf("copied %q, want %q", text, want)
	}
	if m.sel.dragging {
		t.Fatal("release should end the drag")
	}
	if m.selectionContent() != joinScrollLines(m.scrollLines) {
		t.Fatal("highlight should be cleared after release")
	}
}

func TestHighlightRowKeepsTextAndWidth(t *testing.T) {
	t.Parallel()
	style := newTestModel().th.Selected.Reverse(true)
	row := "\x1b[1mbold\x1b[0m plain text"
	got := highlightRow(row, 2, 8, style)
	if ansi.Strip(got) != ansi.Strip(row) {
		t.Fatalf("highlight changed text: %q", ansi.Strip(got))
	}
	if highlightRow(row, 5, 5, style) != row {
		t.Fatal("empty range should leave the row untouched")
	}
}

func TestClickWithoutDragCopiesNothing(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	top := m.viewportTop()
	m.handleMouse(mouse(4, top+2, tea.MouseActionPress, tea.MouseButtonLeft))
	if _, cmd := m.handleMouse(mouse(4, top+2, tea.MouseActionRelease, tea.MouseButtonNone)); cmd != nil {
		t.Fatal("a click must not copy")
	}
}

func TestPressOutsideViewportIgnored(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	m.handleMouse(mouse(4, 0, tea.MouseActionPress, tea.MouseButtonLeft)) // header row
	if m.sel.dragging {
		t.Fatal("press on the header should not start a selection")
	}
}

func TestWheelWhileDraggingExtendsSelection(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	top := m.viewportTop()
	m.handleMouse(mouse(0, top, tea.MouseActionPress, tea.MouseButtonLeft))
	m.handleMouse(mouse(5, top+2, tea.MouseActionMotion, tea.MouseButtonLeft))
	before := m.sel.head.line
	m.handleMouse(mouse(5, top+2, tea.MouseActionPress, tea.MouseButtonWheelDown))
	if m.viewport.YOffset == 0 {
		t.Fatal("wheel should scroll the viewport")
	}
	if m.sel.head.line <= before {
		t.Fatalf("head line %d should move past %d after scrolling", m.sel.head.line, before)
	}
	if !m.sel.dragging || m.sel.anchor.line != 0 {
		t.Fatalf("drag should continue from its anchor, got %+v", m.sel)
	}
}

func TestEdgeAutoScroll(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	top := m.viewportTop()
	m.handleMouse(mouse(0, top+1, tea.MouseActionPress, tea.MouseButtonLeft))
	_, cmd := m.handleMouse(mouse(3, top+m.viewport.Height-1, tea.MouseActionMotion, tea.MouseButtonLeft))
	if cmd == nil || m.sel.edgeDir != 1 {
		t.Fatalf("drag to the bottom edge should start auto-scroll (dir %d)", m.sel.edgeDir)
	}
	gen := m.sel.tickGen
	if next := m.handleSelectionScrollTick(selectionScrollTickMsg{gen: gen}); next == nil {
		t.Fatal("tick should reschedule while held at the edge")
	}
	if m.viewport.YOffset != 1 {
		t.Fatalf("YOffset = %d, want 1", m.viewport.YOffset)
	}
	if m.sel.head.line != m.viewport.YOffset+m.viewport.Height-1 {
		t.Fatalf("head should follow the revealed row, got %d", m.sel.head.line)
	}

	// Moving off the edge stops the loop; a stale tick from an older drag too.
	m.handleMouse(mouse(3, top+3, tea.MouseActionMotion, tea.MouseButtonLeft))
	if next := m.handleSelectionScrollTick(selectionScrollTickMsg{gen: gen}); next != nil {
		t.Fatal("tick should stop once the pointer leaves the edge")
	}
	m.handleMouse(mouse(3, top+3, tea.MouseActionRelease, tea.MouseButtonNone))
	m.handleMouse(mouse(0, top+1, tea.MouseActionPress, tea.MouseButtonLeft))
	m.handleMouse(mouse(0, top+m.viewport.Height-1, tea.MouseActionMotion, tea.MouseButtonLeft))
	if next := m.handleSelectionScrollTick(selectionScrollTickMsg{gen: gen}); next != nil {
		t.Fatal("a tick from an earlier drag must not drive a new one")
	}
}

func TestResizeClearsSelection(t *testing.T) {
	t.Parallel()
	m := newDragModel(t)
	top := m.viewportTop()
	m.handleMouse(mouse(0, top+1, tea.MouseActionPress, tea.MouseButtonLeft))
	m.handleWindowSize(tea.WindowSizeMsg{Width: 60, Height: 20})
	if m.sel.dragging {
		t.Fatal("resize should clear the selection")
	}
}
