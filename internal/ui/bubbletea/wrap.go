package bubbletea

import "strings"

// wrapText breaks long lines at spaces so viewport content is not clipped at
// the right edge. Existing newlines are preserved.
func wrapText(text string, width int) string {
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
	if len(line) <= width {
		return []string{line}
	}

	var wrapped []string
	rest := line
	for len(rest) > width {
		cut := width
		if sp := strings.LastIndex(rest[:width], " "); sp > 0 {
			cut = sp
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
