package theme

import (
	"regexp"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ansiColor matches SGR sequences that set a foreground or background color
// (codes 38/48 for 256/truecolor, or the 30-49 basic color range). Attribute-
// only sequences (bold=1, faint=2, reverse=7, underline=4) are allowed in
// greyscale and must not match.
var ansiColor = regexp.MustCompile(`\x1b\[[0-9;]*(?:38|48|3[0-9]|4[0-9])[;m]`)

func TestGreyscaleEmitsNoColor(t *testing.T) {
	// Force color rendering on so a leaking color would actually be emitted.
	lipgloss.SetColorProfile(termenv.TrueColor)

	th := Greyscale()
	if th.Colored {
		t.Fatal("greyscale theme should report Colored=false")
	}

	styles := map[string]lipgloss.Style{
		"title":     th.Title,
		"primary":   th.Primary,
		"secondary": th.Secondary,
		"accent":    th.Accent,
		"response":  th.Response,
		"link":      th.Link,
		"dim":       th.Dim,
		"error":     th.Error,
		"warning":   th.Warning,
		"success":   th.Success,
		"selected":  th.Selected,
	}
	for name, st := range styles {
		out := st.Render("sample")
		if ansiColor.MatchString(out) {
			t.Errorf("greyscale style %q emitted a color code: %q", name, out)
		}
	}
}

func TestDefaultEmitsColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	th := Default()
	if !th.Colored {
		t.Fatal("default theme should report Colored=true")
	}
	if !ansiColor.MatchString(th.Accent.Render("x")) {
		t.Error("default accent should emit a color code")
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		noColor bool
		want    string
	}{
		{"", false, "default"},
		{"default", false, "default"},
		{"greyscale", false, "greyscale"},
		{"grayscale", false, "greyscale"},
		{"GREYSCALE", false, "greyscale"},
		{"default", true, "greyscale"}, // NO_COLOR overrides a named theme
		{"", true, "greyscale"},
	}
	for _, tc := range tests {
		if got := Resolve(tc.name, tc.noColor).Name; got != tc.want {
			t.Errorf("Resolve(%q, %v) = %q, want %q", tc.name, tc.noColor, got, tc.want)
		}
	}
}

func TestResolveTrimsAndLowercases(t *testing.T) {
	if got := Resolve("  Mono  ", false).Name; got != "greyscale" {
		t.Errorf("Resolve(\"  Mono  \") = %q, want greyscale", got)
	}
}
