package overlay

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// ansiColorCode matches an SGR color-setting escape sequence (foreground,
// background, or truecolor), mirroring the bubbletea package's helper of the
// same name. Forcing lipgloss.SetColorProfile(termenv.TrueColor) makes the
// colored-theme assertions deterministic regardless of whether the test
// binary's stdout is a TTY.
var ansiColorCode = regexp.MustCompile(`\x1b\[[0-9;]*(?:38|48|3[0-9]|4[0-9])[;m]`)

func TestHintsGradientsSingleLine(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	th := theme.Default()
	got := Hints(th, "Enter select • Esc close")
	if !ansiColorCode.MatchString(got) {
		t.Errorf("expected color ANSI in gradiented hint, got %q", got)
	}
	if stripANSI(got) != "Enter select • Esc close" {
		t.Errorf("expected original text preserved, got %q", stripANSI(got))
	}
}

func TestHintsGradientsEachLineIndependently(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	th := theme.Default()
	got := Hints(th, "line one\nline two")
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if !ansiColorCode.MatchString(line) {
			t.Errorf("line %d missing gradient ANSI: %q", i, line)
		}
	}
}

func TestHintsEmptyString(t *testing.T) {
	th := theme.Default()
	if got := Hints(th, ""); got != "" {
		t.Errorf("expected empty string passthrough, got %q", got)
	}
}

func TestHintsGreyscaleFallback(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	th := theme.Greyscale()
	got := Hints(th, "Enter select • Esc close")
	if ansiColorCode.MatchString(got) {
		t.Errorf("expected no color ANSI on greyscale theme, got %q", got)
	}
	if stripANSI(got) != "Enter select • Esc close" {
		t.Errorf("expected text preserved on greyscale theme, got %q", stripANSI(got))
	}
}

func TestTitleGradientsAndFallsBack(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	th := theme.Default()
	got := Title(th, "Providers")
	if !ansiColorCode.MatchString(got) {
		t.Errorf("expected color ANSI in gradiented title, got %q", got)
	}
	if stripANSI(got) != "Providers" {
		t.Errorf("expected original text preserved, got %q", stripANSI(got))
	}

	grey := theme.Greyscale()
	gotGrey := Title(grey, "Providers")
	if ansiColorCode.MatchString(gotGrey) {
		t.Errorf("expected no color ANSI on greyscale theme, got %q", gotGrey)
	}
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
