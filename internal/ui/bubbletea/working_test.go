package bubbletea

import (
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

func TestWorkingIndicatorVisibility(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24

	if m.working || m.workingRows() != 0 {
		t.Fatalf("idle model should not show the working line")
	}

	// A tool round shows the indicator with the tool name.
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file"})
	if !m.working || m.workingLabel != "Running write_file" || m.workingRows() != 1 {
		t.Fatalf("tool start: working=%v label=%q rows=%d", m.working, m.workingLabel, m.workingRows())
	}

	// Streaming visible text hides the indicator (the response block takes over).
	m.handleStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "hi"})
	if m.working {
		t.Fatal("text delta should hide the working line")
	}

	// After a tool result the model is queried again: back to Thinking….
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "write_file", Text: "ok"})
	if !m.working || m.workingLabel != "Thinking…" {
		t.Fatalf("tool result: working=%v label=%q", m.working, m.workingLabel)
	}

	// End of turn hides the indicator.
	m.handleStream(ui.StreamEvent{Type: ui.StreamDone})
	if m.working || m.busy {
		t.Fatalf("done: working=%v busy=%v", m.working, m.busy)
	}
}

func TestRenderWorkingLine(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	line := stripANSI(renderWorkingLine(m.spin, "Thinking…", m.th, 80))
	if !strings.Contains(line, "Thinking…") {
		t.Fatalf("working line missing label: %q", line)
	}
	// MiniDot's first frame is ⠋; the spinner must render a glyph, not "(error)".
	if !strings.Contains(line, "⠋") {
		t.Fatalf("working line missing spinner glyph: %q", line)
	}
}

func TestSpinnerColorCycles(t *testing.T) {
	t.Parallel()
	grad := theme.Default().SpinnerGradient
	if len(grad) < 2 {
		t.Fatalf("default theme should define a multi-stop spinner gradient, got %d", len(grad))
	}

	base := time.Unix(0, 0)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		c, ok := spinnerColorAt(grad, base.Add(time.Duration(i)*colorCycleDuration/8))
		if !ok {
			t.Fatal("expected a color for a non-empty gradient")
		}
		seen[string(c)] = true
	}
	if len(seen) < 4 {
		t.Fatalf("spinner color should change over time, got only %d distinct colors", len(seen))
	}
}

func TestSpinnerColorGreyscaleStatic(t *testing.T) {
	t.Parallel()
	if _, ok := spinnerColorAt(theme.Greyscale().SpinnerGradient, time.Now()); ok {
		t.Fatal("greyscale theme should not produce a cycling spinner color")
	}
}

func TestLerpHex(t *testing.T) {
	t.Parallel()
	if got := lerpHex("#000000", "#FFFFFF", 0); got != "#000000" {
		t.Fatalf("lerp at 0 = %q, want #000000", got)
	}
	if got := lerpHex("#000000", "#FFFFFF", 1); got != "#FFFFFF" {
		t.Fatalf("lerp at 1 = %q, want #FFFFFF", got)
	}
	if got := lerpHex("#000000", "#FFFFFF", 0.5); got != "#808080" {
		t.Fatalf("lerp at 0.5 = %q, want #808080", got)
	}
}
