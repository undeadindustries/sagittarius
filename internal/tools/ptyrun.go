package tools

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// renderEmulator converts the current emulator screen to a trimmed string.
// It splits the rendered lines and strips empty trailing lines so the UI
// doesn't show a huge blank box when the screen is mostly empty.
func renderEmulator(term vt.Terminal) string {
	r := term.Render()
	lines := strings.Split(r, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if ansi.Strip(lines[i]) != "" {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	return ""
}
