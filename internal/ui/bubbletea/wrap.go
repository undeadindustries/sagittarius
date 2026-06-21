package bubbletea

import "github.com/undeadindustries/sagittarius/internal/ui"

// wrapText breaks long lines at spaces so viewport content is not clipped at
// the right edge. Existing newlines are preserved.
func wrapText(text string, width int) string {
	return ui.WrapText(text, width)
}
