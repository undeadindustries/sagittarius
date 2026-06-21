package bubbletea

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

var ansiColorCode = regexp.MustCompile(`\x1b\[[0-9;]*(?:38|48|3[0-9]|4[0-9])[;m]`)

func TestWelcomeShowsLogoAndTips(t *testing.T) {
	out := welcomeText(ui.Options{Version: "1.2.3"}, theme.Greyscale())
	if !strings.Contains(out, "S A G I T T A R I U S") {
		t.Errorf("welcome missing wordmark:\n%s", out)
	}
	if !strings.Contains(out, "1.2.3") {
		t.Errorf("welcome missing version:\n%s", out)
	}
	if !strings.Contains(out, "Tips for getting started") {
		t.Errorf("welcome missing tips:\n%s", out)
	}
	if !strings.Contains(out, "/providers") {
		t.Errorf("welcome missing provider tip:\n%s", out)
	}
}

func TestWelcomeHideBanner(t *testing.T) {
	out := welcomeText(ui.Options{Version: "1.0", HideBanner: true}, theme.Greyscale())
	if strings.Contains(out, "»»") || strings.Contains(out, "S A G I T") {
		t.Errorf("hideBanner should drop the ASCII logo:\n%s", out)
	}
	// A plain title still appears so the session is identifiable.
	if !strings.Contains(out, "Sagittarius") {
		t.Errorf("hideBanner should still show a plain title:\n%s", out)
	}
}

func TestWelcomeHideTips(t *testing.T) {
	out := welcomeText(ui.Options{HideTips: true}, theme.Greyscale())
	if strings.Contains(out, "Tips for getting started") {
		t.Errorf("hideTips should drop the tips block:\n%s", out)
	}
}

func TestWelcomeGreyscaleHasNoColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	out := welcomeText(ui.Options{Version: "1.0", Notice: "⚠ key missing"}, theme.Greyscale())
	if ansiColorCode.MatchString(out) {
		t.Errorf("greyscale welcome emitted color codes:\n%q", out)
	}
}
