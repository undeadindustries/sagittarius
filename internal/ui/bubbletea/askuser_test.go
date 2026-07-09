package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func startAskUserCard(m *model) chan ui.AskAnswer {
	reply := make(chan ui.AskAnswer, 1)
	m.handleStream(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "ask_user", ToolCallID: "c1"})
	m.handleStream(ui.StreamEvent{
		Type:        ui.StreamAskUser,
		ToolCallID:  "c1",
		AskQuestion: "Per-seat or flat-rate pricing?",
		AskOptions: []ui.AskOption{
			{Label: "Per-seat", Description: "scales with team size"},
			{Label: "Flat-rate", Description: "one price for everyone"},
		},
		AskRecommended: 1,
		AskReply:       reply,
	})
	return reply
}

// TestAskUserCardRendersQuestionAndOptions asserts the card shows the question,
// numbered options (recommended flagged), and the automatic "Other" row.
func TestAskUserCardRendersQuestionAndOptions(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	startAskUserCard(m)

	out := stripANSI(m.renderScrollback(80))
	for _, want := range []string{
		"Question",
		"Per-seat or flat-rate pricing?",
		"1 Per-seat",
		"(recommended)",
		"2 Flat-rate",
		"3 Other — type my own",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ask card missing %q:\n%s", want, out)
		}
	}
}

// TestAskUserDefaultHighlightIsRecommended asserts the picker starts with the
// recommended option highlighted, not always index 0.
func TestAskUserDefaultHighlightIsRecommended(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	startAskUserCard(m)

	if m.askChoice != 1 {
		t.Fatalf("askChoice = %d, want 1 (the recommended option)", m.askChoice)
	}
	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "› 2 Flat-rate") {
		t.Fatalf("expected the recommended row to be pre-highlighted:\n%s", out)
	}
}

// TestAskUserDigitKeySelectsOption asserts pressing a digit key answers
// immediately with that option, delivering it on the reply channel and
// resetting picker state.
func TestAskUserDigitKeySelectsOption(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := startAskUserCard(m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	got := <-reply
	if got.Index != 0 || got.Text != "Per-seat" {
		t.Fatalf("answer = %+v, want {0 Per-seat}", got)
	}
	if m.askReply != nil {
		t.Fatal("askReply should clear after answering")
	}
	if m.activeCard == nil || m.activeCard.phase != toolRunning {
		t.Fatalf("card phase = %v, want toolRunning after answering", m.activeCard)
	}
}

// TestAskUserArrowNavigationAndEnter asserts up/down move the highlight and
// Enter submits the highlighted option.
func TestAskUserArrowNavigationAndEnter(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := startAskUserCard(m)

	// Starts on the recommended option (index 1); move down twice: -> "Other"
	// (index 2), then wrap back to index 0 ("Per-seat").
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.askChoice != 0 {
		t.Fatalf("askChoice after two downs = %d, want 0 (wrapped)", m.askChoice)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	got := <-reply
	if got.Index != 0 || got.Text != "Per-seat" {
		t.Fatalf("answer = %+v, want {0 Per-seat}", got)
	}
}

// TestAskUserOtherSwitchesToFreeTextInput asserts selecting "Other" (the
// trailing menu row) does not answer immediately but instead focuses free-text
// capture in the main input box, and Enter there delivers the typed text.
func TestAskUserOtherSwitchesToFreeTextInput(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := startAskUserCard(m)

	// Digit 3 is the automatic "Other" row (2 options + 1).
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if !m.askOtherMode {
		t.Fatal("expected askOtherMode after selecting Other")
	}
	if m.askReply == nil {
		t.Fatal("askReply should still be pending while typing the free-text answer")
	}

	out := stripANSI(m.renderScrollback(80))
	if !strings.Contains(out, "Type your answer") {
		t.Fatalf("expected a free-text hint in the card:\n%s", out)
	}

	for _, r := range "Usage-based" {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	got := <-reply
	if got.Index != -1 || got.Text != "Usage-based" {
		t.Fatalf("answer = %+v, want {-1 Usage-based}", got)
	}
	if m.askOtherMode {
		t.Fatal("askOtherMode should clear after answering")
	}
	if m.input.Value() != "" {
		t.Fatalf("input should be cleared after submitting the free-text answer, got %q", m.input.Value())
	}
}

// TestAskUserOtherEscReturnsToMenu asserts Esc while typing a free-text answer
// backs out to the menu instead of submitting an empty/garbage answer.
func TestAskUserOtherEscReturnsToMenu(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	startAskUserCard(m)

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})

	if m.askOtherMode {
		t.Fatal("askOtherMode should clear on Esc")
	}
	if m.askReply == nil {
		t.Fatal("the question should still be pending after backing out of Other")
	}
	if m.input.Value() != "" {
		t.Fatalf("input should be cleared on Esc, got %q", m.input.Value())
	}
}

// TestAskUserSuppressesWorkingIndicator asserts the standalone working spinner
// is hidden while a question is pending (the card owns the '?' affordance),
// mirroring tool-confirmation behavior.
func TestAskUserSuppressesWorkingIndicator(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	startAskUserCard(m)

	if m.showWorkingIndicator() {
		t.Fatal("working indicator should be suppressed while asking")
	}
}

// TestAskStateClearedOnStreamDone asserts a turn ending (e.g. cancelled while a
// question was pending) resets the ask picker so the composer stops intercepting
// keystrokes (AD-072 bugbot high-severity fix).
func TestAskStateClearedOnStreamDone(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	startAskUserCard(m)
	m.askOtherMode = true
	if m.askReply == nil {
		t.Fatal("precondition: askReply should be pending")
	}

	m.handleStream(ui.StreamEvent{Type: ui.StreamDone})

	if m.askReply != nil {
		t.Fatal("askReply should be cleared after StreamDone")
	}
	if m.askOtherMode {
		t.Fatal("askOtherMode should be cleared after StreamDone")
	}
	if m.askChoice != 0 {
		t.Fatalf("askChoice = %d, want 0 after StreamDone", m.askChoice)
	}
}

// TestAskStateClearedOnStreamError asserts a failed turn likewise resets the
// pending question so the input is not stuck in answer mode.
func TestAskStateClearedOnStreamError(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.busy = true
	startAskUserCard(m)
	if m.askReply == nil {
		t.Fatal("precondition: askReply should be pending")
	}

	m.handleStream(ui.StreamEvent{Type: ui.StreamError, Text: "boom"})

	if m.askReply != nil {
		t.Fatal("askReply should be cleared after StreamError")
	}
}
