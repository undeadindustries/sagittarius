package theme

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// luma is the Rec. 709 relative luminance of a "#RRGGBB" theme color on a
// 0..1 scale, enough to order border colors by how much they draw the eye.
// It parses the hex itself: lipgloss's RGBA goes through the renderer's color
// profile, which is no-color in a test process.
func luma(t *testing.T, c lipgloss.TerminalColor) float64 {
	t.Helper()
	hex, ok := c.(lipgloss.Color)
	if !ok {
		t.Fatalf("border %#v is not a hex lipgloss.Color", c)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(string(hex), "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("parse %q: %v", hex, err)
	}
	const maxChannel = 0xff
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / maxChannel
}

// TestCardBordersStayBelowFocus guards the card-state palette: the focus
// border is the only one asking for input, so every other card border must be
// dimmer, and a running card must stand out from a settled one.
func TestCardBordersStayBelowFocus(t *testing.T) {
	t.Parallel()
	th := Default()
	focus := luma(t, th.FocusBorderColor)
	for name, c := range map[string]lipgloss.TerminalColor{
		"settled": th.BorderColor,
		"running": th.RunningBorderColor,
		"error":   th.ErrorBorderColor,
	} {
		if got := luma(t, c); got >= focus {
			t.Errorf("%s border luma %.3f is not below focus %.3f", name, got, focus)
		}
	}
	if luma(t, th.RunningBorderColor) <= luma(t, th.BorderColor) {
		t.Error("running border is not brighter than the settled border")
	}

	grey := Greyscale()
	for name, c := range map[string]lipgloss.TerminalColor{
		"settled": grey.BorderColor,
		"running": grey.RunningBorderColor,
		"error":   grey.ErrorBorderColor,
		"focus":   grey.FocusBorderColor,
	} {
		if _, ok := c.(lipgloss.NoColor); !ok {
			t.Errorf("greyscale %s border = %#v, want NoColor", name, c)
		}
	}
}

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
