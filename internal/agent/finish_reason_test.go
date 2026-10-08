package agent

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// infoTexts returns the text of every StreamInfo event.
func infoTexts(events []ui.StreamEvent) []string {
	var out []string
	for _, ev := range events {
		if ev.Type == ui.StreamInfo {
			out = append(out, ev.Text)
		}
	}
	return out
}

// runFinishTurn runs one turn against a generator that streams text and then a
// finish reason, returning the UI events.
func runFinishTurn(t *testing.T, finish string) []ui.StreamEvent {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{
		{TextDelta: "if (x) {"},
		{FinishReason: finish},
		{Done: true},
	}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      t.TempDir(),
		ApprovalMode: ApprovalYolo,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	events, err := runner.RunTurn(testContext(t), "quote the code")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	return collectEvents(t, events)
}

// TestTruncatedReplyNamesTheFinishReason is the regression for a Gemini reply
// that stopped mid code block with no sign anything was wrong.
func TestTruncatedReplyNamesTheFinishReason(t *testing.T) {
	got := infoTexts(runFinishTurn(t, "RECITATION"))
	for _, text := range got {
		if strings.Contains(text, "RECITATION") {
			return
		}
	}
	t.Fatalf("info events = %q, want one naming RECITATION", got)
}

func TestNormalFinishAddsNoNotice(t *testing.T) {
	for _, text := range infoTexts(runFinishTurn(t, "")) {
		if strings.Contains(strings.ToLower(text), "cut short") {
			t.Fatalf("unexpected cut-short notice on a normal finish: %q", text)
		}
	}
}
