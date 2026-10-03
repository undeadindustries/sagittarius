package agent

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func listDirBatch() []provider.StreamResponse {
	return []provider.StreamResponse{
		{ToolCalls: []provider.ToolCall{{
			Name: tools.ListDirectoryToolName,
			Args: map[string]any{tools.ParamDirPath: "."},
		}}},
		{Done: true},
	}
}

func textBatch(s string) []provider.StreamResponse {
	return []provider.StreamResponse{
		{TextDelta: s},
		{Done: true},
	}
}

func collectEventsReplyingContinue(t *testing.T, events <-chan ui.StreamEvent, reply ui.ConfirmDecision) (got []ui.StreamEvent, confirms int) {
	t.Helper()
	for ev := range events {
		if ev.Type == ui.StreamToolConfirm && ev.ToolName == continueAgentToolName && ev.ConfirmReply != nil {
			confirms++
			ev.ConfirmReply <- reply
		}
		got = append(got, ev)
	}
	return got, confirms
}

func eventHasText(events []ui.StreamEvent, typ ui.StreamEventType, substr string) bool {
	for _, ev := range events {
		if ev.Type == typ && strings.Contains(ev.Text, substr) {
			return true
		}
	}
	return false
}

func TestContinueRoundsOnceGrantsAnotherBatch(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			listDirBatch(),
			listDirBatch(),
			textBatch("done"),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           true,
		MaxToolRoundsOverride: &one,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got, confirms := collectEventsReplyingContinue(t, events, ui.ConfirmOnce)
	if confirms != 2 {
		t.Fatalf("Continue prompts = %d, want 2 (one after each capped batch)", confirms)
	}
	if eventHasText(got, ui.StreamError, "max tool rounds exceeded") {
		t.Fatal("Once should have completed after the text batch, not hit the cap error")
	}
	if gen.calls() != 3 {
		t.Fatalf("generator calls = %d, want 3", gen.calls())
	}
}

func TestContinueRoundsSessionUncapsTurn(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			listDirBatch(),
			listDirBatch(),
			listDirBatch(),
			textBatch("done"),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           true,
		MaxToolRoundsOverride: &one,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got, confirms := collectEventsReplyingContinue(t, events, ui.ConfirmSession)
	if confirms != 1 {
		t.Fatalf("Continue prompts = %d, want 1 (Session lifts the rest of the turn)", confirms)
	}
	if eventHasText(got, ui.StreamError, "max tool rounds exceeded") {
		t.Fatal("Session grant should not emit max tool rounds exceeded")
	}
	if gen.calls() != 4 {
		t.Fatalf("generator calls = %d, want 4", gen.calls())
	}
}

func TestContinueRoundsDenyStops(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			listDirBatch(),
			textBatch("should not run"),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           true,
		MaxToolRoundsOverride: &one,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got, confirms := collectEventsReplyingContinue(t, events, ui.ConfirmDeny)
	if confirms != 1 {
		t.Fatalf("Continue prompts = %d, want 1", confirms)
	}
	if !eventHasText(got, ui.StreamError, "max tool rounds exceeded") {
		t.Fatal("deny should emit max tool rounds exceeded")
	}
	if gen.calls() != 1 {
		t.Fatalf("generator calls = %d, want 1", gen.calls())
	}
}

func TestMaxToolRoundsZeroNeverPrompts(t *testing.T) {
	t.Parallel()

	zero := 0
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			listDirBatch(),
			listDirBatch(),
			listDirBatch(),
			textBatch("done"),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           true,
		MaxToolRoundsOverride: &zero,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got, confirms := collectEventsReplyingContinue(t, events, ui.ConfirmOnce)
	if confirms != 0 {
		t.Fatalf("Continue prompts = %d, want 0 when maxToolRounds is 0", confirms)
	}
	if eventHasText(got, ui.StreamError, "max tool rounds exceeded") {
		t.Fatal("unlimited turn should finish without a cap error")
	}
	if gen.calls() != 4 {
		t.Fatalf("generator calls = %d, want 4", gen.calls())
	}
}

func TestHeadlessMaxToolRoundsStopsWithoutPrompt(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			listDirBatch(),
			listDirBatch(),
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           false,
		MaxToolRoundsOverride: &one,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	got, confirms := collectEventsReplyingContinue(t, events, ui.ConfirmOnce)
	if confirms != 0 {
		t.Fatalf("headless Continue prompts = %d, want 0", confirms)
	}
	if !eventHasText(got, ui.StreamError, "max tool rounds exceeded") {
		t.Fatal("headless should stop at the cap without asking")
	}
	if gen.calls() != 1 {
		t.Fatalf("generator calls = %d, want 1", gen.calls())
	}
}

func TestHeadlessMaxToolRoundsRecordsMaxRoundsOutcome(t *testing.T) {
	t.Parallel()

	one := 1
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{listDirBatch()},
	}
	rec := session.NewRecorder(t.TempDir(), "max-rounds-outcome", "hash", "main")
	runner, err := NewRunner(RunnerConfig{
		Generator:             gen,
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		ApprovalMode:          ApprovalYolo,
		Interactive:           false,
		MaxToolRoundsOverride: &one,
		SessionRecorder:       rec,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "list")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	if got := loadedSessionOutcome(t, runner); got != session.OutcomeMaxRounds {
		t.Fatalf("outcome = %q, want %q", got, session.OutcomeMaxRounds)
	}
}

func TestCleanTurnRecordsDoneOutcome(t *testing.T) {
	t.Parallel()

	off := string(config.AutoTitleOff)
	gen := &fakeGenerator{
		batches: [][]provider.StreamResponse{textBatch("ok")},
	}
	rec := session.NewRecorder(t.TempDir(), "done-outcome", "hash", "main")
	runner, err := NewRunner(RunnerConfig{
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         t.TempDir(),
		ApprovalMode:    ApprovalYolo,
		Interactive:     false,
		SessionRecorder: rec,
		Settings: &config.Settings{
			Sagittarius: &config.SagittariusSettings{
				Sessions: &config.SagittariusSessionsConfig{AutoTitle: &off},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := runner.RunTurn(testContext(t), "hi")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	drainEvents(t, events)

	if got := loadedSessionOutcome(t, runner); got != session.OutcomeDone {
		t.Fatalf("outcome = %q, want %q", got, session.OutcomeDone)
	}
}

func loadedSessionOutcome(t *testing.T, runner *Runner) string {
	t.Helper()
	loaded, err := session.LoadSession(runner.SessionFilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	return loaded.Outcome
}
