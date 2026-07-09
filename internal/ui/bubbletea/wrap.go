package bubbletea

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// wrapText breaks long lines at spaces so viewport content is not clipped at
// the right edge. Existing newlines are preserved.
func wrapText(text string, width int) string {
	return ui.WrapText(text, width)
}

// padOrTruncate visual-pads a styled line with spaces to exactly width cells,
// and truncates safely with ansi.Truncate if it exceeds width.
func padOrTruncate(line string, width int) string {
	w := lipgloss.Width(line)
	if w < width {
		return line + strings.Repeat(" ", width-w)
	}
	if w > width {
		return ansi.Truncate(line, width, "")
	}
	return line
}
