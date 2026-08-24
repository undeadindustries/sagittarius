package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestMaxToolRoundsOverride(t *testing.T) {
	t.Parallel()

	n := goal.EvaluatorMaxToolRounds
	runner, err := NewRunner(RunnerConfig{
		Generator:             &fakeGenerator{},
		Model:                 "test-model",
		WorkDir:               t.TempDir(),
		MaxToolRoundsOverride: &n,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := runner.maxToolRounds(); got != n {
		t.Fatalf("maxToolRounds() = %d, want %d", got, n)
	}
}

func TestEvaluatorRunnerReadOnlyAndPrompt(t *testing.T) {
	t.Parallel()

	parent, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	child, err := parent.newEvaluatorRunner(testContext(t))
	if err != nil {
		t.Fatalf("newEvaluatorRunner: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })

	if child.InteractionMode() != modes.ModeAsk {
		t.Fatalf("mode = %s, want ask", child.InteractionMode())
	}
	if got := child.maxToolRounds(); got != goal.EvaluatorMaxToolRounds {
		t.Fatalf("maxToolRounds = %d, want %d", got, goal.EvaluatorMaxToolRounds)
	}
	if _, ok := child.registry.Lookup(tools.TaskToolName); ok {
		t.Fatal("evaluator must not register task")
	}
	if _, ok := child.registry.Lookup("update_goal"); ok {
		t.Fatal("evaluator must not register update_goal")
	}

	allowed, reason := tools.InteractionModeAllow(child.InteractionMode(), tools.WriteFileToolName, map[string]any{
		tools.ParamFilePath:         "out.go",
		tools.WriteFileParamContent: "hi",
	}, nil)
	if allowed {
		t.Fatalf("write_file allowed in evaluator mode: %s", reason)
	}

	child.rebuildSystem()
	child.modelMu.RLock()
	sys := child.system
	child.modelMu.RUnlock()
	if !strings.Contains(sys, "You did not do this work") {
		t.Fatalf("system prompt missing evaluator stance:\n%s", sys)
	}
	if strings.Contains(sys, "Ask mode ACTIVE") {
		t.Fatal("ask-mode suffix leaked into evaluator prompt")
	}
}

func TestEvaluatorNestingRefused(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{sessionKindSubagent, sessionKindEvaluator} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			rec := session.NewRecorder(t.TempDir(), "sid-"+kind, "hash", kind)
			parent, err := NewRunner(RunnerConfig{
				Generator:       &fakeGenerator{},
				Model:           "test-model",
				WorkDir:         t.TempDir(),
				SessionRecorder: rec,
			})
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			if _, err := parent.newEvaluatorRunner(testContext(t)); err == nil {
				t.Fatal("expected nesting refusal")
			}
		})
	}
}

func TestEvaluateGoalTurnSelfJudgeWarningOnce(t *testing.T) {
	t.Parallel()

	jsonReply := []provider.StreamResponse{{
		TextDelta: `{"done": false, "reason": "still open"}`,
		Done:      true,
	}}
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{jsonReply, jsonReply}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGoal(&goal.Goal{
		Objective: "document the adapters",
		Status:    goal.StatusActive,
		MaxTurns:  5,
	})

	ctx := testContext(t)
	first := make(chan ui.StreamEvent, 16)
	if !runner.evaluateGoalTurn(ctx, first, "") {
		t.Fatal("expected goal to continue")
	}
	close(first)
	if !infoContains(collectEvents(t, first), selfJudgeWarningText) {
		t.Fatal("first evaluation should announce the worker-judges-itself default")
	}

	second := make(chan ui.StreamEvent, 16)
	if !runner.evaluateGoalTurn(ctx, second, "") {
		t.Fatal("expected goal to continue on second eval")
	}
	close(second)
	if infoContains(collectEvents(t, second), selfJudgeWarningText) {
		t.Fatal("self-judge warning must fire only once")
	}
}

func TestEvaluateGoalTurnUsesEvaluatorPrompt(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: `{"done": false, "reason": "gap remains"}`, Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGoal(&goal.Goal{Objective: "add a nil guard", Status: goal.StatusActive, MaxTurns: 3})

	out := make(chan ui.StreamEvent, 16)
	if !runner.evaluateGoalTurn(testContext(t), out, "") {
		t.Fatal("expected continuation")
	}
	close(out)
	_ = collectEvents(t, out)

	req := gen.lastRequest()
	if req == nil {
		t.Fatal("expected evaluator generate request")
	}
	if !strings.Contains(req.SystemInstruction, "You did not do this work") {
		t.Fatalf("evaluator request missing judge prompt:\n%s", req.SystemInstruction)
	}
	if strings.Contains(req.SystemInstruction, "Ask mode ACTIVE") {
		t.Fatal("ask-mode suffix leaked onto the wire")
	}
}

func TestEvaluateGoalTurnParseRetryThenDone(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "I think it is done", Done: true}},
		{{TextDelta: `{"done": true, "reason": "file exists"}`, Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGoal(&goal.Goal{Objective: "add hello.txt", Status: goal.StatusActive, MaxTurns: 3})

	out := make(chan ui.StreamEvent, 16)
	if runner.evaluateGoalTurn(testContext(t), out, "") {
		t.Fatal("expected goal to complete after retry")
	}
	close(out)
	if runner.Goal().Status != goal.StatusComplete {
		t.Fatalf("status = %s, want complete", runner.Goal().Status)
	}
}

func TestEvaluateGoalTurnParseFailureContinues(t *testing.T) {
	t.Parallel()

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{{TextDelta: "nope", Done: true}},
		{{TextDelta: "still nope", Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGoal(&goal.Goal{Objective: "add hello.txt", Status: goal.StatusActive, MaxTurns: 3})

	out := make(chan ui.StreamEvent, 16)
	if !runner.evaluateGoalTurn(testContext(t), out, "") {
		t.Fatal("parse failure must not kill the goal")
	}
	close(out)
	events := collectEvents(t, out)
	if !infoContains(events, "could not parse decision") {
		t.Fatalf("expected parse-failure warning, got %+v", events)
	}
	if runner.Goal().Status != goal.StatusActive {
		t.Fatalf("status = %s, want active", runner.Goal().Status)
	}
}

func TestEvaluateGoalTurnDeniesWriteFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{
		{
			{ToolCalls: []provider.ToolCall{{
				Name: tools.WriteFileToolName,
				Args: map[string]any{
					tools.ParamFilePath:         "pwned.txt",
					tools.WriteFileParamContent: "no",
				},
			}}},
			{Done: true},
		},
		{{TextDelta: `{"done": false, "reason": "write was denied"}`, Done: true}},
	}}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetGoal(&goal.Goal{Objective: "inspect only", Status: goal.StatusActive, MaxTurns: 3})

	out := make(chan ui.StreamEvent, 16)
	_ = runner.evaluateGoalTurn(testContext(t), out, "")
	close(out)
	_ = collectEvents(t, out)

	if _, err := os.Stat(filepath.Join(root, "pwned.txt")); !os.IsNotExist(err) {
		t.Fatalf("evaluator wrote a file: %v", err)
	}
}

func TestGoalEvaluatorLabelSelfJudge(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     "worker-model",
		WorkDir:   t.TempDir(),
		Settings: &config.Settings{
			Providers: &config.ProvidersSettings{Active: "openrouter"},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	label := runner.GoalEvaluatorLabel()
	if !strings.Contains(label, selfJudgeWarningText) {
		t.Fatalf("label = %q, want self-judge warning", label)
	}
	if !strings.Contains(label, "worker-model") {
		t.Fatalf("label = %q, want worker model", label)
	}
}

func infoContains(events []ui.StreamEvent, needle string) bool {
	for _, ev := range events {
		if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, needle) {
			return true
		}
	}
	return false
}
