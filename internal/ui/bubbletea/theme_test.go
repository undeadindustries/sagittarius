package bubbletea

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestStreamSetThemeSwitchesLive(t *testing.T) {
	m := newModel(ui.Options{}, quitApp{}, NewTerminal(ui.Options{}))
	if m.th.Name != "default" {
		t.Fatalf("initial theme = %q, want default", m.th.Name)
	}
	if !m.th.Colored {
		t.Fatal("default theme should be colored")
	}

	m.handleStream(ui.StreamEvent{Type: ui.StreamSetTheme, Text: "greyscale"})
	if m.th.Name != "greyscale" {
		t.Errorf("after switch theme = %q, want greyscale", m.th.Name)
	}
	if m.th.Colored {
		t.Error("greyscale theme should not be colored")
	}

	m.handleStream(ui.StreamEvent{Type: ui.StreamSetTheme, Text: "default"})
	if m.th.Name != "default" {
		t.Errorf("after switch back theme = %q, want default", m.th.Name)
	}
	if !m.th.Colored {
		t.Error("default theme should be colored")
	}
}
