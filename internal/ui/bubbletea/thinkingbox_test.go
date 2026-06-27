package bubbletea

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// thinkingApp implements ui.App and ui.ThinkingController, recording the last
// persisted visibility so the Ctrl+T toggle can be asserted.
type thinkingApp struct {
	lastSet bool
	calls   int
}

func (*thinkingApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent)
	close(ch)
	return ch, nil
}

func (a *thinkingApp) SetShowThinking(on bool) error {
	a.lastSet = on
	a.calls++
	return nil
}

func TestThinkingBoxHiddenByDefault(t *testing.T) {
	t.Parallel()
	m := newTestModel() // quitApp: not a ComposerStatusProvider, showThinking off
	m.busy = true
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "considering options"})
	if m.thinking != "considering options" {
		t.Fatalf("reasoning should accumulate even while hidden, got %q", m.thinking)
	}
	if m.thinkingBoxVisible() || m.thinkingBoxRows() != 0 {
		t.Fatal("thinking box should be hidden when showThinking is off")
	}
	if m.renderThinkingBox() != "" {
		t.Fatal("hidden box should render empty")
	}
}

// TestThinkingBoxHiddenUntilReasoning asserts the box does NOT appear merely
// because a turn is busy with showThinking enabled: it stays hidden (and the
// "Working…" line shows instead) until the model actually streams reasoning
// tokens. This prevents the false "Thinking" cue during context prep / network
// waits / providers that send no reasoning.
func TestThinkingBoxHiddenUntilReasoning(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.showThinking = true
	m.thinkingToggled = true
	m.busy = true

	if m.thinkingBoxVisible() {
		t.Fatal("box should be hidden while busy before any reasoning tokens arrive")
	}
	if !m.showWorkingIndicator() {
		t.Fatal("the Working… line should show while waiting, before reasoning arrives")
	}

	// Once reasoning streams in, the box appears and the working line yields.
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "considering options"})
	if !m.thinkingBoxVisible() {
		t.Fatal("box should become visible once reasoning tokens arrive")
	}
	out := stripANSI(m.renderThinkingBox())
	if !strings.Contains(out, "Thinking") || !strings.Contains(out, "considering options") {
		t.Fatalf("thinking box missing label/reasoning:\n%s", out)
	}
	if m.showWorkingIndicator() {
		t.Fatal("standalone working line should be hidden while the thinking box is shown")
	}
}

func TestThinkingBoxShowsWhenEnabled(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.showThinking = true
	m.thinkingToggled = true
	m.busy = true
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "step one then step two"})

	if !m.thinkingBoxVisible() {
		t.Fatal("box should be visible when enabled with buffered reasoning")
	}
	if got := m.thinkingBoxRows(); got != thinkingBoxInnerRows+2 {
		t.Fatalf("thinkingBoxRows = %d, want %d", got, thinkingBoxInnerRows+2)
	}
	out := stripANSI(m.renderThinkingBox())
	for _, want := range []string{"Thinking", "step one"} {
		if !strings.Contains(out, want) {
			t.Errorf("thinking box missing %q\n%s", want, out)
		}
	}
}

func TestThinkingBufferClearsOnDone(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.showThinking = true
	m.thinkingToggled = true
	m.busy = true
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "abc"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamDone})
	if m.thinking != "" {
		t.Fatalf("reasoning buffer should clear at StreamDone, got %q", m.thinking)
	}
	if m.thinkingBoxVisible() {
		t.Fatal("box should auto-hide once the turn is done")
	}
}

// TestThinkingBoxClearsWhenModelActs asserts the box is retired the moment the
// model stops reasoning for a step and starts acting — on the first tool start
// or the first answer-text delta — instead of lingering (with a frozen reasoning
// tail and an animating border spinner) through tool execution until StreamDone.
func TestThinkingBoxClearsWhenModelActs(t *testing.T) {
	t.Parallel()

	// (1) A tool start retires the box.
	m := newTestModel()
	m.showThinking = true
	m.thinkingToggled = true
	m.busy = true
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "plan the write"})
	if !m.thinkingBoxVisible() {
		t.Fatal("box should show while reasoning streams")
	}
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "write_file", ToolCallID: "c1"})
	if m.thinking != "" {
		t.Fatalf("tool start should clear the reasoning buffer, got %q", m.thinking)
	}
	if m.thinkingBoxVisible() {
		t.Fatal("box should be hidden once a tool starts")
	}

	// (2) Answer text retires the box.
	m2 := newTestModel()
	m2.showThinking = true
	m2.thinkingToggled = true
	m2.busy = true
	m2.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "compose answer"})
	if !m2.thinkingBoxVisible() {
		t.Fatal("box should show while reasoning streams")
	}
	m2.handleStream(ui.StreamEvent{Type: ui.StreamTextDelta, Text: "Here is the answer"})
	if m2.thinking != "" {
		t.Fatalf("answer text should clear the reasoning buffer, got %q", m2.thinking)
	}
	if m2.thinkingBoxVisible() {
		t.Fatal("box should be hidden once answer text streams")
	}
}

func TestReasoningNeverEntersScrollback(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	before := len(m.blocks)
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "secret thoughts"})
	if len(m.blocks) != before {
		t.Fatal("reasoning must not create scrollback blocks")
	}
	if strings.Contains(stripANSI(m.renderScrollback(80)), "secret thoughts") {
		t.Fatal("reasoning leaked into the scrollback")
	}
}

func TestToggleThinkingFlipsAndPersists(t *testing.T) {
	t.Parallel()
	app := &thinkingApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width, m.height = 80, 24

	// The visual flip is synchronous; persistence runs in the returned Cmd so the
	// disk write never blocks the Update goroutine. Execute the Cmd to drive it.
	_, cmd := m.toggleThinking()
	if !m.showThinking || !m.thinkingToggled {
		t.Fatalf("toggle on: show=%v toggled=%v", m.showThinking, m.thinkingToggled)
	}
	if cmd == nil {
		t.Fatal("toggleThinking should return a persistence command")
	}
	cmd()
	if !app.lastSet || app.calls != 1 {
		t.Fatalf("toggle on should persist true once: set=%v calls=%d", app.lastSet, app.calls)
	}

	_, cmd = m.toggleThinking()
	if m.showThinking {
		t.Fatal("second toggle should turn the box off")
	}
	if cmd == nil {
		t.Fatal("toggleThinking should return a persistence command")
	}
	cmd()
	if app.lastSet || app.calls != 2 {
		t.Fatalf("toggle off should persist false: set=%v calls=%d", app.lastSet, app.calls)
	}
}

func TestEffectiveShowThinkingFromComposerStatus(t *testing.T) {
	t.Parallel()
	app := statusApp{cs: ui.ComposerStatus{ShowThinking: true}}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width, m.height = 80, 24

	if !m.effectiveShowThinking() {
		t.Fatal("resolved per-model showThinking should make the box effective without a toggle")
	}
	// A live Ctrl+T toggle overrides the resolved setting (statusApp does not
	// implement ThinkingController, so persistence is skipped harmlessly).
	m.toggleThinking()
	if m.effectiveShowThinking() {
		t.Fatal("user toggle should override the resolved setting")
	}
}
