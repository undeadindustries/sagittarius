package overlay

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

const (
	// frameRows is the rounded border's top and bottom line.
	frameRows = 2
	// framePaddingCols is Frame's horizontal padding, inside ContentWidth.
	framePaddingCols = 2
	// scrollMarkerRows reserves the "more above" and "more below" lines.
	scrollMarkerRows = 2
)

// Box is the terminal area a dialog must fit. Width and Height are the full
// terminal size; zero means the size is not known yet and nothing is clipped.
type Box struct {
	Width, Height int
	// MinWidth is the dialog's minimum content width (DefaultMinWidth if zero).
	MinWidth int
}

// List is a dialog body split into its fixed parts and its scrollable rows.
// Head and Foot are rendered whole; Rows are windowed around Cursor. Rows are
// already rendered (selection marker included) and should be one line each.
type List struct {
	Head, Foot string
	Rows       []string
	Cursor     int
}

func (b Box) minWidth() int {
	if b.MinWidth > 0 {
		return b.MinWidth
	}
	return DefaultMinWidth
}

// textWidth is the column count a body line gets inside Frame, or 0 when the
// width is unknown.
func (b Box) textWidth() int {
	if b.Width <= 0 {
		return 0
	}
	return max(ContentWidth(b.Width, b.minWidth())-framePaddingCols, 1)
}

// FitFrame renders l inside the standard frame so the whole box fits the
// terminal height. Bubble Tea drops the top lines of a frame taller than the
// terminal, which hides a dialog's title and first rows; windowing the rows
// here keeps the title, the hints, and the selected row on screen instead.
func FitFrame(th theme.Theme, b Box, l List) string {
	return Frame(th, b.Width, b.minWidth(), FitList(th, b, l))
}

// FitList returns the frame body for l: the head, a window of rows that keeps
// the cursor visible, "… N more" markers for the rows left out, and the foot.
// The window is centered on the cursor and holds no state, so a dialog's View
// stays pure and a resize needs no bookkeeping.
func FitList(th theme.Theme, b Box, l List) string {
	tw := b.textWidth()
	rows := l.Rows
	if tw > 0 {
		rows = make([]string, len(l.Rows))
		for i, r := range l.Rows {
			rows[i] = ansi.Truncate(r, tw, "…")
		}
	}
	start, end := 0, len(rows)
	if b.Height > 0 {
		budget := b.Height - frameRows - wrappedHeight(l.Head, tw) - wrappedHeight(l.Foot, tw)
		start, end = listWindow(len(rows), l.Cursor, budget)
	}

	parts := make([]string, 0, end-start+4)
	if l.Head != "" {
		parts = append(parts, l.Head)
	}
	if start > 0 {
		parts = append(parts, th.Dim.Render(fmt.Sprintf("  … %d more above", start)))
	}
	parts = append(parts, rows[start:end]...)
	if end < len(rows) {
		parts = append(parts, th.Dim.Render(fmt.Sprintf("  … %d more below", len(rows)-end)))
	}
	if l.Foot != "" {
		parts = append(parts, l.Foot)
	}
	return strings.Join(parts, "\n")
}

// Foot builds a List.Foot from a dialog's info line, error line, and key
// hints: a blank line after the rows, then each non-empty part separated by a
// blank line. info and errMsg are rendered dim and as an error respectively.
func Foot(th theme.Theme, info, errMsg, hints string) string {
	parts := make([]string, 0, 3)
	if info != "" {
		parts = append(parts, th.Dim.Render(info))
	}
	if errMsg != "" {
		parts = append(parts, th.Error.Render("✗ "+errMsg))
	}
	if hints != "" {
		parts = append(parts, Hints(th, hints))
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n" + strings.Join(parts, "\n\n")
}

// listWindow returns the [start, end) rows to show for a row budget. When the
// list does not fit, two lines go to the scroll markers and at least one row
// (the cursor's) is always shown.
func listWindow(total, cursor, budget int) (start, end int) {
	if total <= budget {
		return 0, total
	}
	visible := max(budget-scrollMarkerRows, 1)
	cursor = min(max(cursor, 0), total-1)
	start = min(max(cursor-visible/2, 0), total-visible)
	return start, start + visible
}

// wrappedHeight is how many lines s occupies once Frame wraps it to width.
func wrappedHeight(s string, width int) int {
	if s == "" {
		return 0
	}
	if width <= 0 {
		return lipgloss.Height(s)
	}
	return lipgloss.Height(lipgloss.NewStyle().Width(width).Render(s))
}

// FitHeight clips a rendered view to height lines, keeping the top so a
// dialog's title stays visible, and replaces the last line with a notice. It
// is the safety net for content FitList does not window (long text, edit
// screens); a view that already fits is returned unchanged.
func FitHeight(th theme.Theme, view string, height int) string {
	if height <= 0 || strings.Count(view, "\n") < height {
		return view
	}
	lines := strings.Split(view, "\n")
	if height == 1 {
		return lines[0]
	}
	kept := append(lines[:height-1:height-1], th.Dim.Render("… enlarge the terminal to see more"))
	return strings.Join(kept, "\n")
}
