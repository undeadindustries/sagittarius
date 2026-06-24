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

	m.toggleThinking()
	if !m.showThinking || !m.thinkingToggled {
		t.Fatalf("toggle on: show=%v toggled=%v", m.showThinking, m.thinkingToggled)
	}
	if !app.lastSet || app.calls != 1 {
		t.Fatalf("toggle on should persist true once: set=%v calls=%d", app.lastSet, app.calls)
	}

	m.toggleThinking()
	if m.showThinking {
		t.Fatal("second toggle should turn the box off")
	}
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
