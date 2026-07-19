package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// WrapText breaks long lines at spaces so content is not clipped at the right
// edge. Existing newlines are preserved. Used by TUI overlays and viewports.
func WrapText(text string, width int) string {
	if width <= 0 || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, wrapLine(line, width)...)
	}
	return strings.Join(out, "\n")
}

func wrapLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	if lipgloss.Width(line) <= width {
		return []string{line}
	}

	var wrapped []string
	rest := line
	for lipgloss.Width(rest) > width {
		// Find the longest prefix of rest that fits in width cells.
		low, high := 0, len(rest)
		cut := 0
		for low <= high {
			mid := low + (high-low)/2
			// Ensure mid is on a rune boundary
			for mid < len(rest) && !utf8.RuneStart(rest[mid]) {
				mid++
			}
			if lipgloss.Width(rest[:mid]) <= width {
				cut = mid
				low = mid + 1
			} else {
				high = mid - 1
				// Adjust high to rune boundary
				for high > 0 && !utf8.RuneStart(rest[high]) {
					high--
				}
			}
		}

		// Try to break at the last space within the fit prefix.
		if sp := strings.LastIndex(rest[:cut], " "); sp > 0 {
			cut = sp
		}

		// If cut is 0 but we have text, force progress to avoid infinite loop.
		if cut == 0 && len(rest) > 0 {
			// Find the next rune boundary to make minimal progress
			cut = 1
			for cut < len(rest) && !utf8.RuneStart(rest[cut]) {
				cut++
			}
		}

		wrapped = append(wrapped, strings.TrimSpace(rest[:cut]))
		rest = strings.TrimSpace(rest[cut:])
		if rest == "" {
			break
		}
	}
	if rest != "" {
		wrapped = append(wrapped, rest)
	}
	return wrapped
}
