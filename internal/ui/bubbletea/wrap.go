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

// sanitizeDisplayText normalizes text for display inside borders by stripping
// raw carriage returns, expanding tabs to 8 spaces, and stripping ANSI so
// unprintable/color sequences don't corrupt the box layout width.
func sanitizeDisplayText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = ansi.Strip(s)

	if !strings.Contains(s, "\t") {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "\t") {
			continue
		}
		var b strings.Builder
		col := 0
		for _, r := range line {
			if r == '\t' {
				spaces := 8 - (col % 8)
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
			} else {
				b.WriteRune(r)
				col += lipgloss.Width(string(r))
			}
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}
