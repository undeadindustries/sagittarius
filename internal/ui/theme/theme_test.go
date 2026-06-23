package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestGradientText(t *testing.T) {
	th := Default()
	style := lipgloss.NewStyle().Bold(true)

	// Valid stops
	stops := []string{"#FF0000", "#00FF00", "#0000FF"}
	res := th.GradientText("Hello", style, stops)

	if !strings.Contains(res, "H") || !strings.Contains(res, "o") {
		t.Errorf("expected string to contain characters")
	}

	// Should not gradient if not colored
	thGrey := Greyscale()
	resGrey := thGrey.GradientText("Hello", style, stops)
	if strings.Contains(resGrey, "38;2") {
		t.Errorf("expected no truecolor ansi in greyscale, got %q", resGrey)
	}

	// Should fall back gracefully with < 2 stops
	res1Stop := th.GradientText("Hello", style, []string{"#FF0000"})
	if strings.Contains(res1Stop, "38;2") {
		t.Errorf("expected no gradient with 1 stop, got %q", res1Stop)
	}
}
