package bubbletea

import "testing"

func TestWrapTextBreaksLongLines(t *testing.T) {
	t.Parallel()

	const width = 40
	msg := "Error: rebuild runner after provider switch: api key missing: set GEMINI_API_KEY or GOOGLE_API_KEY, or store a key with /auth <key>"
	wrapped := wrapText(msg, width)

	for _, line := range splitLines(wrapped) {
		if line == "" {
			continue
		}
		if len(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
	if wrapped == msg {
		t.Fatal("expected wrapping to produce multiple lines")
	}
}

func TestWrapTextPreservesShortLines(t *testing.T) {
	t.Parallel()
	got := wrapText("short line\n", 80)
	if got != "short line\n" {
		t.Fatalf("got %q", got)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
