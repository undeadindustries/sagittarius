package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestSetThemeGreyscaleClearsComposerColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := newModel(ui.Options{ThemeName: "default"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 40

	before := m.input.View()
	if !strings.Contains(before, "38;") && !strings.Contains(before, "48;") {
		t.Fatalf("default composer should be colored, got %q", before)
	}

	m.setTheme("greyscale")
	view := m.input.View()
	if strings.Contains(view, "38;") || strings.Contains(view, "48;") {
		t.Fatalf("greyscale composer still has color: %q", view)
	}
	if !strings.Contains(view, "\x1b[7") {
		t.Fatalf("greyscale prompt should be reverse video, got %q", view)
	}
	if !strings.Contains(view, "Agent>") {
		t.Fatalf("prompt missing, got %q", view)
	}
}

func TestSetThemeKeepsSpinnerTicking(t *testing.T) {
	m := newModel(ui.Options{ThemeName: "default"}, quitApp{}, NewTerminal(ui.Options{}))
	m.busy = true
	tick := m.spin.Tick()
	m.setTheme("greyscale")
	_, cmd := m.Update(tick)
	if cmd == nil {
		t.Fatal("spinner tick chain died after a theme switch")
	}
}
