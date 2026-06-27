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

	if m.showWorkingIndicator() || m.workingRows() != 0 {
		t.Fatalf("idle model should not show the working line")
	}

	m.busy = true

	// A running tool card carries its own spinner in the header, so the
	// standalone working line is suppressed while the card is active.
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", ToolCallID: "c1"})
	if m.showWorkingIndicator() || m.workingRows() != 0 {
		t.Fatalf("running tool card should suppress the working line: show=%v rows=%d", m.showWorkingIndicator(), m.workingRows())
	}

	// After a tool result no card is active: the model is queried again and the
	// working line returns showing Working… (waiting on the next model output).
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "write_file", Text: "ok", ToolCallID: "c1"})
	if !m.showWorkingIndicator() || m.workingLabel != "Working…" {
		t.Fatalf("tool result: show=%v label=%q", m.showWorkingIndicator(), m.workingLabel)
	}

	// End of turn hides the indicator.
	m.handleStream(ui.StreamEvent{Type: ui.StreamDone})
	if m.showWorkingIndicator() || m.busy {
		t.Fatalf("done: show=%v busy=%v", m.showWorkingIndicator(), m.busy)
	}
}

// TestWorkingHiddenWhileTextStreams asserts the spinner row is suppressed while
// assistant text is visibly streaming into the scrollback: the words are the
// feedback, so a "Working…" line would be redundant noise. Once the response
// block is closed (e.g. a tool starts, or the turn ends) the suppression lifts.
func TestWorkingHiddenWhileTextStreams(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.busy = true

	// Before any output the model is waiting on the provider: show Working….
	if !m.showWorkingIndicator() {
		t.Fatal("busy turn with no output yet should show the working line")
	}

	// Text streaming opens a response block (openResponseIdx >= 0): hide it.
	m.handleStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "Let me fix it:"})
	if m.showWorkingIndicator() {
		t.Fatal("working line should be hidden while assistant text is streaming")
	}

	// A tool start closes the response block; the working line is suppressed by
	// the running card instead, but openResponseIdx no longer gates it.
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "read_file", ToolCallID: "c1"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "read_file", Text: "ok", ToolCallID: "c1"})
	if !m.showWorkingIndicator() {
		t.Fatal("after the tool result the working line should return (waiting on the model)")
	}
}

// TestWorkingReturnsAfterStreamingPause asserts the spinner reappears during the
// silent wait for the model's next action. While text deltas flow it stays
// hidden (the words are the feedback), but once they pause beyond
// streamingSpinnerGrace — with the response block still open — the working line
// returns, so "Queue a message" is never shown without an activity cue.
func TestWorkingReturnsAfterStreamingPause(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	m.busy = true

	m.handleStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "Now let me create the file:"})
	if m.showWorkingIndicator() {
		t.Fatal("spinner should be hidden while a text delta just arrived")
	}
	if m.openResponseIdx < 0 {
		t.Fatal("response block should be open after a text delta")
	}

	// Simulate streaming pausing: backdate the last delta beyond the grace.
	m.lastTextDeltaAt = time.Now().Add(-2 * streamingSpinnerGrace)
	if !m.showWorkingIndicator() {
		t.Fatal("spinner should return once streaming pauses beyond the grace, even with the response block still open")
	}
}

func TestRenderWorkingLine(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))
	line := stripANSI(renderWorkingLine(m.spin, "Working…", m.th, 80))
	if !strings.Contains(line, "Working…") {
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
