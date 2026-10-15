package bubbletea

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// In-app mouse selection. The alternate screen hides the terminal's own
// scrollback, so the terminal's native selection can only reach the rows
// currently on screen. With mouse reporting on, Sagittarius owns the drag
// instead: it highlights the range, scrolls the viewport when the drag reaches
// an edge (or the wheel turns mid-drag), and copies the source text on release.

// selectionScrollInterval paces edge auto-scroll while a drag is held at the
// top or bottom row: fast enough to cover a long reply, slow enough to stop on
// the intended line.
const selectionScrollInterval = 60 * time.Millisecond

// scrollLine is one rendered scrollback row plus what the selection needs to
// copy source text rather than screen layout. cont marks a soft-wrapped
// continuation of the previous row and join is the whitespace the wrap
// consumed there (empty for a mid-token break). indent counts leading synthetic
// columns (role glyph, hanging indent) and clip, when non-zero, is the column
// where content ends (a card's right border). chrome rows (card borders) hold
// no copyable content.
type scrollLine struct {
	text   string
	cont   bool
	join   string
	indent int
	clip   int
	chrome bool
}

// selPos is a selection endpoint: a scrollback row and a cell column.
type selPos struct {
	line int
	col  int
}

// selectionState tracks one drag. anchor is the press position and head the
// current one. moved stays false for a plain click, which copies nothing.
// edgeDir (-1 up, +1 down) drives auto-scroll ticks; tickGen survives
// clearSelection so a tick scheduled by an earlier drag cannot drive a new one.
type selectionState struct {
	dragging bool
	moved    bool
	anchor   selPos
	head     selPos
	lastX    int
	edgeDir  int
	ticking  bool
	tickGen  uint64
}

// selectionScrollTickMsg continues edge auto-scroll for the drag tagged gen.
type selectionScrollTickMsg struct{ gen uint64 }

// mouseDefaultOn reports whether mouse reporting starts enabled: in the
// interactive full-screen TUI only. Screen-reader mode keeps the terminal's
// own handling, and headless runs have no terminal.
func mouseDefaultOn(opts ui.Options) bool {
	return !opts.Headless && !opts.ScreenReader
}

func joinScrollLines(lines []scrollLine) string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	return strings.Join(texts, "\n")
}

// framedScrollLines tags the rows of a bordered card: "│ content │" rows copy
// only the content between the borders, and every other row (top and bottom
// borders) is chrome.
func framedScrollLines(rows []string) []scrollLine {
	const borderCols = 2 // "│ " and " │"
	out := make([]scrollLine, len(rows))
	for i, r := range rows {
		plain := ansi.Strip(r)
		if !strings.HasPrefix(plain, "│") {
			out[i] = scrollLine{text: r, chrome: true}
			continue
		}
		out[i] = scrollLine{text: r, indent: borderCols, clip: max(ansi.StringWidth(plain)-borderCols, borderCols)}
	}
	return out
}

// wrapJoins returns, for each row a source line was wrapped into, the source
// whitespace between the previous row and this one. Rows are matched back into
// src in order, so a wrap that dropped a space restores that exact space and a
// hard break inside a long token (a URL) restores nothing. A row that cannot be
// located (rewritten by styling) falls back to a single space.
func wrapJoins(src string, rows []string) []string {
	joins := make([]string, len(rows))
	pos := 0
	for i, r := range rows {
		t := strings.TrimSpace(ansi.Strip(r))
		if t == "" {
			continue
		}
		idx := strings.Index(src[pos:], t)
		if idx < 0 {
			if i > 0 {
				joins[i] = " "
			}
			continue
		}
		if i > 0 {
			gap := src[pos : pos+idx]
			if strings.TrimSpace(gap) == "" {
				joins[i] = gap
			} else {
				joins[i] = " "
			}
		}
		pos += idx + len(t)
	}
	return joins
}

// orderedSelection returns the endpoints in document order.
func orderedSelection(a, b selPos) (selPos, selPos) {
	if b.line < a.line || (b.line == a.line && b.col < a.col) {
		return b, a
	}
	return a, b
}

// selectionCols is the [lo, hi) cell range row i contributes, for a row of
// width w. The end column is inclusive of the cell under the pointer.
func selectionCols(i int, start, end selPos, w int) (int, int) {
	lo, hi := 0, w
	if i == start.line {
		lo = start.col
	}
	if i == end.line {
		hi = min(end.col+1, w)
	}
	return lo, hi
}

// extractSelection returns the source text covered by [a, b]. Synthetic columns
// and card borders are dropped, trailing padding is trimmed, and soft-wrapped
// rows are rejoined with the whitespace the wrap consumed, so a paste matches
// what the model wrote rather than how the terminal width laid it out.
func extractSelection(lines []scrollLine, a, b selPos) string {
	start, end := orderedSelection(a, b)
	if len(lines) == 0 || start.line >= len(lines) {
		return ""
	}
	if end.line >= len(lines) {
		// Dragged below the content: take the last row through its end.
		end = selPos{line: len(lines) - 1, col: math.MaxInt - 1}
	}
	var out strings.Builder
	emitted := false
	for i := start.line; i <= end.line; i++ {
		ln := lines[i]
		if ln.chrome {
			continue
		}
		plain := ansi.Strip(ln.text)
		lo, hi := selectionCols(i, start, end, ansi.StringWidth(plain))
		lo = max(lo, ln.indent)
		if ln.clip > 0 {
			hi = min(hi, ln.clip)
		}
		seg := ""
		if hi > lo {
			seg = strings.TrimRight(ansi.Cut(plain, lo, hi), " ")
		}
		switch {
		case !emitted:
			emitted = true
		case ln.cont && i > start.line && !lines[i-1].chrome:
			// The join already holds any leading whitespace of this row.
			if ln.join != "" {
				seg = ln.join + strings.TrimLeft(seg, " ")
			}
		default:
			out.WriteByte('\n')
		}
		out.WriteString(seg)
	}
	return out.String()
}

// highlightRow renders row with cells [lo, hi) in the selection style. The
// selected span is restyled from plain text so the row's own colors cannot
// mask the highlight; escape codes outside it are kept so styling resumes.
func highlightRow(row string, lo, hi int, style lipgloss.Style) string {
	plain := ansi.Strip(row)
	w := ansi.StringWidth(plain)
	hi = min(hi, w)
	if hi <= lo {
		return row
	}
	return ansi.Cut(row, 0, lo) + style.Render(ansi.Cut(plain, lo, hi)) + ansi.Cut(row, hi, w)
}

// selectionContent is the viewport content: the cached scrollback, with the
// active drag highlighted.
func (m *model) selectionContent() string {
	if !m.sel.dragging || !m.sel.moved {
		return joinScrollLines(m.scrollLines)
	}
	start, end := orderedSelection(m.sel.anchor, m.sel.head)
	texts := make([]string, len(m.scrollLines))
	for i, l := range m.scrollLines {
		texts[i] = l.text
		if i < start.line || i > end.line || l.chrome {
			continue
		}
		lo, hi := selectionCols(i, start, end, ansi.StringWidth(ansi.Strip(l.text)))
		texts[i] = highlightRow(l.text, lo, hi, m.th.Selected)
	}
	return strings.Join(texts, "\n")
}

// refreshSelection repaints the viewport from the cached rows without
// re-rendering the scrollback, which keeps drag motion cheap.
func (m *model) refreshSelection() {
	m.viewport.SetContent(m.selectionContent())
}

// clearSelection drops any drag in progress, keeping tickGen so pending edge
// ticks from it are recognized as stale.
func (m *model) clearSelection() {
	m.sel = selectionState{tickGen: m.sel.tickGen}
}

// viewportTop is the screen row of the viewport's first line (below the header).
func (m *model) viewportTop() int {
	return lipgloss.Height(renderHeader(m.opts, m.th, max(m.width, 1)))
}

// selPosAt maps a pointer position to a scrollback position, clamping rows
// outside the viewport to its first or last visible line.
func (m *model) selPosAt(x, relY int) selPos {
	relY = min(max(relY, 0), max(m.viewport.Height-1, 0))
	line := m.viewport.YOffset + relY
	if n := len(m.scrollLines); n > 0 {
		line = min(line, n-1)
	}
	return selPos{line: line, col: max(x, 0)}
}

// handleMouse routes mouse events: the wheel scrolls (extending a drag in
// progress), and the left button drives selection.
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	relY := msg.Y - m.viewportTop()
	switch {
	case tea.MouseEvent(msg).IsWheel():
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if m.sel.dragging {
			m.updateSelectionHead(msg.X, relY)
			return m, cmd
		}
		m.followBottom = m.viewport.AtBottom()
		return m, cmd
	case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
		m.beginSelection(msg.X, relY)
		return m, nil
	case msg.Action == tea.MouseActionMotion && m.sel.dragging:
		return m, m.dragSelection(msg.X, relY)
	case msg.Action == tea.MouseActionRelease && m.sel.dragging:
		return m, m.endSelection(msg.X, relY)
	}
	return m, nil
}

// beginSelection starts a drag when the press lands inside the viewport. Output
// keeps streaming during a drag, so following the bottom is paused: otherwise
// each delta would scroll the text out from under the pointer.
func (m *model) beginSelection(x, relY int) {
	m.clearSelection()
	if relY < 0 || relY >= m.viewport.Height {
		return
	}
	p := m.selPosAt(x, relY)
	m.sel.dragging = true
	m.sel.anchor, m.sel.head, m.sel.lastX = p, p, x
	m.followBottom = false
}

// updateSelectionHead moves the drag head to the pointer and repaints.
func (m *model) updateSelectionHead(x, relY int) {
	m.sel.head = m.selPosAt(x, relY)
	m.sel.lastX = x
	if m.sel.head != m.sel.anchor {
		m.sel.moved = true
	}
	m.refreshSelection()
}

// dragSelection extends the drag and starts edge auto-scroll when the pointer
// sits on (or beyond) the viewport's first or last row.
func (m *model) dragSelection(x, relY int) tea.Cmd {
	m.updateSelectionHead(x, relY)
	m.sel.edgeDir = 0
	if m.sel.moved {
		switch {
		case relY <= 0:
			m.sel.edgeDir = -1
		case relY >= m.viewport.Height-1:
			m.sel.edgeDir = 1
		}
	}
	if m.sel.edgeDir == 0 || m.sel.ticking {
		return nil
	}
	m.sel.ticking = true
	m.sel.tickGen++
	return selectionScrollTick(m.sel.tickGen)
}

func selectionScrollTick(gen uint64) tea.Cmd {
	return tea.Tick(selectionScrollInterval, func(time.Time) tea.Msg {
		return selectionScrollTickMsg{gen: gen}
	})
}

// handleSelectionScrollTick scrolls one row toward the held edge and extends
// the selection to the newly revealed row, rescheduling while the drag stays
// at the edge. Terminals send no motion while the pointer is still, so the
// tick is what keeps a held drag scrolling.
func (m *model) handleSelectionScrollTick(msg selectionScrollTickMsg) tea.Cmd {
	if msg.gen != m.sel.tickGen || !m.sel.dragging || m.sel.edgeDir == 0 {
		m.sel.ticking = false
		return nil
	}
	relY := 0
	if m.sel.edgeDir < 0 {
		m.viewport.ScrollUp(1)
	} else {
		m.viewport.ScrollDown(1)
		relY = m.viewport.Height - 1
	}
	m.updateSelectionHead(m.sel.lastX, relY)
	return selectionScrollTick(msg.gen)
}

// endSelection finishes the drag: a click copies nothing, a drag copies the
// selected source text. The highlight is cleared and the copy result reported
// in the status row.
func (m *model) endSelection(x, relY int) tea.Cmd {
	if m.sel.moved {
		m.sel.head = m.selPosAt(x, relY)
	}
	moved := m.sel.moved
	text := extractSelection(m.scrollLines, m.sel.anchor, m.sel.head)
	m.clearSelection()
	m.refreshSelection()
	m.followBottom = m.viewport.AtBottom()
	if !moved || strings.TrimSpace(text) == "" {
		return nil
	}
	return m.copyToClipboardMode(text, true)
}
