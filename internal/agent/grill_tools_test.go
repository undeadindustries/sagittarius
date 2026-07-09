package agent

import (
	"context"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// newGrillRunner builds a minimal runner with a grill session already active,
// for exercising askUserTool directly without a full RunTurn round trip.
func newGrillRunner(t *testing.T) *Runner {
	t.Helper()
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGrill(&grill.Session{Topic: "widget pricing", Status: grill.StatusActive, StartedAt: time.Now()})
	return runner
}

// TestAskUserHeadlessAutoSelectsRecommended asserts that without an interactive
// UI (interactive=false, emit=nil) the tool degrades gracefully by answering
// with the recommended option, rather than blocking forever.
func TestAskUserHeadlessAutoSelectsRecommended(t *testing.T) {
	t.Parallel()
	runner := newGrillRunner(t)
	tool := &askUserTool{runner: runner}

	args := map[string]any{
		"question": "Should pricing be per-seat or flat-rate?",
		"options": []any{
			map[string]any{"label": "Per-seat", "description": "scales with team size"},
			map[string]any{"label": "Flat-rate", "description": "one price for everyone"},
		},
		"recommended_index": float64(1),
	}

	result, err := tool.ExecuteInteractive(context.Background(), args, false, nil)
	if err != nil {
		t.Fatalf("ExecuteInteractive: %v", err)
	}
	if result["answer"] != "Flat-rate" {
		t.Fatalf("answer = %v, want Flat-rate (the recommended option)", result["answer"])
	}

	g := runner.Grill()
	if g.QuestionCount != 1 {
		t.Fatalf("QuestionCount = %d, want 1", g.QuestionCount)
	}
	if len(g.Decisions) != 1 || g.Decisions[0].Answer != "Flat-rate" {
		t.Fatalf("Decisions = %+v, want one Flat-rate decision", g.Decisions)
	}
}

// TestAskUserInteractiveDeliversPickedOption asserts the interactive path
// blocks on the reply channel and records whatever answer the UI sends back,
// including a free-text "Other" answer (Index -1).
func TestAskUserInteractiveDeliversPickedOption(t *testing.T) {
	t.Parallel()
	runner := newGrillRunner(t)
	tool := &askUserTool{runner: runner}

	args := map[string]any{
		"question": "Which billing cycle?",
		"options": []any{
			map[string]any{"label": "Monthly"},
			map[string]any{"label": "Annual"},
		},
	}

	var captured ui.StreamEvent
	emit := func(ev ui.StreamEvent) {
		captured = ev
		if ev.Type == ui.StreamAskUser {
			ev.AskReply <- ui.AskAnswer{Index: -1, Text: "Quarterly, actually"}
		}
	}

	result, err := tool.ExecuteInteractive(context.Background(), args, true, emit)
	if err != nil {
		t.Fatalf("ExecuteInteractive: %v", err)
	}
	if captured.Type != ui.StreamAskUser {
		t.Fatalf("emitted event type = %v, want StreamAskUser", captured.Type)
	}
	if captured.AskQuestion != "Which billing cycle?" {
		t.Fatalf("AskQuestion = %q", captured.AskQuestion)
	}
	if len(captured.AskOptions) != 2 {
		t.Fatalf("AskOptions = %+v, want 2 entries", captured.AskOptions)
	}
	if result["answer"] != "Quarterly, actually" {
		t.Fatalf("answer = %v, want the free-text reply", result["answer"])
	}

	g := runner.Grill()
	if len(g.Decisions) != 1 || g.Decisions[0].Question != "Which billing cycle?" {
		t.Fatalf("Decisions = %+v", g.Decisions)
	}
}

// TestAskUserInteractiveCancelPropagatesContextError asserts a canceled
// context while waiting for the user's answer returns the context error
// instead of hanging, so a turn cancellation cannot leave a tool call stuck.
func TestAskUserInteractiveCancelPropagatesContextError(t *testing.T) {
	t.Parallel()
	runner := newGrillRunner(t)
	tool := &askUserTool{runner: runner}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	args := map[string]any{
		"question": "Anything?",
		"options":  []any{map[string]any{"label": "Yes"}, map[string]any{"label": "No"}},
	}
	emit := func(ui.StreamEvent) {}

	_, err := tool.ExecuteInteractive(ctx, args, true, emit)
	if err == nil {
		t.Fatal("expected context.Canceled error")
	}
}

// TestAskUserMissingOptionsErrors asserts the declaration's required
// parameters are actually enforced.
func TestAskUserMissingOptionsErrors(t *testing.T) {
	t.Parallel()
	runner := newGrillRunner(t)
	tool := &askUserTool{runner: runner}

	if _, err := tool.ExecuteInteractive(context.Background(), map[string]any{"question": "x"}, false, nil); err == nil {
		t.Fatal("expected error for missing options")
	}
	if _, err := tool.ExecuteInteractive(context.Background(), map[string]any{"options": []any{}}, false, nil); err == nil {
		t.Fatal("expected error for missing question")
	}
}

// TestAskUserNoActiveGrillSessionStillAnswers asserts the tool degrades
// gracefully (no panic, no decision recorded) if grill mode ended between the
// model deciding to call ask_user and the tool executing.
func TestAskUserNoActiveGrillSessionStillAnswers(t *testing.T) {
	t.Parallel()
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	tool := &askUserTool{runner: runner}

	args := map[string]any{
		"question": "Still relevant?",
		"options":  []any{map[string]any{"label": "Yes"}},
	}
	result, err := tool.ExecuteInteractive(context.Background(), args, false, nil)
	if err != nil {
		t.Fatalf("ExecuteInteractive: %v", err)
	}
	if result["answer"] != "Yes" {
		t.Fatalf("answer = %v, want Yes", result["answer"])
	}
	if g := runner.Grill(); g != nil {
		t.Fatalf("Grill() = %+v, want nil (no session to record into)", g)
	}
}
