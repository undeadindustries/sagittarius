// Package overlay holds shared rendering primitives for the TUI dialog overlays:
// the rounded frame, the content-width clamp, and selectable list rows. Before
// this, every *dialog package copy-pasted identical boxStyle/contentWidth/renderRow
// helpers; centralizing them keeps the lipgloss usage in one charm-owning leaf
// (alongside theme and scopedialog) and guarantees the overlays render identically.
package overlay

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// DefaultMinWidth is the minimum inner content width used by most overlays.
const DefaultMinWidth = 20

// ContentWidth returns the inner content width for a terminal of the given width,
// clamped to min. The 4-column inset accounts for the rounded border (2 cols) plus
// the horizontal padding (2 cols).
func ContentWidth(width, min int) int {
	if w := width - 4; w >= min {
		return w
	}
	return min
}

// Frame wraps body in the standard rounded overlay border, using the theme's focus
// color when the theme is colored. When width > 0 the box is sized to
// ContentWidth(width, min); otherwise it renders at its natural width (the pre-layout
// state before the first WindowSizeMsg).
func Frame(th theme.Theme, width, min int, body string) string {
	s := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if th.Colored {
		s = s.BorderForeground(th.FocusBorderColor)
	}
	if width > 0 {
		s = s.Width(ContentWidth(width, min))
	}
	return s.Render(body)
}

// Row renders a selectable list row: an accented "> " prefix when selected, a
// two-space indent otherwise.
func Row(th theme.Theme, label string, selected bool) string {
	if selected {
		return th.Accent.Render("> " + label)
	}
	return "  " + label
}
