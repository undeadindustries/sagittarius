package ui

import (
	"strings"
	"testing"
)

func TestWrapTextBreaksLongLines(t *testing.T) {
	t.Parallel()
	const width = 40
	msg := "This is a deliberately long error message that should wrap cleanly at word boundaries instead of running off the screen edge."
	wrapped := WrapText(msg, width)
	for _, line := range strings.Split(wrapped, "\n") {
		if len(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
}
