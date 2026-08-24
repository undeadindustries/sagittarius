package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestGoalStartAnnouncesEvaluator(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.evaluatorLabel = "openrouter / qwen/qwen3.5-122b (worker is grading its own work)"
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/goal fix the auth bug", deps)
	if result.Err != nil {
		t.Fatalf("Process: %v", result.Err)
	}
	joined := strings.Join(result.Messages, "\n")
	if !strings.Contains(joined, "Evaluator: "+hooks.evaluatorLabel) {
		t.Fatalf("start messages missing evaluator label:\n%s", joined)
	}
}

func TestGoalStatusShowsEvaluator(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.goal = &goal.Goal{
		Objective:  "fix the auth bug",
		Status:     goal.StatusActive,
		TurnCount:  2,
		MaxTurns:   25,
		LastReason: "tests still fail",
	}
	hooks.evaluatorLabel = "openrouter / qwen/qwen3.5-122b (worker is grading its own work)"
	p := slash.NewProcessor()

	result := p.Process(context.Background(), "/goal status", deps)
	if result.Err != nil {
		t.Fatalf("Process: %v", result.Err)
	}
	joined := strings.Join(result.Messages, "\n")
	if !strings.Contains(joined, "Evaluator: "+hooks.evaluatorLabel) {
		t.Fatalf("status missing evaluator line:\n%s", joined)
	}
	if !strings.Contains(joined, "Last evaluator reason: tests still fail") {
		t.Fatalf("status missing last reason:\n%s", joined)
	}
}
