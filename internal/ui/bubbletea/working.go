package bubbletea

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// colorCycleDuration is how long the working spinner takes to traverse its full
// color gradient once, matching gemini-cli's ~4s GeminiSpinner cycle.
const colorCycleDuration = 4 * time.Second

// newWorkingSpinner builds the Braille-dot spinner used for the working/thinking
// indicator. MiniDot matches gemini-cli's "dots" frames (⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏), so the
// motion cue is familiar.
func newWorkingSpinner(th theme.Theme) spinner.Model {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	s.Style = th.Accent
	return s
}

// renderWorkingLine renders the animated working indicator shown above the input
// while the agent is busy (e.g. "⠹ Thinking…"). label is the current activity
// (e.g. "Thinking…" or "Running write_file").
func renderWorkingLine(s spinner.Model, label string, th theme.Theme, width int) string {
	line := s.View() + " " + th.Secondary.Render(label)
	return lipgloss.NewStyle().Width(max(width, 1)).Render(line)
}

// applySpinnerColor restyles the spinner glyph to the gradient color for the
// current wall-clock instant, producing the smooth color cycle seen while the
// agent is working. On greyscale themes (empty gradient) the spinner keeps its
// static accent style.
func applySpinnerColor(s *spinner.Model, th theme.Theme) {
	if c, ok := spinnerColorAt(th.SpinnerGradient, time.Now()); ok {
		s.Style = lipgloss.NewStyle().Foreground(c)
	}
}

// spinnerColorAt returns the interpolated gradient color for instant t. The
// gradient is treated as a loop (the last stop blends back to the first). It
// reports ok=false when the gradient is empty so callers can leave the spinner
// style untouched.
func spinnerColorAt(grad []string, t time.Time) (lipgloss.Color, bool) {
	n := len(grad)
	switch n {
	case 0:
		return "", false
	case 1:
		return lipgloss.Color(grad[0]), true
	}
	elapsed := t.UnixNano() % int64(colorCycleDuration)
	if elapsed < 0 {
		elapsed += int64(colorCycleDuration)
	}
	pos := float64(elapsed) / float64(colorCycleDuration) * float64(n)
	idx := int(pos) % n
	frac := pos - math.Floor(pos)
	return lipgloss.Color(lerpHex(grad[idx], grad[(idx+1)%n], frac)), true
}

// lerpHex linearly interpolates between two "#RRGGBB" colors, returning a new
// "#RRGGBB" string. t is clamped to [0,1].
func lerpHex(a, b string, t float64) string {
	t = math.Max(0, math.Min(1, t))
	ar, ag, ab := parseHex(a)
	br, bg, bb := parseHex(b)
	lerp := func(x, y int) int { return int(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return fmt.Sprintf("#%02X%02X%02X", lerp(ar, br), lerp(ag, bg), lerp(ab, bb))
}

// parseHex parses a "#RRGGBB" string into its red, green, and blue components,
// falling back to white on malformed input.
func parseHex(s string) (int, int, int) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 255, 255, 255
	}
	r, err1 := strconv.ParseInt(s[0:2], 16, 0)
	g, err2 := strconv.ParseInt(s[2:4], 16, 0)
	b, err3 := strconv.ParseInt(s[4:6], 16, 0)
	if err1 != nil || err2 != nil || err3 != nil {
		return 255, 255, 255
	}
	return int(r), int(g), int(b)
}
