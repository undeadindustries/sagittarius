package bubbletea

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestThinkingBudgetNoticeRetiresThinkingState is the regression for the
// display contradicting itself: the reasoning buffer is what drives both the
// "Thinking…" label and the thinking box, so a notice saying thinking has been
// stopped has to retire that buffer or the UI keeps claiming the model is
// still thinking.
func TestThinkingBudgetNoticeRetiresThinkingState(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.showThinking = true
	m.thinkingToggled = true
	m.busy = true

	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "going in circles"})
	if m.workingLabel != "Thinking…" {
		t.Fatalf("precondition: label = %q, want Thinking…", m.workingLabel)
	}
	if !m.thinkingBoxVisible() {
		t.Fatal("precondition: thinking box should be visible")
	}

	const notice = "Thinking budget of 2048 tokens used up."
	m.handleStream(ui.StreamEvent{Type: ui.StreamThinkingBudget, Text: notice})

	if m.thinking != "" {
		t.Errorf("reasoning buffer = %q, want cleared", m.thinking)
	}
	if m.thinkingBoxVisible() {
		t.Error("thinking box should be hidden once the budget cut the reasoning")
	}
	if m.workingLabel == "Thinking…" {
		t.Error(`label still says "Thinking…" after the model was told to stop thinking`)
	}
	if !strings.Contains(stripANSI(m.renderScrollback(80)), "2048 tokens used up") {
		t.Error("the notice should be visible in the scrollback")
	}
}

// TestThinkingBudgetNoticeSurvivesHiddenThinkingBox covers the default
// configuration, where the box is off and only the label reveals the state.
func TestThinkingBudgetNoticeSurvivesHiddenThinkingBox(t *testing.T) {
	t.Parallel()

	m := newTestModel()
	m.busy = true
	m.handleStream(ui.StreamEvent{Type: ui.StreamReasoningDelta, Text: "hidden thoughts"})
	m.handleStream(ui.StreamEvent{Type: ui.StreamThinkingBudget, Text: "budget used up"})

	if m.thinking != "" {
		t.Errorf("reasoning buffer = %q, want cleared", m.thinking)
	}
	if m.workingLabel != "Working…" {
		t.Errorf("label = %q, want Working… once reasoning is retired", m.workingLabel)
	}
	if strings.Contains(stripANSI(m.renderScrollback(80)), "hidden thoughts") {
		t.Error("reasoning must never reach the scrollback")
	}
}
