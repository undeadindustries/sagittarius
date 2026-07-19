package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapTextBreaksLongLines(t *testing.T) {
	t.Parallel()
	const width = 40
	msg := "This is a deliberately long error message that should wrap cleanly at word boundaries instead of running off the screen edge."
	wrapped := WrapText(msg, width)
	for _, line := range strings.Split(wrapped, "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
}

func TestWrapTextWithANSI(t *testing.T) {
	t.Parallel()
	const width = 10
	msg := "\x1b[31mThis is a red message that should wrap cleanly\x1b[0m"
	wrapped := WrapText(msg, width)
	for _, line := range strings.Split(wrapped, "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
}

func TestWrapTextWithWideCharacters(t *testing.T) {
	t.Parallel()
	const width = 10
	msg := "你好世界这是一个长句子"
	wrapped := WrapText(msg, width)
	for _, line := range strings.Split(wrapped, "\n") {
		if lipgloss.Width(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
}
